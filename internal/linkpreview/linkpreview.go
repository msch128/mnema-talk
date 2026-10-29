// Package linkpreview fetches title, description and image of public web
// pages for link cards in chat. The server fetches on behalf of users, so it
// guards against SSRF: only http(s) on ports 80/443, and every connection is
// checked after DNS resolution (also on redirects), so private, loopback and
// link-local addresses are never reached.
package linkpreview

import (
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

	"github.com/msch128/mnema-talk/internal/ssrf"
)

const (
	maxPageBytes  = 512 << 10
	maxImageBytes = 4 << 20
	maxRedirects  = 3
	maxURLLen     = 2048
	cacheTTL      = 6 * time.Hour
	cacheMax      = 2000
	maxTitleRunes = 200
	maxDescRunes  = 400
	// Proxied images are kept in memory up to this many bytes in total.
	imageCacheBytes = 32 << 20
	imageCacheTTL   = time.Hour
	// maxConcurrentFetches caps outbound fetches across all members (the
	// per-user rate limit alone allows many in parallel), and
	// maxFetchesPerRequester keeps one member's slow links from holding all
	// of them. Callers wait up to fetchTimeout for both slots, then get no
	// preview; that outcome is not cached.
	maxConcurrentFetches   = 8
	maxFetchesPerRequester = 2
)

// fetchTimeout is the total time one fetch may take, connect to last byte; a
// host that drips its answer is cut off here. Tests shorten it.
var fetchTimeout = 6 * time.Second

// Preview is what a link card shows. Everything but URL and Title is
// optional; the client shows what is there.
type Preview struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty" binding:"optional"`
	// SiteName is the provider ("YouTube", "Steam", the page's og:site_name).
	SiteName string `json:"site_name,omitempty" binding:"optional"`
	// Author is the channel, creator or publisher; AuthorURL links to it.
	Author    string `json:"author,omitempty" binding:"optional"`
	AuthorURL string `json:"author_url,omitempty" binding:"optional"`
	// Image is the original absolute image URL; clients load it through the
	// image proxy because the CSP only allows same-origin images.
	Image string `json:"image,omitempty" binding:"optional"`
	// Compact asks for a small thumbnail beside the text (a square avatar or
	// a "summary" card) instead of a wide image above it.
	Compact bool `json:"compact,omitempty" binding:"optional"`
	// Color is the page's theme colour as #rrggbb, for a card accent.
	Color string `json:"color,omitempty" binding:"optional"`
	// Fields are short labelled facts such as a price.
	Fields []Field `json:"fields,omitempty" binding:"optional"`
	// Embed is set for videos the client may play inline after a click.
	Embed *Embed `json:"embed,omitempty" binding:"optional"`

	// oembed is the discovered oEmbed endpoint of the page (not sent).
	oembed string
}

