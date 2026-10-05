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
		panic(err)
	}
	return newHandler(sub)
}

// apiDocsPage is the API reference page. It is only served through
// APIDocsPage (behind the session check), never as a plain static file.
const apiDocsPage = "api-docs.html"

// APIDocsPage returns the built API reference page (Swagger UI), or nil when
// the web app was built without it.
func APIDocsPage() []byte {
	b, _ := distFS.ReadFile("dist/" + apiDocsPage)
	return b
}

func newHandler(sub fs.FS) http.Handler {
	indexHTML, _ := fs.ReadFile(sub, "index.html")
	fileServer := http.FileServer(http.FS(sub))

	serveIndex := func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		if indexHTML == nil {
			http.Error(w, "web app not built: run `make web`", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write(indexHTML)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" || name == "index.html" || name == apiDocsPage {
			serveIndex(w)
			return
		}
		info, err := fs.Stat(sub, name)
		if err != nil && strings.HasPrefix(name, "assets/") {
			// A tab from before a deploy asks for an old hashed file: answer
			// 404 instead of the app shell, which the browser would reject
			// with a MIME error.
			http.NotFound(w, r)
			return
		}
		if err == nil && !info.IsDir() {
			if strings.HasPrefix(name, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				w.Header().Add("Vary", "Accept-Encoding")
				if serveCompressed(w, r, sub, name) {
					return
				}
			}
			fileServer.ServeHTTP(w, r)
			return
		}
		serveIndex(w)
	})
}

// precompressed lists the variants the build writes next to assets
// (precompress in vite.config.js), in order of preference.
var precompressed = []struct{ encoding, ext string }{
	{"br", ".br"},
	{"gzip", ".gz"},
}

// serveCompressed answers with a precompressed variant of the asset if the
// client accepts one. Reports whether it wrote a response.
func serveCompressed(w http.ResponseWriter, r *http.Request, fsys fs.FS, name string) bool {
	accepted := r.Header.Get("Accept-Encoding")
	for _, p := range precompressed {
		if !acceptsEncoding(accepted, p.encoding) {
			continue
		}
		f, err := fsys.Open(name + p.ext)
		if err != nil {
			continue
		}
		defer func() { _ = f.Close() }()
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
	return false
}

// acceptsEncoding reports whether an Accept-Encoding header allows enc
// (listed by name and not refused with q=0).
func acceptsEncoding(header, enc string) bool {
	for _, part := range strings.Split(header, ",") {
		token, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(token), enc) {
			continue
		}
		q, ok := strings.CutPrefix(strings.ReplaceAll(params, " ", ""), "q=")
		if !ok {
			return true
		}
		weight, err := strconv.ParseFloat(q, 64)
		return err == nil && weight > 0
	}
	return false
}
