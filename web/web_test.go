package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestServesPrecompressedAssets(t *testing.T) {
	h := newHandler(fstest.MapFS{
		"index.html":           {Data: []byte("<html>")},
		"assets/df.wasm":       {Data: []byte("raw")},
		"assets/df.wasm.br":    {Data: []byte("brotli")},
		"assets/df.wasm.gz":    {Data: []byte("gzip")},
		"assets/model.tgz":     {Data: []byte("tgz")},
		"assets/plain-only.js": {Data: []byte("js")},
	})

	cases := []struct {
		path, accept, wantBody, wantEncoding string
	}{
		{"/assets/df.wasm", "gzip, deflate, br, zstd", "brotli", "br"},
		{"/assets/df.wasm", "gzip", "gzip", "gzip"},
		{"/assets/df.wasm", "br;q=0, gzip", "gzip", "gzip"},
		{"/assets/df.wasm", "", "raw", ""},
		{"/assets/model.tgz", "br, gzip", "tgz", ""},
		{"/assets/plain-only.js", "br", "js", ""},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodGet, c.path, nil)
		if c.accept != "" {
			r.Header.Set("Accept-Encoding", c.accept)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		body, _ := io.ReadAll(rec.Body)
		if string(body) != c.wantBody || rec.Header().Get("Content-Encoding") != c.wantEncoding {
			t.Errorf("%s with %q: body %q encoding %q, want %q %q",
				c.path, c.accept, body, rec.Header().Get("Content-Encoding"), c.wantBody, c.wantEncoding)
		}
		if rec.Header().Get("Vary") != "Accept-Encoding" {
			t.Errorf("%s: Vary = %q", c.path, rec.Header().Get("Vary"))
		}
	}

	r := httptest.NewRequest(http.MethodGet, "/assets/df.wasm", nil)
	r.Header.Set("Accept-Encoding", "br")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if ct := rec.Header().Get("Content-Type"); ct != "application/wasm" {
		t.Errorf("Content-Type = %q, want application/wasm", ct)
	}
}

// The API reference page needs a session, so the static handler must never
// hand it out; /api/docs serves it behind the session check.
func TestDoesNotServeAPIDocsPageStatically(t *testing.T) {
	h := newHandler(fstest.MapFS{
		"index.html":    {Data: []byte("<html>app")},
		"api-docs.html": {Data: []byte("<html>docs")},
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api-docs.html", nil))
	if body, _ := io.ReadAll(rec.Body); string(body) != "<html>app" {
		t.Fatalf("/api-docs.html served %q, want the app shell", body)
	}
}

func TestMissingAssetIs404(t *testing.T) {
	h := newHandler(fstest.MapFS{"index.html": {Data: []byte("<html>app")}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/main-old.css", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing asset: %d, want 404", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/c/some-channel", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("client route: %d, want the app shell", rec.Code)
	}
}
