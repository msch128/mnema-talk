//go:build integration

package account

import (
	"context"
	"crypto/rand"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
)

// exportRow is what a test reads back about one export.
type exportRow struct {
	downloaded, claimed, hasKey bool
}

func readExportRow(t *testing.T, pool *db.Pool, id uuid.UUID) exportRow {
	t.Helper()
	var row exportRow
	if err := pool.QueryRow(context.Background(), `SELECT downloaded_at IS NOT NULL, claimed_at IS NOT NULL, s3_key IS NOT NULL
		FROM data_exports WHERE id = $1`, id).Scan(&row.downloaded, &row.claimed, &row.hasKey); err != nil {
		t.Fatal(err)
	}
	return row
}

func readyExport(t *testing.T, h *API, member *auth.User) uuid.UUID {
	t.Helper()
	id, err := runExport(t, h, member.ID)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestACompleteDownloadCarriesItsLengthAndCanBeRepeatedForAnHour(t *testing.T) {
	h, pool, member, store, _ := exportFixture(t)
	ctx := context.Background()
	id := readyExport(t, h, member)
	w := httptest.NewRecorder()
	download(h, member, w, "same-origin")
	if w.Code != http.StatusOK || w.Header().Get("Content-Length") != strconv.Itoa(w.Body.Len()) {
		t.Fatalf("first download: %d, Content-Length %q for %d bytes", w.Code, w.Header().Get("Content-Length"), w.Body.Len())
	}
	if row := readExportRow(t, pool, id); !row.downloaded || row.claimed || !row.hasKey || store.count() != 1 {
		t.Fatalf("after a complete download the file stays for the hour: %+v files=%d", row, store.count())
	}
	st, err := h.exportState(ctx, member.ID)
	if err != nil || st.State != "ready" || st.DownloadedAt == nil || st.ExpiresAt == nil ||
		st.ExpiresAt.After(st.DownloadedAt.Add(exportRedownload+time.Second)) {
		t.Fatalf("status in the hour after the download: %+v %v", st, err)
	}
	again := httptest.NewRecorder()
	download(h, member, again, "same-origin")
	if again.Code != http.StatusOK || again.Body.Len() != w.Body.Len() {
		t.Fatalf("a second download within the hour: %d %d", again.Code, again.Body.Len())
	}
}

func TestTheFileIsGoneAnHourAfterTheDownload(t *testing.T) {
	h, pool, member, store, _ := exportFixture(t)
	ctx := context.Background()
	id := readyExport(t, h, member)
	download(h, member, httptest.NewRecorder(), "same-origin")
	if _, err := pool.Exec(ctx, `UPDATE data_exports SET downloaded_at = NOW() - INTERVAL '61 minutes' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	download(h, member, w, "same-origin")
	if w.Code != http.StatusNotFound {
		t.Fatalf("after the hour: %d", w.Code)
	}
	if st, err := h.exportState(ctx, member.ID); err != nil || st.State != "downloaded" {
		t.Fatalf("status after the hour: %+v %v", st, err)
	}
	if err := h.sweepExports(ctx); err != nil {
		t.Fatal(err)
	}
	if row := readExportRow(t, pool, id); row.hasKey || store.count() != 0 {
		t.Fatalf("the sweep removes the file: %+v files=%d", row, store.count())
	}
}

func TestTheHourNeverOutlastsTheExpiry(t *testing.T) {
	h, pool, member, _, _ := exportFixture(t)
	ctx := context.Background()
	id := readyExport(t, h, member)
	if _, err := pool.Exec(ctx, `UPDATE data_exports SET expires_at = NOW() + INTERVAL '5 minutes' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	download(h, member, httptest.NewRecorder(), "same-origin")
	st, err := h.exportState(ctx, member.ID)
	if err != nil || st.ExpiresAt == nil || st.ExpiresAt.After(time.Now().Add(6*time.Minute)) {
		t.Fatalf("the window ends with the expiry: %+v %v", st, err)
	}
}

// bigExport replaces the stored archive with n random bytes, more than the
// socket buffers between a test server and its client hold.
func bigExport(t *testing.T, pool *db.Pool, store *memStore, id uuid.UUID, n int) {
	t.Helper()
	var key string
	if err := pool.QueryRow(context.Background(), `UPDATE data_exports SET size_bytes = $2 WHERE id = $1 RETURNING s3_key`, id, n).Scan(&key); err != nil {
		t.Fatal(err)
	}
	body := make([]byte, n)
	_, _ = rand.Read(body)
	store.mu.Lock()
	store.data[key] = body
	store.mu.Unlock()
}

func TestATransferCutInTheMiddleKeepsTheExport(t *testing.T) {
	h, pool, member, store, _ := exportFixture(t)
	id := readyExport(t, h, member)
	const size = 32 << 20
	bigExport(t, pool, store, id, size)
	served := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(served)
		download(h, member, w, r.Header.Get("Sec-Fetch-Site"))
	}))
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	// A failed check must not leave the handler writing to a reader that never reads.
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.ContentLength != size {
		t.Fatalf("answer: %d, length %d", resp.StatusCode, resp.ContentLength)
	}
	if _, err := io.CopyN(io.Discard, resp.Body, 1<<20); err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close() // the client goes away after a megabyte
	select {
	case <-served:
	case <-time.After(30 * time.Second):
		t.Fatal("the handler did not notice the client leaving")
	}
	if row := readExportRow(t, pool, id); row.downloaded || row.claimed || !row.hasKey || store.count() != 1 {
		t.Fatalf("a cut transfer must leave the export as it was: %+v files=%d", row, store.count())
	}
}

