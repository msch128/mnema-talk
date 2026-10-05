// Package linkpreview fetches title, description and image of public web
// pages for link cards in chat. The server fetches on behalf of users, so it
// guards against SSRF: only http(s) on ports 80/443, and every connection is
// checked after DNS resolution (also on redirects), so private, loopback and
// link-local addresses are never reached.
package linkpreview

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html"
)

const (
	maxPageBytes  = 512 << 10
	maxImageBytes = 4 << 20
	maxRedirects  = 3
	fetchTimeout  = 6 * time.Second
	cacheTTL      = 6 * time.Hour
	cacheMax      = 2000
	maxTitleRunes = 200
	maxDescRunes  = 400
)

// Preview is what a link card shows.
type Preview struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	SiteName    string `json:"site_name,omitempty"`
	// Image is the original absolute image URL; clients load it through the
	// image proxy because the CSP only allows same-origin images.
	Image string `json:"image,omitempty"`
}

var (
	ErrBlocked   = errors.New("link target is not a public web address")
	ErrNoPreview = errors.New("page has no preview")
)

// Fetcher fetches and caches previews.
type Fetcher struct {
	client       *http.Client
	allowPrivate bool // tests only: lets httptest servers on loopback through

	mu    sync.Mutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	preview *Preview
	err     error
	expires time.Time
}

func New() *Fetcher {
	f := &Fetcher{cache: map[string]cacheEntry{}}
	dialer := &net.Dialer{
		Timeout: fetchTimeout,
		// Runs for the resolved address of every connection attempt, so
		// neither DNS rebinding nor a redirect can reach an internal host.
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return ErrBlocked
			}
			if f.allowPrivate {
				return nil
			}
			if !publicIP(net.ParseIP(host)) {
				return ErrBlocked
			}
			return nil
		},
	}
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   fetchTimeout,
		ResponseHeaderTimeout: fetchTimeout,
		MaxIdleConns:          16,
		IdleConnTimeout:       30 * time.Second,
	}
	f.client = &http.Client{
		Transport: transport,
		Timeout:   fetchTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return errors.New("too many redirects")
			}
			return f.checkURL(req.URL)
		},
	}
	return f
}

var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// publicIP reports whether ip is a globally routable unicast address.
func publicIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() &&
		!ip.IsLinkLocalUnicast() && !ip.IsUnspecified() && !cgnat.Contains(addr)
}

// checkURL allows only plain http(s) URLs on the default ports.
func (f *Fetcher) checkURL(u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return ErrBlocked
	}
	if u.User != nil || u.Hostname() == "" {
		return ErrBlocked
	}
	if p := u.Port(); p != "" && p != "80" && p != "443" && !f.allowPrivate {
		return ErrBlocked
	}
	return nil
}

func (f *Fetcher) parse(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || len(raw) > 2048 {
		return nil, ErrBlocked
	}
	if err := f.checkURL(u); err != nil {
		return nil, err
	}
	u.Fragment = ""
	return u, nil
}

// get fetches u and returns at most limit bytes of the body.
func (f *Fetcher) get(ctx context.Context, u *url.URL, accept string, limit int64) ([]byte, string, *url.URL, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", nil, ErrBlocked
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "MnemaTalk-LinkPreview/1.0")
	res, err := f.client.Do(req)
	if err != nil {
		if errors.Is(err, ErrBlocked) {
			return nil, "", nil, ErrBlocked
		}
		return nil, "", nil, fmt.Errorf("fetch: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, "", nil, fmt.Errorf("fetch: status %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, limit))
	if err != nil {
		return nil, "", nil, fmt.Errorf("read: %w", err)
	}
	return body, res.Header.Get("Content-Type"), res.Request.URL, nil
}

// Preview returns the link card for raw, from cache when possible.
func (f *Fetcher) Preview(ctx context.Context, raw string) (*Preview, error) {
	u, err := f.parse(raw)
	if err != nil {
		return nil, err
	}
	key := u.String()
	if p, err, ok := f.cached(key); ok {
		return p, err
	}
	p, err := f.fetchPreview(ctx, u)
	f.store(key, p, err)
	return p, err
}

