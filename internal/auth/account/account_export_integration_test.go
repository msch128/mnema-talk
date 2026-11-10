//go:build integration

package account

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/auth/authn"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm/permtest"
	"github.com/msch128/mnema-talk/internal/userdata"
)

// errStoreMissing is what memStore answers for an unknown key.
var errStoreMissing = errors.New("not found")

// memStore is a minimal ObjectStore for these tests.
type memStore struct {
	mu   sync.Mutex
	data map[string][]byte
}

func newMemStore() *memStore { return &memStore{data: map[string][]byte{}} }

func (m *memStore) Upload(_ context.Context, key string, body io.Reader, _ string, _ int64) error {
	b, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.data[key] = b
	m.mu.Unlock()
	return nil
}

func (m *memStore) GetObjectFrom(_ context.Context, key string, _ int64) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.data[key]
	if !ok {
		return nil, errStoreMissing
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (m *memStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	delete(m.data, key)
	m.mu.Unlock()
	return nil
}

func (m *memStore) DeleteBatch(ctx context.Context, keys []string) error {
	for _, k := range keys {
		_ = m.Delete(ctx, k)
	}
	return nil
}

func (m *memStore) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.data)
}

func exportFixture(t *testing.T) (*API, *db.Pool, *auth.User, *memStore, uuid.UUID) {
	t.Helper()
	pool, _, member := authFixture(t)
	store := newMemStore()
	h := New(authn.NewHandler(&auth.Sessions{DB: pool, Secret: "export-test-secret-export-test-secret"}, permtest.NewFixture().Service(), nil))
	h.Objects = store
	var channel uuid.UUID
	if err := pool.QueryRow(context.Background(), `INSERT INTO channels (name, type) VALUES ('room', 'text') RETURNING id`).Scan(&channel); err != nil {
		t.Fatal(err)
	}
	return h, pool, member, store, channel
}

func seedMessages(t *testing.T, pool *db.Pool, user, channel uuid.UUID, n int) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `INSERT INTO messages (channel_id, user_id, content, created_at)
		SELECT $1, $2, 'message ' || g, NOW() - (($3 - g) || ' seconds')::interval FROM generate_series(1, $3) g`, channel, user, n)
	if err != nil {
		t.Fatal(err)
	}
}

type exportedDoc struct {
	Truncated bool `json:"truncated"`
	Messages  []struct {
		Content string `json:"content"`
	} `json:"messages"`
}

func runExport(t *testing.T, h *API, user uuid.UUID) (uuid.UUID, error) {
	t.Helper()
	id, _, err := h.claimExport(context.Background(), user)
	if err != nil {
		t.Fatal(err)
	}
	return id, h.buildExport(context.Background(), id, user)
}

func readMessages(t *testing.T, store *memStore) exportedDoc {
	t.Helper()
	for _, data := range store.data {
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range zr.File {
			if f.Name != "messages.json" {
				continue
			}
			rc, _ := f.Open()
			var doc exportedDoc
			if err := json.NewDecoder(rc).Decode(&doc); err != nil {
				t.Fatalf("messages.json is not valid JSON: %v", err)
			}
			return doc
		}
	}
	t.Fatal("no messages.json")
	return exportedDoc{}
}

func TestExportStreamsEveryMessageInOrderAcrossPages(t *testing.T) {
	h, pool, member, store, channel := exportFixture(t)
	seedMessages(t, pool, member.ID, channel, 2500)
	if _, err := runExport(t, h, member.ID); err != nil {
		t.Fatal(err)
	}
	doc := readMessages(t, store)
	if doc.Truncated || len(doc.Messages) != 2500 || doc.Messages[0].Content != "message 1" || doc.Messages[2499].Content != "message 2500" {
		t.Fatalf("truncated=%v count=%d", doc.Truncated, len(doc.Messages))
	}
}

func TestExportStopsAtTheMessageBoundAndSaysSo(t *testing.T) {
	h, pool, member, store, channel := exportFixture(t)
	seedMessages(t, pool, member.ID, channel, 1500)
	old := exportMaxMessages
	exportMaxMessages = 1100
	t.Cleanup(func() { exportMaxMessages = old })
	if _, err := runExport(t, h, member.ID); err != nil {
		t.Fatal(err)
	}
	if doc := readMessages(t, store); !doc.Truncated || len(doc.Messages) != 1100 {
		t.Fatalf("truncated=%v count=%d", doc.Truncated, len(doc.Messages))
	}
}

func TestExportStopsAtTheMessageByteBoundAndSaysSo(t *testing.T) {
	h, pool, member, store, channel := exportFixture(t)
	seedMessages(t, pool, member.ID, channel, 400)
	old := exportMessageBytes
	exportMessageBytes = 5000
	t.Cleanup(func() { exportMessageBytes = old })
	if _, err := runExport(t, h, member.ID); err != nil {
		t.Fatal(err)
	}
	if doc := readMessages(t, store); !doc.Truncated || len(doc.Messages) == 0 || len(doc.Messages) >= 400 {
		t.Fatalf("truncated=%v count=%d", doc.Truncated, len(doc.Messages))
	}
}

func TestExportOverTheArchiveBudgetFailsAndLeavesNoFile(t *testing.T) {
	h, pool, member, store, channel := exportFixture(t)
	seedMessages(t, pool, member.ID, channel, 200)
	old := exportMaxBytes
	exportMaxBytes = 1500
	t.Cleanup(func() { exportMaxBytes = old })
	id, err := runExport(t, h, member.ID)
	if !errors.Is(err, errExportTooLarge) {
		t.Fatalf("want the budget error, got %v", err)
	}
	if store.count() != 0 {
		t.Fatal("a partial archive must not stay in storage")
	}
	var status string
	if err := pool.QueryRow(context.Background(), `SELECT status FROM data_exports WHERE id = $1`, id).Scan(&status); err != nil || status != "pending" {
		t.Fatalf("the build helper marks failure; the row stays pending here: %q %v", status, err)
	}
}