func TestAPrefetchDoesNotTouchTheExport(t *testing.T) {
	h, pool, member, _, _ := exportFixture(t)
	id := readyExport(t, h, member)
	for _, header := range [][2]string{{"Sec-Purpose", "prefetch"}, {"Sec-Purpose", "prefetch;prerender"}, {"Purpose", "prefetch"}} {
		r := httptest.NewRequest(http.MethodGet, "/api/users/me/export/download", nil)
		r.Header.Set(header[0], header[1])
		w := httptest.NewRecorder()
		mounted(h, member).ServeHTTP(w, r)
		if w.Code != http.StatusForbidden || w.Body.Len() == 0 {
			t.Fatalf("%v: %d", header, w.Code)
		}
	}
	if row := readExportRow(t, pool, id); row.downloaded || row.claimed || !row.hasKey {
		t.Fatalf("a prefetch claimed the export: %+v", row)
	}
}

func TestAHeadRequestTakesNothing(t *testing.T) {
	h, pool, member, store, _ := exportFixture(t)
	id := readyExport(t, h, member)
	r := httptest.NewRequest(http.MethodHead, "/api/users/me/export/download", nil)
	w := httptest.NewRecorder()
	mounted(h, member).ServeHTTP(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("HEAD: %d", w.Code)
	}
	if row := readExportRow(t, pool, id); row.downloaded || row.claimed || !row.hasKey || store.count() != 1 {
		t.Fatalf("HEAD changed the export: %+v files=%d", row, store.count())
	}
}

func TestADownloadDoesNotChangeTheDailyLimit(t *testing.T) {
	h, pool, member, _, _ := exportFixture(t)
	ctx := context.Background()
	id := readyExport(t, h, member)
	download(h, member, httptest.NewRecorder(), "same-origin")
	for _, step := range []string{"after the download", "after the hour"} {
		if _, wait, err := h.claimExport(ctx, member.ID); err != nil || wait <= 0 {
			t.Fatalf("%s a new export must still wait: %v %v", step, wait, err)
		}
		if _, err := pool.Exec(ctx, `UPDATE data_exports SET downloaded_at = NOW() - INTERVAL '61 minutes' WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
	}
}

// mounted is the mounted account routes as the server serves them, for the
// signed-in member.
func mounted(h *API, member *auth.User) http.Handler {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(auth.WithUser(req.Context(), member)))
		})
	})
	r.Route("/api", h.MountAuthenticated)
	return r
}
