package server

import (
	"bytes"
	"context"
	"html"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/msch128/mnema-talk/internal/community"
	"github.com/msch128/mnema-talk/internal/onboarding"
	"github.com/msch128/mnema-talk/web"
)

// Link previews of the instance root and of invite links. Messengers and
// social sites read the Open Graph and Twitter tags of the page a link points
// to; the app shell has none, and a browser app cannot add them, because the
// crawlers do not run scripts. So for these two public routes the server
// writes the tags into index.html. They carry only public information: the
// community's name, the welcome text of a working invite, and the Mnema icon.
// Everything else is served unchanged by the static handler.

var invitePagePath = regexp.MustCompile(`^/invite/([A-Za-z0-9_-]{6,64})/?$`)

// joinPagePath is the vanity invite link (a slug of lowercase letters, digits
// and hyphens; any case is read, the lookup folds it).
var joinPagePath = regexp.MustCompile(`^/join/([A-Za-z0-9-]{3,32})/?$`)

const (
	// shareIcon is a 512x512 PNG: crawlers do not render SVG.
	shareIcon        = "/pwa-icon-512.png"
	shareTagline     = "Chat, voice and video for a private community."
	shareProduct     = "Mnema Talk"
	shareRootNameTTL = 30 * time.Second
)

// sharePages wraps the static web handler.
type sharePages struct {
	next      http.Handler
	index     []byte
	publicURL string // PUBLIC_URL without trailing slash; "" leaves the absolute tags out
	onboard   *onboarding.Handler
	names     *community.Names // the page title: the community name, else the host

	mu       sync.Mutex
	rootName string
	rootAt   time.Time
}

func newSharePages(next http.Handler, index []byte, publicURL string, onboard *onboarding.Handler, names *community.Names) http.Handler {
	if index == nil || onboard == nil {
		return next
	}
	return &sharePages{next: next, index: index, publicURL: strings.TrimRight(publicURL, "/"), onboard: onboard, names: names}
}

func (s *sharePages) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		s.next.ServeHTTP(w, r)
		return
	}
	switch {
	case r.URL.Path == "/":
		s.write(w, r, s.rootTags(r.Context()))
	case invitePagePath.MatchString(r.URL.Path):
		code := invitePagePath.FindStringSubmatch(r.URL.Path)[1]
		s.write(w, r, s.inviteTags(r, code))
	case joinPagePath.MatchString(r.URL.Path):
		slug := joinPagePath.FindStringSubmatch(r.URL.Path)[1]
		s.write(w, r, s.joinTags(r, slug))
	default:
		s.next.ServeHTTP(w, r)
	}
}

func (s *sharePages) write(w http.ResponseWriter, r *http.Request, p pageTags) {
	page := p.apply(s.index)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	if r.Method == http.MethodHead {
		return
	}
	if _, err := w.Write(page); err != nil {
		slog.Debug("share page write", "err", err)
	}
}

// communityName is cached briefly: crawlers and every page load of the root
// ask, and the name changes rarely.
func (s *sharePages) communityName(ctx context.Context) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Since(s.rootAt) < shareRootNameTTL {
		return s.rootName
	}
	info, err := s.onboard.RootShareInfo(ctx)
	if err != nil {
		slog.Warn("share page: community name", "err", err)
		return s.rootName
	}
	s.rootName, s.rootAt = info.CommunityName, time.Now()
	return s.rootName
}

func (s *sharePages) rootTags(ctx context.Context) pageTags {
	name := strings.TrimSpace(s.communityName(ctx))
	title := s.names.Get(ctx)
	t := pageTags{title: title, appName: title, description: shareTagline, url: s.abs("/"), image: s.abs(shareIcon)}
	if name != "" {
		t.description = "Join " + name + " on " + shareProduct + ". " + shareTagline
	}
	return s.withIcon(ctx, t)
}

