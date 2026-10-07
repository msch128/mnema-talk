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

func TestHandlerBusyFetchCanRetry(t *testing.T) {
	oldWait := slotWait
	slotWait = 10 * time.Millisecond
	t.Cleanup(func() { slotWait = oldWait })

	for _, endpoint := range []string{"preview", "image"} {
		for _, limit := range []string{"server", "requester"} {
			t.Run(endpoint+"/"+limit, func(t *testing.T) {
				png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")
				var originHits atomic.Int32
				origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					originHits.Add(1)
					if endpoint == "image" {
						_, _ = w.Write(png)
						return
					}
					w.Header().Set("Content-Type", "text/html")
					_, _ = w.Write([]byte(`<title>Available after retry</title>`))
				}))
				t.Cleanup(origin.Close)

				fetcher := New()
				fetcher.allowPrivate = true // Only the isolated httptest origin is fetched.
				t.Cleanup(fetcher.client.CloseIdleConnections)
				user := &auth.User{ID: uuid.MustParse("00000000-0000-4000-8000-000000000001")}
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
				res, err := server.Client().Get(requestURL)
				if err != nil {
					t.Fatal(err)
				}
				var response httpx.ErrorResponse
				decodeErr := json.NewDecoder(res.Body).Decode(&response)
				_ = res.Body.Close()
				if res.StatusCode != http.StatusTooManyRequests || res.Header.Get("Retry-After") != "1" {
					t.Fatalf("busy response: status=%d Retry-After=%q", res.StatusCode, res.Header.Get("Retry-After"))
				}
				if decodeErr != nil || response.Error == nil || response.Error.Code != httpx.CodeRateLimited {
					t.Fatalf("busy response envelope: %+v, decode=%v", response, decodeErr)
				}
				if res.Header.Get("Cache-Control") != "" || originHits.Load() != 0 {
					t.Fatal("busy response was cacheable or reached the origin")
				}

				// The same URL must be fetched after capacity becomes available,
				// rather than served as a remembered negative preview or image.
				release()
				res, err = server.Client().Get(requestURL)
				if err != nil {
					t.Fatal(err)
				}
				body, readErr := io.ReadAll(res.Body)
				_ = res.Body.Close()
				if readErr != nil || res.StatusCode != http.StatusOK || originHits.Load() != 1 {
					t.Fatalf("retry response: status=%d origin hits=%d read=%v", res.StatusCode, originHits.Load(), readErr)
				}
				if res.Header.Get("Retry-After") != "" {
					t.Fatal("successful retry retained the transient header")
				}
				if endpoint == "image" {
					if !bytes.Equal(body, png) || res.Header.Get("Content-Type") != "image/png" ||
						res.Header.Get("X-Content-Type-Options") != "nosniff" ||
						res.Header.Get("Content-Security-Policy") != "default-src 'none'; sandbox" {
						t.Fatal("successful image retry changed its bytes or security headers")
					}
				} else {
					var preview Preview
					if err := json.Unmarshal(body, &preview); err != nil || preview.Title != "Available after retry" {
						t.Fatalf("successful preview retry: %+v, decode=%v", preview, err)
					}
				}
			})
		}
	}
}

func TestHandlerDefinitiveFailuresKeepTheirStatus(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body>No title or raster image</body></html>`))
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
