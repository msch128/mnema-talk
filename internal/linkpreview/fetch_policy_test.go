package linkpreview

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"
)

type edgePreviewTransport func(*http.Request) (*http.Response, error)

func (f edgePreviewTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type edgePreviewReadFailure struct{}

func (edgePreviewReadFailure) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func edgePreviewResponse(r *http.Request, contentType string, body io.Reader) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {contentType}},
		Body: io.NopCloser(body), Request: r}
}

func TestMalformedAddressInputsFailClosed(t *testing.T) {
	f := New()
	for _, ip := range []net.IP{nil, {1, 2, 3}, {1, 2, 3, 4, 5}} {
		if publicIP(ip) || !f.isDenied(ip) {
			t.Errorf("invalid IP bytes %v must be non-public and denied", []byte(ip))
		}
	}
	for _, raw := range []string{"https://example.com/%not-encoded", "https://example.com/" + strings.Repeat("a", 2049)} {
		if _, err := f.Preview(context.Background(), raw); !errors.Is(err, ErrBlocked) {
			t.Errorf("malformed/oversized URL preview returned %v", err)
		}
		if _, _, err := f.Image(context.Background(), raw); !errors.Is(err, ErrBlocked) {
			t.Errorf("malformed/oversized URL image returned %v", err)
		}
	}
}

func TestDenyUsesDefaultResolverForLocalhost(t *testing.T) {
	f := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	f.Deny(ctx, "localhost")
	if !f.isDenied(net.ParseIP("127.0.0.1")) && !f.isDenied(net.ParseIP("::1")) {
		t.Fatal("default resolver did not retain either standard localhost address")
	}
}

func TestProductionDialGuardBlocksLoopbackAndExplicitlyDeniedPublicAddress(t *testing.T) {
	f := New()
	// Both default-port URLs pass the URL policy, but the native dialer's
	// resolved-address guard must refuse them before any connection is made.
	f.Deny(context.Background(), "93.184.216.34")
	for _, raw := range []string{"http://127.0.0.1/", "http://[::1]/", "http://93.184.216.34/"} {
		if _, err := f.Preview(context.Background(), raw); !errors.Is(err, ErrBlocked) {
			t.Fatalf("native resolved-address guard did not reject %q: %v", raw, err)
		}
	}
}

func TestResolvedAddressPolicyAllowsPublicTargetsAndRefusesUnsafeTargets(t *testing.T) {
	f := New()
	f.Deny(context.Background(), "93.184.216.34", "2606:4700::1111")
	for _, address := range []string{"93.184.216.35:443", "[2606:4700::1001]:80"} {
		if err := f.checkResolvedAddress(address); err != nil {
			t.Errorf("public target %q refused: %v", address, err)
		}
	}
	for _, address := range []string{
		"missing-port", "[::1", "127.0.0.1:80", "[::1]:443", "10.0.0.1:80",
		"169.254.169.254:80", "[fe80::1]:443", "not-an-ip:80",
		"93.184.216.34:80", "[2606:4700::1111]:443", "[::ffff:93.184.216.34]:80",
	} {
		if err := f.checkResolvedAddress(address); !errors.Is(err, ErrBlocked) {
			t.Errorf("unsafe/denied resolved target %q accepted: %v", address, err)
		}
	}
	// The existing local-origin test hook bypasses the IP policy after valid
	// host:port parsing, while malformed dialer input stays fail-closed.
	f.allowPrivate = true
	if err := f.checkResolvedAddress("127.0.0.1:80"); err != nil {
		t.Fatalf("existing private-address test hook changed: %v", err)
	}
	if err := f.checkResolvedAddress("missing-port"); !errors.Is(err, ErrBlocked) {
		t.Fatalf("private-address test hook accepted malformed address: %v", err)
	}
}

func TestPreviewRedirectsAreValidatedAndBounded(t *testing.T) {
	for _, target := range []string{"/card", "/loop", "file:///etc/passwd", "http://user@example.invalid/card"} {
		t.Run(target, func(t *testing.T) {
			var hits atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				if r.URL.Path == "/card" {
					w.Header().Set("Content-Type", "text/html")
					_, _ = io.WriteString(w, "<title>redirected card</title>")
					return
				}
				http.Redirect(w, r, target, http.StatusFound)
			}))
			defer srv.Close()
			f := New()
			f.allowPrivate = true
			p, err := f.Preview(context.Background(), srv.URL+"/start")
			switch target {
			case "/card":
				if err != nil || p.Title != "redirected card" || p.URL != srv.URL+"/start" || hits.Load() != 2 {
					t.Fatalf("same-origin redirect: preview=%+v err=%v hits=%d", p, err, hits.Load())
				}
			case "/loop":
				if err == nil || hits.Load() != maxRedirects || !strings.Contains(err.Error(), "too many redirects") {
					t.Fatalf("unbounded redirect loop: err=%v hits=%d", err, hits.Load())
				}
			default:
				if !errors.Is(err, ErrBlocked) || hits.Load() != 1 {
					t.Fatalf("unsafe redirect: err=%v hits=%d", err, hits.Load())
				}
			}
		})
	}
}

