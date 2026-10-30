// Package web embeds the built Vue app (web/dist) into the binary.
package web

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"html"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// Handler serves the static build and falls back to index.html for client-side
// routes. Hashed assets are cached forever; index.html is always revalidated
// so a deploy reaches browsers immediately.
//
// name returns what the community is called (nil keeps the built-in product
// name): it becomes the page title and the name of the installed app, so a
// tab, a bookmark and the home screen carry the community's own name.
func Handler(name func(context.Context) string) http.Handler {
	return HandlerWithIcon(name, nil)
}

// HandlerWithIcon is Handler with the community icon: icon returns its version
// ("" while the Owner set none), and while there is one, the tab icon, the
// home-screen icon and the manifest's icons point at it instead of the Mnema
// icon. nil keeps the built-in icons.
func HandlerWithIcon(name, icon func(context.Context) string) http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		sub = distFS
	}
	h := newHandler(sub, name)
	h.icon = icon
	return h
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

// IndexHTML returns the built app shell (index.html), or nil when the web app
// was not built. The server fills in link preview tags for a few public pages.
func IndexHTML() []byte {
	b, err := distFS.ReadFile("dist/index.html")
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
	name       func(context.Context) string
	icon       func(context.Context) string
}

// The built page names the product in these two places; the server swaps in
// the community's name.
var (
	titleTag    = []byte("<title>Mnema Talk</title>")
	appTitleTag = []byte(`<meta name="apple-mobile-web-app-title" content="Mnema" />`)
	faviconTag  = []byte(`<link rel="icon" type="image/svg+xml" href="/favicon.svg" />`)
	touchIcon   = []byte(`<link rel="apple-touch-icon" href="/apple-touch-icon.png" />`)
)

// IconURL is where the community icon of the given version is served, at the
// given edge (128 or 512). The version makes the URL immutable.
func IconURL(version string, edge int) string {
	return "/api/community/icon?size=" + strconv.Itoa(edge) + "&v=" + url.QueryEscape(version)
}

// SetIcon points the tab icon and the home-screen icon of a copy of the built
// page at the community icon of the given version.
func SetIcon(page []byte, version string) []byte {
	if version == "" {
		return page
	}
	page = bytes.Replace(page, faviconTag, []byte(`<link rel="icon" type="image/png" sizes="128x128" href="`+html.EscapeString(IconURL(version, 128))+`" />`), 1)
	return bytes.Replace(page, touchIcon, []byte(`<link rel="apple-touch-icon" href="`+html.EscapeString(IconURL(version, 512))+`" />`), 1)
}

// SetAppTitle names the iOS home-screen label of a copy of the built page.
func SetAppTitle(page []byte, name string) []byte {
	return bytes.Replace(page, appTitleTag, []byte(`<meta name="apple-mobile-web-app-title" content="`+html.EscapeString(name)+`" />`), 1)
}

// shortNameRunes is how much of the name fits under a home-screen icon.
const shortNameRunes = 12

func (s *staticServer) serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	if s.indexHTML == nil {
		http.Error(w, "web app not built: run `make web`", http.StatusServiceUnavailable)
		return
	}
	page := s.indexHTML
	if s.name != nil {
		name := html.EscapeString(s.name(r.Context()))
		page = bytes.Replace(page, titleTag, []byte("<title>"+name+"</title>"), 1)
		page = SetAppTitle(page, s.name(r.Context()))
	}
	if s.icon != nil {
		page = SetIcon(page, s.icon(r.Context()))
	}
	if _, err := w.Write(page); err != nil {
		return
	}
}

// serveManifest answers the installable app's manifest with the community's
// name; a manifest that cannot be read goes out unchanged.
func (s *staticServer) serveManifest(w http.ResponseWriter, r *http.Request) {
	raw, err := fs.ReadFile(s.sub, manifestFile)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var doc map[string]any
	if (s.name != nil || s.icon != nil) && json.Unmarshal(raw, &doc) == nil {
		s.brandManifest(r.Context(), doc)
		if out, err := json.MarshalIndent(doc, "", "  "); err == nil {
			raw = out
		}
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "application/manifest+json")
	if _, err := w.Write(raw); err != nil {
		return
	}
}

// brandManifest writes the community's name, and its icon when it has one,
// into the manifest document.
func (s *staticServer) brandManifest(ctx context.Context, doc map[string]any) {
	if s.name != nil {
		name := s.name(ctx)
		short := []rune(name)
		if len(short) > shortNameRunes {
			short = short[:shortNameRunes]
		}
		doc["name"], doc["short_name"] = name, string(short)
	}
	if s.icon == nil {
		return
	}
	if v := s.icon(ctx); v != "" {
		doc["icons"] = []map[string]string{
			{"src": IconURL(v, 128), "sizes": "128x128", "type": "image/png", "purpose": "any"},
			{"src": IconURL(v, 512), "sizes": "512x512", "type": "image/png", "purpose": "any"},
		}
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
		s.serveIndex(w, r)
		return
	}
	info, err := fs.Stat(s.sub, name)
	if err != nil {
		if strings.HasPrefix(name, "assets/") {
			http.NotFound(w, r)
			return
		}
		s.serveIndex(w, r)
		return
	}
	if info.IsDir() {
		s.serveIndex(w, r)
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
		s.serveManifest(w, r)
		return
	}
	s.fileServer.ServeHTTP(w, r)
}

func newHandler(sub fs.FS, name func(context.Context) string) *staticServer {
	indexHTML, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		indexHTML = nil
	}
	return &staticServer{
		sub:        sub,
		indexHTML:  indexHTML,
		fileServer: http.FileServer(http.FS(sub)),
		name:       name,
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
