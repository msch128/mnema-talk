package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
	}, nil)

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
	}, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api-docs.html", nil))
	if body, _ := io.ReadAll(rec.Body); string(body) != "<html>app" {
		t.Fatalf("/api-docs.html served %q, want the app shell", body)
	}
}

func TestMissingAssetIs404(t *testing.T) {
	h := newHandler(fstest.MapFS{"index.html": {Data: []byte("<html>app")}}, nil)
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

func TestServiceWorkerAndManifestAreRevalidatedAndTyped(t *testing.T) {
	h := newHandler(fstest.MapFS{
		"index.html":           {Data: []byte("<html>")},
		"sw.js":                {Data: []byte("self")},
		"manifest.webmanifest": {Data: []byte(`{"name":"Mnema Talk"}`)},
		"favicon.svg":          {Data: []byte("<svg/>")},
	}, nil)
	cases := []struct{ path, wantType, wantCache string }{
		{"/sw.js", "text/javascript", "no-cache"},
		{"/manifest.webmanifest", "application/manifest+json", "no-cache"},
		{"/favicon.svg", "image/svg+xml", ""},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d", c.path, rec.Code)
		}
		if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, c.wantType) {
			t.Errorf("%s: Content-Type %q, want %q", c.path, got, c.wantType)
		}
		if got := rec.Header().Get("Cache-Control"); got != c.wantCache {
			t.Errorf("%s: Cache-Control %q, want %q", c.path, got, c.wantCache)
		}
	}
}

func TestPageAndManifestCarryTheCommunityName(t *testing.T) {
	h := newHandler(fstest.MapFS{
		"index.html":           {Data: []byte(`<title>Mnema Talk</title><meta name="apple-mobile-web-app-title" content="Mnema" />`)},
		"manifest.webmanifest": {Data: []byte(`{"name":"Mnema Talk","short_name":"Mnema","start_url":"/"}`)},
	}, func(context.Context) string { return `Lantern & Fox Studio Berlin` })

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	for _, want := range []string{`<title>Lantern &amp; Fox Studio Berlin</title>`, `content="Lantern &amp; Fox Studio Berlin"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("page lacks %s: %s", want, rec.Body)
		}
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/manifest.webmanifest", nil))
	var doc map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["name"] != "Lantern & Fox Studio Berlin" || doc["short_name"] != "Lantern & Fo" || doc["start_url"] != "/" {
		t.Errorf("manifest = %v", doc)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/manifest+json" {
		t.Errorf("Content-Type %q", got)
	}
}

func TestCommunityIconReplacesTheBuiltInIcons(t *testing.T) {
	shell := `<head><link rel="icon" type="image/svg+xml" href="/favicon.svg" />` +
		`<link rel="apple-touch-icon" href="/apple-touch-icon.png" /></head>`
	version := ""
	s := newHandler(fstest.MapFS{
		"index.html":           {Data: []byte(shell)},
		"manifest.webmanifest": {Data: []byte(`{"name":"Mnema Talk","icons":[{"src":"/pwa-icon-512.png"}]}`)},
	}, nil)
	s.icon = func(context.Context) string { return version }
	get := func(path string) string {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec.Body.String()
	}

	if got := get("/"); got != shell {
		t.Errorf("without an icon the page is unchanged:\n%s", got)
	}
	if !strings.Contains(get("/manifest.webmanifest"), "pwa-icon-512.png") {
		t.Error("without an icon the manifest keeps the Mnema icons")
	}

	version = "abc123"
	page := get("/")
	for _, want := range []string{
		`<link rel="icon" type="image/png" sizes="128x128" href="/api/community/icon?size=128&amp;v=abc123" />`,
		`<link rel="apple-touch-icon" href="/api/community/icon?size=512&amp;v=abc123" />`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("missing %q in %s", want, page)
		}
	}
	var doc struct {
		Icons []struct{ Src, Sizes, Type string }
	}
	if err := json.Unmarshal([]byte(get("/manifest.webmanifest")), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Icons) != 2 || doc.Icons[1].Src != "/api/community/icon?size=512&v=abc123" || doc.Icons[1].Sizes != "512x512" || doc.Icons[1].Type != "image/png" {
		t.Errorf("manifest icons: %+v", doc.Icons)
	}
}