func TestPreviewHTTPFailuresUseShortNegativeCache(t *testing.T) {
	for _, failure := range []string{"transport", "status", "body"} {
		t.Run(failure, func(t *testing.T) {
			f := New()
			var hits atomic.Int32
			f.client.Transport = edgePreviewTransport(func(r *http.Request) (*http.Response, error) {
				hits.Add(1)
				switch failure {
				case "transport":
					return nil, context.DeadlineExceeded
				case "status":
					res := edgePreviewResponse(r, "text/html", strings.NewReader("upstream not found"))
					res.StatusCode = http.StatusNotFound
					return res, nil
				default:
					return edgePreviewResponse(r, "text/html", edgePreviewReadFailure{}), nil
				}
			})
			const raw = "https://example.invalid/card"
			start := time.Now()
			_, first := f.Preview(context.Background(), raw)
			if first == nil || errors.Is(first, ErrNoPreview) || errors.Is(first, ErrBlocked) {
				t.Fatalf("transient origin failure misclassified: %v", first)
			}
			_, second := f.Preview(context.Background(), raw)
			if second != first || hits.Load() != 1 {
				t.Fatalf("negative cache not reused: first=%v second=%v hits=%d", first, second, hits.Load())
			}
			expires := f.cache[raw].expires
			if expires.Before(start.Add(5*time.Minute)) || expires.After(time.Now().Add(5*time.Minute)) {
				t.Fatalf("transient negative cache expiry %v should be five minutes", expires)
			}
			if failure == "transport" && !errors.Is(first, context.DeadlineExceeded) {
				t.Fatalf("transport cause lost: %v", first)
			}
			if failure == "status" && first.Error() != "fetch: status 404" {
				t.Fatalf("HTTP status lost: %v", first)
			}
			if failure == "body" && !errors.Is(first, io.ErrUnexpectedEOF) {
				t.Fatalf("body failure cause lost: %v", first)
			}
		})
	}
}

func TestPreviewClipsUnicodeMetadataAndRejectsUnsafeImageURLs(t *testing.T) {
	for _, image := range []string{"javascript:alert(1)", "https://example.invalid/%bad%"} {
		t.Run(image, func(t *testing.T) {
			f := New()
			f.client.Transport = edgePreviewTransport(func(r *http.Request) (*http.Response, error) {
				html := `<title>` + strings.Repeat("界", maxTitleRunes+10) + `</title>` +
					`<meta name="description" content="` + strings.Repeat("ä", maxDescRunes+10) + `">` +
					`<meta property="og:site_name" content="` + strings.Repeat("🙂", 100) + `">` +
					`<meta property="og:image" content="` + image + `">`
				return edgePreviewResponse(r, "text/html", strings.NewReader(html)), nil
			})
			p, err := f.Preview(context.Background(), "https://example.invalid/card")
			if err != nil || p.Image != "" {
				t.Fatalf("unsafe image URL survived: p=%+v err=%v", p, err)
			}
			for _, field := range []struct {
				value string
				limit int
			}{{p.Title, maxTitleRunes}, {p.Description, maxDescRunes}, {p.SiteName, 80}} {
				if !utf8.ValidString(field.value) || utf8.RuneCountInString(field.value) != field.limit || !strings.HasSuffix(field.value, "…") {
					t.Errorf("metadata clipping should preserve Unicode and limit %d runes: %q", field.limit, field.value)
				}
			}
		})
	}
}

