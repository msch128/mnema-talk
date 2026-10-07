package web

import (
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestStaticHandlerUnavailableAndMethod(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{http.MethodGet, "/", http.StatusServiceUnavailable},
		{http.MethodHead, "/index.html", http.StatusServiceUnavailable},
		{http.MethodPost, "/assets/main.js", http.StatusMethodNotAllowed},
	} {
		rec := httptest.NewRecorder()
		newHandler(fstest.MapFS{}).ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != tc.status {
			t.Fatalf("%s %s: %d", tc.method, tc.path, rec.Code)
		}
	}
	// Exercise the actual embedded filesystem independently of which build is present.
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK && rec.Code != http.StatusServiceUnavailable {
		t.Fatal(rec.Code)
	}
	_ = APIDocsPage()
}

type compressedFS struct {
	fs.FS
	statError bool
}
type compressedFile struct {
	fs.File
	statError bool
}

func (f compressedFS) Open(name string) (fs.File, error) {
	file, err := f.FS.Open(name)
	if err != nil {
		return nil, err
	}
	return compressedFile{file, f.statError}, nil
}
func (f compressedFile) Stat() (fs.FileInfo, error) {
	if f.statError {
		return nil, errors.New("stat failed")
	}
	return f.File.Stat()
}

func TestCompressedVariantFailureFallsBack(t *testing.T) {
	for _, brokenStat := range []bool{false, true} {
		base := fstest.MapFS{"assets/main.js.br": {Data: []byte("compressed")}}
		req := httptest.NewRequest(http.MethodGet, "/assets/main.js", nil)
		req.Header.Set("Accept-Encoding", "br")
		rec := httptest.NewRecorder()
		if serveCompressed(rec, req, compressedFS{base, brokenStat}, "assets/main.js") {
			t.Fatal("served an unreadable variant")
		}
		if rec.Body.Len() != 0 || rec.Header().Get("Content-Encoding") != "" {
			t.Fatal("failure wrote compressed response")
		}
	}
}

func TestCompressedUnknownExtensionAndEncodingWeights(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/assets/data.mnema-unknown", nil)
	req.Header.Set("Accept-Encoding", "BR ; q=0.5")
	rec := httptest.NewRecorder()
	if !serveCompressed(rec, req, fstest.MapFS{"assets/data.mnema-unknown.br": {Data: []byte("payload")}}, "assets/data.mnema-unknown") {
		t.Fatal("variant not served")
	}
	if rec.Header().Get("Content-Type") != "application/octet-stream" || strings.TrimSpace(rec.Body.String()) != "payload" {
		t.Fatal(rec.Header(), rec.Body.String())
	}
	for _, tc := range []struct {
		header string
		want   bool
	}{{"br;q=no", false}, {"br;q=-1", false}, {"br;q=0", false}, {"gzip", false}, {" br ; q=0.1", true}, {"br;other=value", true}} {
		if got := acceptsEncoding(tc.header, "br"); got != tc.want {
			t.Fatalf("%q = %v", tc.header, got)
		}
	}
}