func (f *Fetcher) fetchPreview(ctx context.Context, u *url.URL) (*Preview, error) {
	body, ctype, final, err := f.get(ctx, u, "text/html,application/xhtml+xml", maxPageBytes)
	if err != nil {
		return nil, err
	}
	if !strings.Contains(strings.ToLower(ctype), "html") {
		return nil, ErrNoPreview
	}
	p := parseHTML(body, final)
	if p.Title == "" {
		return nil, ErrNoPreview
	}
	p.URL = u.String()
	return p, nil
}

// parseHTML reads Open Graph tags with <title> and meta description as
// fallbacks. Text is plain (entities decoded); the client escapes it.
func parseHTML(body []byte, base *url.URL) *Preview {
	p := &Preview{}
	var title, desc string
	z := html.NewTokenizer(bytes.NewReader(body))
	inTitle := false
	for {
		switch z.Next() {
		case html.ErrorToken:
			return finish(p, title, desc, base)
		case html.StartTagToken, html.SelfClosingTagToken:
			tok := z.Token()
			switch tok.Data {
			case "title":
				inTitle = true
			case "meta":
				var key, content string
				for _, a := range tok.Attr {
					switch strings.ToLower(a.Key) {
					case "property", "name":
						key = strings.ToLower(a.Val)
					case "content":
						content = a.Val
					}
				}
				switch key {
				case "og:title":
					p.Title = content
				case "og:description":
					p.Description = content
				case "og:site_name":
					p.SiteName = content
				case "og:image", "og:image:url":
					if p.Image == "" {
						p.Image = content
					}
				case "description":
					desc = content
				}
			case "body":
				// Everything needed lives in <head>.
				return finish(p, title, desc, base)
			}
		case html.TextToken:
			if inTitle && title == "" {
				title = string(z.Text())
			}
		case html.EndTagToken:
			if tok := z.Token(); tok.Data == "title" {
				inTitle = false
			}
		}
	}
}

func finish(p *Preview, title, desc string, base *url.URL) *Preview {
	if p.Title == "" {
		p.Title = title
	}
	if p.Description == "" {
		p.Description = desc
	}
	p.Title = clip(p.Title, maxTitleRunes)
	p.Description = clip(p.Description, maxDescRunes)
	p.SiteName = clip(p.SiteName, 80)
	if p.Image != "" {
		if img, err := base.Parse(strings.TrimSpace(p.Image)); err == nil && (img.Scheme == "http" || img.Scheme == "https") {
			p.Image = img.String()
		} else {
			p.Image = ""
		}
	}
	return p
}

func clip(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max-1]) + "…"
}

// imageTypes are the formats the proxy passes through, detected by sniffing.
var imageTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

// Image fetches a preview image. Only real raster images (by content) pass.
func (f *Fetcher) Image(ctx context.Context, raw string) ([]byte, string, error) {
	u, err := f.parse(raw)
	if err != nil {
		return nil, "", err
	}
	body, _, _, err := f.get(ctx, u, "image/*", maxImageBytes+1)
	if err != nil {
		return nil, "", err
	}
	if len(body) > maxImageBytes {
		return nil, "", ErrNoPreview
	}
	mime := http.DetectContentType(body)
	if !imageTypes[mime] {
		return nil, "", ErrNoPreview
	}
	return body, mime, nil
}

func (f *Fetcher) cached(key string) (*Preview, error, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.cache[key]
	if !ok || time.Now().After(e.expires) {
		return nil, nil, false
	}
	return e.preview, e.err, true
}

func (f *Fetcher) store(key string, p *Preview, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.cache) >= cacheMax {
		now := time.Now()
		for k, e := range f.cache {
			if now.After(e.expires) || len(f.cache) >= cacheMax {
				delete(f.cache, k)
			}
		}
	}
	ttl := cacheTTL
	if err != nil && !errors.Is(err, ErrNoPreview) && !errors.Is(err, ErrBlocked) {
		ttl = 5 * time.Minute // transient failures are retried sooner
	}
	f.cache[key] = cacheEntry{preview: p, err: err, expires: time.Now().Add(ttl)}
}