func (s *sharePages) inviteTags(r *http.Request, code string) pageTags {
	// An invite page is never indexed, and a code in a URL is not shared with search engines.
	t := pageTags{title: shareProduct, description: shareTagline, image: s.abs(shareIcon), noindex: true}
	info, ok, err := s.onboard.InviteShareInfo(r, code)
	if err != nil {
		slog.Warn("share page: invite", "err", err)
	}
	if !ok {
		return t
	}
	name := strings.TrimSpace(info.CommunityName)
	if name == "" {
		name = shareProduct
	}
	t.appName = name
	t.title = "Join " + name + " on " + shareProduct
	t.description = info.Description
	if t.description == "" {
		t.description = "You have been invited to join " + name + "."
	}
	t.url = s.abs(r.URL.Path)
	return s.withIcon(r.Context(), t)
}

// withIcon puts the community icon, when the Owner set one, in place of the
// Mnema icon: in the preview image and in the tab icon of the page. It is as
// public as the name it goes with.
func (s *sharePages) withIcon(ctx context.Context, t pageTags) pageTags {
	if v := s.names.Icon(ctx); v != "" {
		t.icon = v
		t.image = s.abs(web.IconURL(v, 512))
	}
	return t
}

// joinTags is the preview of a vanity link: the community's name, like an
// invite. The slug is only looked up, never written into the page (the URL in
// og:url is escaped like every value); an unknown one gets the generic page.
func (s *sharePages) joinTags(r *http.Request, slug string) pageTags {
	t := pageTags{title: shareProduct, description: shareTagline, image: s.abs(shareIcon), noindex: true}
	info, ok, err := s.onboard.JoinShareInfo(r, slug)
	if err != nil {
		slog.Warn("share page: vanity link", "err", err)
	}
	if !ok {
		return t
	}
	name := strings.TrimSpace(info.CommunityName)
	if name == "" {
		name = s.names.Get(r.Context())
	}
	t.appName = name
	t.title = "Join " + name + " on " + shareProduct
	t.description = "You have been invited to join " + name + "."
	t.url = s.abs(r.URL.Path)
	return s.withIcon(r.Context(), t)
}

func (s *sharePages) abs(path string) string {
	if s.publicURL == "" {
		return ""
	}
	return s.publicURL + path
}

// pageTags are the values written into the head of the app shell.
type pageTags struct {
	title, description string
	url, image         string // absolute, or empty
	appName            string // home-screen label; empty keeps the built one
	noindex            bool
	icon               string // version of the community icon; empty keeps the Mnema icon
}

var (
	titleElement = regexp.MustCompile(`(?s)<title>.*?</title>`)
	headClose    = []byte("</head>")
)

// apply returns index with its title replaced and the preview tags added.
// Every value is escaped for an attribute or text node.
func (t pageTags) apply(index []byte) []byte {
	esc := html.EscapeString
	var b strings.Builder
	meta := func(attr, key, val string) {
		if val != "" {
			b.WriteString(`    <meta ` + attr + `="` + key + `" content="` + esc(val) + `" />` + "\n")
		}
	}
	meta("name", "description", t.description)
	if t.noindex {
		meta("name", "robots", "noindex, nofollow")
	}
	meta("property", "og:type", "website")
	meta("property", "og:site_name", shareProduct)
	meta("property", "og:title", t.title)
	meta("property", "og:description", t.description)
	meta("property", "og:url", t.url)
	if t.image != "" {
		meta("property", "og:image", t.image)
		meta("property", "og:image:width", "512")
		meta("property", "og:image:height", "512")
		meta("property", "og:image:alt", shareProduct)
		meta("name", "twitter:image", t.image)
	}
	meta("name", "twitter:card", "summary")
	meta("name", "twitter:title", t.title)
	meta("name", "twitter:description", t.description)

	out := titleElement.ReplaceAllLiteral(index, []byte("<title>"+esc(t.title)+"</title>"))
	if t.appName != "" {
		out = web.SetAppTitle(out, t.appName)
	}
	out = web.SetIcon(out, t.icon)
	return bytes.Replace(out, headClose, []byte(b.String()+"  </head>"), 1)
}
