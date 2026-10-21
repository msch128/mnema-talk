package linkpreview

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
)

func newMockOriginServer(t *testing.T, endpoint string, hits *atomic.Int32, png []byte) *httptest.Server {
	t.Helper()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if endpoint == "image" {
			if _, err := w.Write(png); err != nil {
				t.Logf("write png: %v", err)
			}
			return
		}
		w.Header().Set("Content-Type", "text/html")
		if _, err := w.Write([]byte(`<title>Available after retry</title>`)); err != nil {
			t.Logf("write html: %v", err)
		}
	}))
	t.Cleanup(origin.Close)
	return origin
}

func acquireTestSlots(t *testing.T, fetcher *Fetcher, limit string, user *auth.User) func() {
	t.Helper()
	ctx := context.Background()
	count := maxConcurrentFetches
	if limit == "requester" {
		ctx = WithRequester(ctx, user.ID.String())
		count = maxFetchesPerRequester
	}
	var releases []func()
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			for _, free := range releases {
				free()
			}
		})
	}
	t.Cleanup(release)
	for i := 0; i < count; i++ {
		free, err := fetcher.acquire(ctx)
		if err != nil {
			t.Fatalf("hold fetch slot: %v", err)
		}
		releases = append(releases, free)
	}
	return release
}

func assertBusyHeaders(t *testing.T, res *http.Response) {
	t.Helper()
	if res.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", res.StatusCode)
	}
	if res.Header.Get("Retry-After") != "1" {
		t.Fatalf("expected Retry-After 1, got %q", res.Header.Get("Retry-After"))
	}
	if res.Header.Get("Cache-Control") != "" {
		t.Fatalf("unexpected Cache-Control: %q", res.Header.Get("Cache-Control"))
	}
}

func assertBusyPayload(t *testing.T, res *http.Response) {
	t.Helper()
	var response httpx.ErrorResponse
	decodeErr := json.NewDecoder(res.Body).Decode(&response)
	_ = res.Body.Close()
	if decodeErr != nil {
		t.Fatalf("decode busy response: %v", decodeErr)
	}
	if response.Error == nil {
		t.Fatal("expected non-nil error object")
	}
	if response.Error.Code != httpx.CodeRateLimited {
		t.Fatalf("expected rate limited code, got %s", response.Error.Code)
	}
}

func assertBusyResponse(t *testing.T, server *httptest.Server, requestURL string, hits *atomic.Int32) {
	t.Helper()
	res, err := server.Client().Get(requestURL)
	if err != nil {
		t.Fatal(err)
	}
	assertBusyHeaders(t, res)
	assertBusyPayload(t, res)
	if hits.Load() != 0 {
		t.Fatalf("origin hits reached %d during busy response", hits.Load())
	}
}

func assertRetryImageResponse(t *testing.T, res *http.Response, body, png []byte) {
	t.Helper()
	if !bytes.Equal(body, png) {
		t.Fatal("image retry payload mismatch")
	}
	if res.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("unexpected content type: %q", res.Header.Get("Content-Type"))
	}
	if res.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("unexpected nosniff header: %q", res.Header.Get("X-Content-Type-Options"))
	}
	if res.Header.Get("Content-Security-Policy") != "default-src 'none'; sandbox" {
		t.Fatalf("unexpected CSP header: %q", res.Header.Get("Content-Security-Policy"))
	}
}

func assertRetryPreviewResponse(t *testing.T, body []byte) {
	t.Helper()
	var preview Preview
	if err := json.Unmarshal(body, &preview); err != nil {
		t.Fatalf("decode preview retry: %v", err)
	}
	if preview.Title != "Available after retry" {
		t.Fatalf("unexpected title: %q", preview.Title)
	}
}