func TestOversizedRasterImageIsRejectedAndNotCached(t *testing.T) {
	f := New()
	var hits atomic.Int32
	const pngHeader = "\x89PNG\r\n\x1a\n"
	image := append([]byte(pngHeader), make([]byte, maxImageBytes+1-len(pngHeader))...)
	f.client.Transport = edgePreviewTransport(func(r *http.Request) (*http.Response, error) {
		hits.Add(1)
		return edgePreviewResponse(r, "image/png", bytes.NewReader(image)), nil
	})
	for range 2 {
		body, mime, err := f.Image(context.Background(), "https://example.invalid/too-large.png")
		if !errors.Is(err, ErrNoPreview) || body != nil || mime != "" {
			t.Fatalf("oversized image accepted: len=%d mime=%q err=%v", len(body), mime, err)
		}
	}
	if hits.Load() != 2 || f.imgCache.bytes != 0 {
		t.Fatalf("oversized image was cached: hits=%d bytes=%d", hits.Load(), f.imgCache.bytes)
	}
}

func TestPreviewCacheBoundsManyDistinctURLs(t *testing.T) {
	f := New()
	f.client.Transport = edgePreviewTransport(func(r *http.Request) (*http.Response, error) {
		return edgePreviewResponse(r, "text/html", strings.NewReader("<title>bounded</title>")), nil
	})
	var last string
	for i := range cacheMax + 2 {
		last = "https://example.invalid/card/" + strconv.Itoa(i)
		p, err := f.Preview(context.Background(), last)
		if err != nil || p.Title != "bounded" || len(f.cache) > cacheMax {
			t.Fatalf("cache insertion %d: err=%v entries=%d preview=%+v", i, err, len(f.cache), p)
		}
	}
	if p, err, ok := f.cached(last); !ok || err != nil || p.URL != last {
		t.Fatalf("newest entry disappeared under capacity pressure: ok=%v err=%v p=%+v", ok, err, p)
	}
}

func TestReplacingImageCacheEntryRecountsBytesAndRefreshesTTL(t *testing.T) {
	now := time.Unix(1000, 0)
	c := newImageCache(10, time.Minute)
	c.now = func() time.Time { return now }
	c.put("image", []byte("old"), "image/gif")
	c.put("other", []byte("side"), "image/png")
	now = now.Add(30 * time.Second)
	c.put("image", []byte("newer"), "image/webp")
	if c.bytes != 9 || len(c.items) != 2 || c.order.Len() != 2 {
		t.Fatalf("replacement retained stale byte/list entry: bytes=%d items=%d order=%d", c.bytes, len(c.items), c.order.Len())
	}
	now = now.Add(31 * time.Second)
	body, mime, ok := c.get("image")
	if !ok || string(body) != "newer" || mime != "image/webp" {
		t.Fatalf("replacement payload/type/TTL not refreshed: body=%q mime=%q ok=%v", body, mime, ok)
	}
	if _, _, ok := c.get("other"); ok || c.bytes != 5 {
		t.Fatalf("independent old entry expiry broken: old present=%v bytes=%d", ok, c.bytes)
	}
}

func TestImmediateOriginConcurrentPreviewAndImageCacheConsistency(t *testing.T) {
	for _, image := range []bool{false, true} {
		t.Run(strconv.FormatBool(image), func(t *testing.T) {
			f := New()
			var hits atomic.Int32
			const png = "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"
			f.client.Transport = edgePreviewTransport(func(r *http.Request) (*http.Response, error) {
				hits.Add(1)
				if image {
					return edgePreviewResponse(r, "image/png", strings.NewReader(png)), nil
				}
				return edgePreviewResponse(r, "text/html", strings.NewReader("<title>shared fast origin</title>")), nil
			})
			const phases, callers = 40, 32
			for phase := range phases {
				var wg sync.WaitGroup
				start := make(chan struct{})
				errs := make(chan error, callers)
				raw := "https://example.invalid/card/" + strconv.Itoa(phase)
				for range callers {
					wg.Add(1)
					go func() {
						defer wg.Done()
						<-start
						if image {
							body, mime, err := f.Image(context.Background(), raw)
							if err == nil && (string(body) != png || mime != "image/png") {
								err = fmt.Errorf("inconsistent image body=%q MIME=%q", body, mime)
							}
							errs <- err
						} else {
							p, err := f.Preview(context.Background(), raw)
							if err == nil && (p.Title != "shared fast origin" || p.URL != raw) {
								err = fmt.Errorf("inconsistent preview %+v", p)
							}
							errs <- err
						}
					}()
				}
				close(start)
				wg.Wait()
				close(errs)
				for err := range errs {
					if err != nil {
						t.Fatal(err)
					}
				}
			}
			if hits.Load() != phases {
				t.Fatalf("shared cache fetched %d times for %d distinct URLs", hits.Load(), phases)
			}
		})
	}
}