// Field is one labelled fact of a card. Key is stable ("price",
// "recommendations") so clients can translate Name.
type Field struct {
	Key   string `json:"key,omitempty" binding:"optional"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Embed is a player the client may load in a sandboxed iframe once the member
// clicks play. URL is always built by the server from the link's video id for
// a fixed set of providers, never taken from the fetched page.
type Embed struct {
	Provider string `json:"provider" enums:"youtube,vimeo"`
	URL      string `json:"url"`
}

var (
	ErrBlocked   = errors.New("link target is not a public web address")
	ErrNoPreview = errors.New("page has no preview")
	// errBusy means no fetch slot was free in time. It says nothing about
	// the link, so it is never cached.
	errBusy = errors.New("too many link previews in flight")
)

// slotWait is how long a fetch waits for its slots; tests shorten it.
var slotWait = fetchTimeout

// Fetcher fetches and caches previews.
type Fetcher struct {
	client       *http.Client
	allowPrivate bool // tests only: lets httptest servers on loopback through

	// denied are extra addresses to refuse, e.g. this server's own public
	// IP: behind a router with hairpin NAT it leads back into the LAN.
	// deniedHosts keeps the last addresses each host resolved to, so a
	// failed lookup keeps them instead of dropping them.
	deniedMu    sync.RWMutex
	denied      map[netip.Addr]bool
	deniedHosts map[string][]netip.Addr
	lookup      func(ctx context.Context, host string) ([]netip.Addr, error)

	mu    sync.Mutex
	cache map[string]cacheEntry

	slots     chan struct{} // one per outbound fetch in flight
	requester requesterSlots
	slow      slowHosts

	previews flightGroup[*Preview]
	images   flightGroup[cachedImage]
	imgCache *imageCache
}

type cacheEntry struct {
	preview *Preview
	err     error
	expires time.Time
}

func New() *Fetcher {
	f := &Fetcher{
		slots:    make(chan struct{}, maxConcurrentFetches),
		cache:    map[string]cacheEntry{},
		imgCache: newImageCache(imageCacheBytes, imageCacheTTL),
		lookup: func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		},
	}
	dialer := &net.Dialer{
		Timeout: fetchTimeout,
		// Runs for the resolved address of every connection attempt, so
		// neither DNS rebinding nor a redirect can reach an internal host.
		Control: func(_, address string, _ syscall.RawConn) error {
			return f.checkResolvedAddress(address)
		},
	}
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   fetchTimeout,
		ResponseHeaderTimeout: fetchTimeout / 2,
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
			if err := f.checkURL(req.URL); err != nil {
				return err
			}
			host := strings.ToLower(req.URL.Hostname())
			if h := lastHop(req.Context()); h != nil {
				h.next(host)
			}
			// The slow table applies on every hop, not only to the posted link.
			if f.slow.is(host) {
				return errSlowHost
			}
			return nil
		},
	}
	return f
}

func (f *Fetcher) parse(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if ssrf.RejectedURLText(raw) {
		return nil, ErrBlocked
	}
	u, err := url.Parse(raw)
	if err != nil {
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
	host := strings.ToLower(u.Hostname())
	if f.slow.is(host) {
		return nil, "", nil, errSlowHost
	}
	release, err := f.acquire(ctx)
	if err != nil {
		return nil, "", nil, err
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	hops := newHopTimer(host)
	ctx = context.WithValue(ctx, hopTimerKey{}, hops)
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
		hops.noteTimeout(&f.slow, err)
		return nil, "", nil, fmt.Errorf("fetch: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, "", nil, fmt.Errorf("fetch: status %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, limit))
	if err != nil {
		hops.noteTimeout(&f.slow, err)
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
	// Many members open the same link at once: fetch it once. The fetch is
	// shared, so one caller going away must not cancel it for the others. The
	// cache is read inside the shared call, so a caller arriving just after
	// another one finished never fetches a second time.
	return f.previews.do(key, func() (*Preview, error) {
		if p, err, ok := f.cached(key); ok {
			return p, err
		}
		p, err := f.fetchPreview(context.WithoutCancel(ctx), u)
		if !errors.Is(err, errBusy) {
			f.store(key, p, err)
		}
		return p, err
	})
}

func (f *Fetcher) fetchPreview(ctx context.Context, u *url.URL) (*Preview, error) {
	// Providers whose pages are not enough on their own are asked first; any
	// of them falling through leaves the generic page path below.
	vid := recognizeVideo(u)
	if vid != nil {
		if p, err := f.videoPreview(ctx, u, vid); p != nil || err != nil {
			return p, err
		}
	}
	if app := steamAppID(u); app != "" {
		if p, err := f.steamPreview(ctx, u, app); p != nil || err != nil {
			return p, err
		}
	}
	body, ctype, final, err := f.get(ctx, u, "text/html,application/xhtml+xml", maxPageBytes)
	if err != nil {
		return nil, err
	}
	if !strings.Contains(strings.ToLower(ctype), "html") {
		return nil, ErrNoPreview
	}
	p := parseHTML(body, ctype, final)
	if vid != nil {
		p.Embed, p.Compact = vid.embed(), false
	}
	if err := f.enrichFromOEmbed(ctx, p, final); err != nil {
		return nil, err
	}
	if p.Title == "" {
		return nil, ErrNoPreview
	}
	p.URL = u.String()
	return p, nil
}

func isBusy(err error) bool { return errors.Is(err, errBusy) }

// imageTypes are the formats the proxy passes through, detected by sniffing.
var imageTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

// Image fetches a preview image, from a small in-memory cache when possible.
// Only real raster images (by content) pass.
func (f *Fetcher) Image(ctx context.Context, raw string) ([]byte, string, error) {
	u, err := f.parse(raw)
	if err != nil {
		return nil, "", err
	}
	key := u.String()
	if body, mime, ok := f.imgCache.get(key); ok {
		return body, mime, nil
	}
	img, err := f.images.do(key, func() (cachedImage, error) {
		if body, mime, ok := f.imgCache.get(key); ok {
			return cachedImage{body: body, mime: mime}, nil
		}
		if _, err, ok := f.cached(imageFailureKey(key)); ok {
			return cachedImage{}, err
		}
		body, mime, err := f.fetchImage(context.WithoutCancel(ctx), u)
		if err != nil {
			if !errors.Is(err, errBusy) {
				f.store(imageFailureKey(key), nil, err)
			}
			return cachedImage{}, err
		}
		f.imgCache.put(key, body, mime)
		return cachedImage{body: body, mime: mime}, nil
	})
	if err != nil {
		return nil, "", err
	}
	return img.body, img.mime, nil
}

// imageFailureKey is the negative-cache key of a proxied image. Preview keys
// are http(s) URLs, so the prefix cannot collide with them.
func imageFailureKey(key string) string { return "image:" + key }

func (f *Fetcher) fetchImage(ctx context.Context, u *url.URL) ([]byte, string, error) {
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
