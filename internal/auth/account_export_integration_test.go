//go:build integration

package auth

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

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

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
		return nil, errors.New("not found")
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

func exportFixture(t *testing.T) (*Handler, *db.Pool, *User, *memStore, uuid.UUID) {
	t.Helper()
	pool, _, member := authFixture(t)
	store := newMemStore()
	h := NewHandler(&Sessions{DB: pool, Secret: "export-test-secret-export-test-secret"}, nil)
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

func runExport(t *testing.T, h *Handler, user uuid.UUID) (uuid.UUID, error) {
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

func download(h *Handler, user *User, w http.ResponseWriter, site string) {
	r := httptest.NewRequest(http.MethodGet, "/api/users/me/export/download", nil)
	if site != "" {
		r.Header.Set("Sec-Fetch-Site", site)
	}
	httpx.Handle(h.downloadExport).ServeHTTP(w, r.WithContext(WithUser(r.Context(), user)))
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