func assertRetrySuccess(t *testing.T, server *httptest.Server, requestURL, endpoint string, png []byte, hits *atomic.Int32) {
	t.Helper()
	res, err := server.Client().Get(requestURL)
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if readErr != nil {
		t.Fatalf("read retry body: %v", readErr)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", res.StatusCode)
	}
	if hits.Load() != 1 {
		t.Fatalf("expected 1 origin hit, got %d", hits.Load())
	}
	if res.Header.Get("Retry-After") != "" {
		t.Fatalf("unexpected Retry-After on success: %q", res.Header.Get("Retry-After"))
	}
	if endpoint == "image" {
		assertRetryImageResponse(t, res, body, png)
	} else {
		assertRetryPreviewResponse(t, body)
	}
}

func runBusyFetchScenario(t *testing.T, endpoint, limit string) {
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")
	var originHits atomic.Int32
	origin := newMockOriginServer(t, endpoint, &originHits, png)

	fetcher := New()
	fetcher.allowPrivate = true // Only the isolated httptest origin is fetched.
	t.Cleanup(fetcher.client.CloseIdleConnections)

	user := &auth.User{ID: uuid.MustParse("00000000-0000-4000-8000-000000000001")}
	release := acquireTestSlots(t, fetcher, limit, user)

	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), user)))
		})
	})
	(&Handler{Fetcher: fetcher}).Mount(router)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	path := "/link-preview"
	if endpoint == "image" {
		path += "/image"
	}
	requestURL := server.URL + path + "?url=" + url.QueryEscape(origin.URL)
	assertBusyResponse(t, server, requestURL, &originHits)

	// The same URL must be fetched after capacity becomes available,
	// rather than served as a remembered negative preview or image.
	release()
	assertRetrySuccess(t, server, requestURL, endpoint, png, &originHits)
}

func TestHandlerBusyFetchCanRetry(t *testing.T) {
	oldWait := slotWait
	slotWait = 10 * time.Millisecond
	t.Cleanup(func() { slotWait = oldWait })

	for _, endpoint := range []string{"preview", "image"} {
		for _, limit := range []string{"server", "requester"} {
			t.Run(endpoint+"/"+limit, func(t *testing.T) {
				runBusyFetchScenario(t, endpoint, limit)
			})
		}
	}
}

func TestHandlerDefinitiveFailuresKeepTheirStatus(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if _, err := w.Write([]byte(`<html><body>No title or raster image</body></html>`)); err != nil {
			t.Logf("write html: %v", err)
		}
	}))
	t.Cleanup(origin.Close)
	fetcher := New()
	fetcher.allowPrivate = true
	t.Cleanup(fetcher.client.CloseIdleConnections)
	router := chi.NewRouter()
	(&Handler{Fetcher: fetcher}).Mount(router)
	for _, tc := range []struct {
		path   string
		target string
		status int
	}{
		{"/link-preview", origin.URL, http.StatusNoContent},
		{"/link-preview", "file:///forbidden", http.StatusNoContent},
		{"/link-preview/image", origin.URL, http.StatusNotFound},
		{"/link-preview/image", "file:///forbidden", http.StatusBadRequest},
	} {
		t.Run(tc.path+"/"+tc.target, func(t *testing.T) {
			res := httptest.NewRecorder()
			router.ServeHTTP(res, httptest.NewRequest(http.MethodGet, tc.path+"?url="+url.QueryEscape(tc.target), nil))
			if res.Code != tc.status || res.Header().Get("Retry-After") != "" {
				t.Fatalf("definitive failure: status=%d retry=%q", res.Code, res.Header().Get("Retry-After"))
			}
		})
	}
}

// failingWriter accepts headers and refuses the body, like a client that left.
type failingWriter struct {
	*httptest.ResponseRecorder
}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestImageWriteFailureAfterHeadersIsNotAnError(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")
	var hits atomic.Int32
	origin := newMockOriginServer(t, "image", &hits, png)
	fetcher := New()
	fetcher.allowPrivate = true
	t.Cleanup(fetcher.client.CloseIdleConnections)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/link-preview/image?url="+url.QueryEscape(origin.URL), nil)
	if err := (&Handler{Fetcher: fetcher}).image(failingWriter{rec}, req); err != nil {
		t.Fatalf("a failed body write after the headers went out returned %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
}