// failingWriter accepts the headers and then refuses the body, like a client
// that went away.
type failingWriter struct {
	*httptest.ResponseRecorder
}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("client gone") }

func download(h *API, user *auth.User, w http.ResponseWriter, site string) {
	r := httptest.NewRequest(http.MethodGet, "/api/users/me/export/download", nil)
	if site != "" {
		r.Header.Set("Sec-Fetch-Site", site)
	}
	httpx.Handle(h.downloadExport).ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), user)))
}

func TestDownloadRefusesRequestsFromOtherSitesAndDoesNotBurnTheExport(t *testing.T) {
	h, _, member, store, _ := exportFixture(t)
	if _, err := runExport(t, h, member.ID); err != nil {
		t.Fatal(err)
	}
	for _, site := range []string{"cross-site", "same-site"} {
		w := httptest.NewRecorder()
		download(h, member, w, site)
		if w.Code != http.StatusForbidden || store.count() != 1 {
			t.Fatalf("%s: %d, files %d", site, w.Code, store.count())
		}
	}
	w := httptest.NewRecorder()
	download(h, member, w, "same-origin")
	if w.Code != http.StatusOK || store.count() != 0 {
		t.Fatalf("same-origin: %d, files %d", w.Code, store.count())
	}
}

func TestAnInterruptedDownloadKeepsTheExportForAnotherTry(t *testing.T) {
	h, pool, member, store, _ := exportFixture(t)
	id, err := runExport(t, h, member.ID)
	if err != nil {
		t.Fatal(err)
	}
	download(h, member, failingWriter{httptest.NewRecorder()}, "same-origin")
	var downloaded, claimed bool
	if err := pool.QueryRow(context.Background(), `SELECT downloaded_at IS NOT NULL, claimed_at IS NOT NULL FROM data_exports WHERE id = $1`, id).Scan(&downloaded, &claimed); err != nil {
		t.Fatal(err)
	}
	if downloaded || claimed || store.count() != 1 {
		t.Fatalf("an unfinished copy must not spend the export: downloaded=%v claimed=%v files=%d", downloaded, claimed, store.count())
	}
	w := httptest.NewRecorder()
	download(h, member, w, "same-origin")
	if w.Code != http.StatusOK || w.Body.Len() == 0 || store.count() != 0 {
		t.Fatalf("second try: %d %d files=%d", w.Code, w.Body.Len(), store.count())
	}
}

func TestOnlyOneDownloadCanHoldTheExportAtATime(t *testing.T) {
	h, pool, member, _, _ := exportFixture(t)
	id, err := runExport(t, h, member.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE data_exports SET claimed_at = NOW() WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	download(h, member, w, "none")
	if w.Code != http.StatusNotFound {
		t.Fatalf("a held export: %d", w.Code)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE data_exports SET claimed_at = NOW() - INTERVAL '11 minutes' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	download(h, member, w, "none")
	if w.Code != http.StatusOK {
		t.Fatalf("a lapsed hold: %d", w.Code)
	}
}

func TestExportSettingsLeaveOutNamesTheMemberCannotSee(t *testing.T) {
	_, pool, member, _, visible := exportFixture(t)
	ctx := context.Background()
	var hidden uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO channels (name, type) VALUES ('backroom', 'text') RETURNING id`).Scan(&hidden); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO channel_reads (user_id, channel_id, notify_level) VALUES ($1, $2, 'all'), ($1, $3, 'mentions')`, member.ID, visible, hidden); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO server_notify (user_id, notify_level, suppress_everyone) VALUES ($1, 'mentions', TRUE)`, member.ID); err != nil {
		t.Fatal(err)
	}
	got, err := exportSettings(ctx, pool, member.ID, userdata.Channels{IDs: map[uuid.UUID]bool{visible: true}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(got.value)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Channels []struct {
			ID    uuid.UUID `json:"channel_id"`
			Name  *string   `json:"channel_name"`
			Level string    `json:"notify_level"`
		} `json:"channels"`
		Server struct {
			Level    string `json:"notify_level"`
			Suppress bool   `json:"suppress_everyone"`
		} `json:"server"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Server.Level != "mentions" || !doc.Server.Suppress {
		t.Fatalf("server setting: %+v", doc.Server)
	}
	if len(doc.Channels) != 2 {
		t.Fatalf("channels: %+v", doc.Channels)
	}
	for _, ch := range doc.Channels {
		switch ch.ID {
		case visible:
			if ch.Name == nil || *ch.Name != "room" || ch.Level != "all" {
				t.Fatalf("visible channel: %+v", ch)
			}
		case hidden:
			if ch.Name != nil || ch.Level != "mentions" {
				t.Fatalf("hidden channel keeps its name: %+v", ch)
			}
		default:
			t.Fatalf("unexpected channel %s", ch.ID)
		}
	}
}

func TestExportSettingsStopWhenAReadIsCancelled(t *testing.T) {
	_, pool, member, _, _ := exportFixture(t)
	for _, match := range []string{"FROM channel_reads", "FROM category_notify", "FROM server_notify"} {
		t.Run(match, func(t *testing.T) {
			hook := &authQueryHook{match: match, cancel: true}
			fault := authHookPool(t, pool, hook)
			_, err := exportSettings(context.Background(), fault, member.ID, userdata.Channels{})
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("err = %v, want the cancelled read", err)
			}
		})
	}
}
