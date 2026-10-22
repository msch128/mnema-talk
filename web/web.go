// Package web embeds the built Vue app (web/dist) into the binary.
package web

import (
	"embed"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// Handler serves the static build and falls back to index.html for client-side
// routes. Hashed assets are cached forever; index.html is always revalidated
// so a deploy reaches browsers immediately.
func Handler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return newHandler(distFS)
	}
	return newHandler(sub)
}

// apiDocsPage is the API reference page. It is only served through
// APIDocsPage (behind the session check), never as a plain static file.
const apiDocsPage = "api-docs.html"

// APIDocsPage returns the built API reference page (Swagger UI), or nil when
// the web app was built without it.
func APIDocsPage() []byte {
	b, err := distFS.ReadFile("dist/" + apiDocsPage)
	if err != nil {
		return nil
	}
	return b
}

// Files of the installable app that sit at the root under a fixed name.
const (
	serviceWorkerScript = "sw.js"
	manifestFile        = "manifest.webmanifest"
)

type staticServer struct {
	sub        fs.FS
	indexHTML  []byte
	fileServer http.Handler
}

func (s *staticServer) serveIndex(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	if s.indexHTML == nil {
		http.Error(w, "web app not built: run `make web`", http.StatusServiceUnavailable)
		return
	}
	if _, err := w.Write(s.indexHTML); err != nil {
		return
	}
}

func isAppRoute(name string) bool {
	return name == "" || name == "index.html" || name == apiDocsPage
}

func isAllowedMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead
}

func (s *staticServer) serveAsset(w http.ResponseWriter, r *http.Request, name string) {
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Add("Vary", "Accept-Encoding")
	if serveCompressed(w, r, s.sub, name) {
		return
	}
	s.fileServer.ServeHTTP(w, r)
}

func (s *staticServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !isAllowedMethod(r.Method) {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if isAppRoute(name) {
		s.serveIndex(w)
		return
	}
	info, err := fs.Stat(s.sub, name)
	if err != nil {
		if strings.HasPrefix(name, "assets/") {
			http.NotFound(w, r)
			return
		}
		s.serveIndex(w)
		return
	}
	if info.IsDir() {
		s.serveIndex(w)
		return
	}
	if strings.HasPrefix(name, "assets/") {
		s.serveAsset(w, r, name)
		return
	}
	s.serveFixed(w, r, name)
}

// serveFixed serves a file whose name carries no hash. The service worker
// script and the manifest must be revalidated on every load (so a deploy
// reaches installed apps at once) and the manifest has its own media type,
// which the Go tables do not know.
func (s *staticServer) serveFixed(w http.ResponseWriter, r *http.Request, name string) {
	switch name {
	case serviceWorkerScript:
		w.Header().Set("Cache-Control", "no-cache")
	case manifestFile:
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Type", "application/manifest+json")
	}
	s.fileServer.ServeHTTP(w, r)
}

func newHandler(sub fs.FS) http.Handler {
	indexHTML, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		indexHTML = nil
	}
	return &staticServer{
		sub:        sub,
		indexHTML:  indexHTML,
		fileServer: http.FileServer(http.FS(sub)),
	}
}

// precompressed lists the variants the build writes next to assets
// (precompress in vite.config.js), in order of preference.
type compressionVariant struct {
	encoding string
	ext      string
}

var precompressed = []compressionVariant{
	{"br", ".br"},
	{"gzip", ".gz"},
}

func tryServeVariant(w http.ResponseWriter, r *http.Request, fsys fs.FS, name string, p compressionVariant) bool {
	f, err := fsys.Open(name + p.ext)
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	rs, ok := f.(io.ReadSeeker)
	if err != nil || !ok {
		return false
	}
	ctype := mime.TypeByExtension(path.Ext(name))
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Encoding", p.encoding)
	http.ServeContent(w, r, name, info.ModTime(), rs)
	return true
}

// serveCompressed answers with a precompressed variant of the asset if the
// client accepts one. Reports whether it wrote a response.
func serveCompressed(w http.ResponseWriter, r *http.Request, fsys fs.FS, name string) bool {
	accepted := r.Header.Get("Accept-Encoding")
	for _, p := range precompressed {
		if !acceptsEncoding(accepted, p.encoding) {
			continue
		}
		if tryServeVariant(w, r, fsys, name, p) {
			return true
		}
	}
	return false
}

// acceptsEncoding reports whether an Accept-Encoding header allows enc
// (listed by name and not refused with q=0).
func acceptsEncoding(header, enc string) bool {
	for _, part := range strings.Split(header, ",") {
		token, params, ok := strings.Cut(strings.TrimSpace(part), ";")
		if !ok {
			params = ""
		}
		if !strings.EqualFold(strings.TrimSpace(token), enc) {
			continue
		}
		q, hasQ := strings.CutPrefix(strings.ReplaceAll(params, " ", ""), "q=")
		if !hasQ {
			return true
		}
		weight, err := strconv.ParseFloat(q, 64)
		if err != nil {
			return false
		}
		return weight > 0
	}
	return false
}
