package linkpreview

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBlocksNonPublicAddresses(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.0.0.5", "192.168.1.20", "172.16.3.4", "169.254.169.254",
		"100.64.1.1", "0.0.0.0", "::1", "fd00::1", "fe80::1", "::ffff:127.0.0.1", "224.0.0.1",
		"198.18.0.1", "240.0.0.1", "192.0.2.10", "64:ff9b::c0a8:1", "2002:c0a8:1::1", "2001:0:4136:e378::1",
		"192.88.99.1", "fec0::1"} {
		if publicIP(net.ParseIP(ip)) {
			t.Errorf("%s must be blocked", ip)
		}
	}
	for _, ip := range []string{"93.184.216.34", "2606:4700::1111"} {
		if !publicIP(net.ParseIP(ip)) {
			t.Errorf("%s must be allowed", ip)
		}
	}
}

func TestDeniedAddressesAreRefused(t *testing.T) {
	f := New()
	f.Deny(context.Background(), "93.184.216.34", "")
	if !f.isDenied(net.ParseIP("93.184.216.34")) || f.isDenied(net.ParseIP("93.184.216.35")) {
		t.Fatal("deny list not applied")
	}
}

func TestRefusesPrivateTargetsAndOddURLs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<title>internal</title>`))
	}))
	defer srv.Close()
	f := New() // production guard: loopback is not public
	for _, u := range []string{srv.URL, "file:///etc/passwd", "ftp://example.com/", "http://user:pw@example.com/", "javascript:alert(1)"} {
		if _, err := f.Preview(context.Background(), u); err == nil {
			t.Errorf("%s must be refused", u)
		}
	}
}

func TestParsesOpenGraphAndFallbacks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch r.URL.Path {
		case "/og":
			_, _ = w.Write([]byte(`<html><head>
				<meta property="og:title" content="Park &amp; Kino">
				<meta property="og:description" content="Treffpunkt um 20 Uhr">
				<meta property="og:site_name" content="Stadtblog">
				<meta property="og:image" content="/img/cover.png">
				<title>ignored</title></head><body>x</body></html>`))
		case "/plain":
			_, _ = w.Write([]byte(`<html><head><title> Nur ein Titel </title>
				<meta name="description" content="Beschreibung"></head></html>`))
		case "/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"title":"no"}`))
		}
	}))
	defer srv.Close()
	f := New()
	f.allowPrivate = true

	p, err := f.Preview(context.Background(), srv.URL+"/og")
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Park & Kino" || p.Description != "Treffpunkt um 20 Uhr" || p.SiteName != "Stadtblog" {
		t.Fatalf("og fields: %+v", p)
	}
	if p.Image != srv.URL+"/img/cover.png" {
		t.Fatalf("relative og:image not resolved: %q", p.Image)
	}

	p, err = f.Preview(context.Background(), srv.URL+"/plain")
	if err != nil || p.Title != "Nur ein Titel" || p.Description != "Beschreibung" {
		t.Fatalf("fallbacks: %+v %v", p, err)
	}

	if _, err := f.Preview(context.Background(), srv.URL+"/json"); err == nil {
		t.Fatal("non-HTML must not produce a preview")
	}
}

func TestImageProxyOnlyReturnsImages(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".png") {
			_, _ = w.Write(png)
			return
		}
		w.Header().Set("Content-Type", "image/png") // lies: it is HTML
		_, _ = w.Write([]byte(`<script>alert(1)</script>`))
	}))
	defer srv.Close()
	f := New()
	f.allowPrivate = true

	img, mime, err := f.Image(context.Background(), srv.URL+"/a.png")
	if err != nil || mime != "image/png" || len(img) != len(png) {
		t.Fatalf("png: %v %q %d", err, mime, len(img))
	}
	if _, _, err := f.Image(context.Background(), srv.URL+"/fake"); err == nil {
		t.Fatal("non-image body served as image")
	}
}

func TestCachesResults(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<title>x</title>`))
	}))
	defer srv.Close()
	f := New()
	f.allowPrivate = true
	for i := 0; i < 3; i++ {
		if _, err := f.Preview(context.Background(), srv.URL); err != nil {
			t.Fatal(err)
		}
	}
	if hits != 1 {
		t.Fatalf("fetched %d times, want 1", hits)
	}
}

func TestCapsConcurrentFetches(t *testing.T) {
	var inFlight, peak atomic.Int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		<-release
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<title>x</title>`))
	}))
	defer srv.Close()
	f := New()
	f.allowPrivate = true

	// Distinct URLs, so neither the cache nor the in-flight dedup applies.
	var wg sync.WaitGroup
	for i := 0; i < 3*maxConcurrentFetches; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = f.Preview(context.Background(), srv.URL+"/?p="+strconv.Itoa(i))
		}()
	}
	for inFlight.Load() < maxConcurrentFetches {
		runtime.Gosched()
	}
	close(release)
	wg.Wait()
	if got := peak.Load(); got != maxConcurrentFetches {
		t.Fatalf("peak concurrent fetches %d, want %d", got, maxConcurrentFetches)
	}
}

func TestCapsFetchesPerRequester(t *testing.T) {
	var inFlight, peak atomic.Int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("who") == "slow" {
			n := inFlight.Add(1)
			defer inFlight.Add(-1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			<-release
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<title>x</title>`))
	}))
	defer srv.Close()
	f := New()
	f.allowPrivate = true

	// One member opens many slow links at once ...
	slow := WithRequester(context.Background(), "slow")
	var wg sync.WaitGroup
	for i := 0; i < maxConcurrentFetches; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = f.Preview(slow, srv.URL+"/?who=slow&p="+strconv.Itoa(i))
		}()
	}
	for inFlight.Load() < maxFetchesPerRequester {
		runtime.Gosched()
	}
	// ... and another member still gets a preview meanwhile.
	if _, err := f.Preview(WithRequester(context.Background(), "other"), srv.URL+"/?who=other"); err != nil {
		t.Fatalf("other member starved: %v", err)
	}
	close(release)
	wg.Wait()
	if got := peak.Load(); got != maxFetchesPerRequester {
		t.Fatalf("peak fetches of one member %d, want %d", got, maxFetchesPerRequester)
	}
}

func TestBusyIsNotCached(t *testing.T) {
	old := slotWait
	slotWait = 50 * time.Millisecond
	defer func() { slotWait = old }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<title>x</title>`))
	}))
	defer srv.Close()
	f := New()
	f.allowPrivate = true

	// Every server-wide slot taken: the fetch gives up without a preview.
	for i := 0; i < maxConcurrentFetches; i++ {
		f.slots <- struct{}{}
	}
	if _, err := f.Preview(context.Background(), srv.URL); !errors.Is(err, errBusy) {
		t.Fatalf("got %v, want errBusy", err)
	}
	for i := 0; i < maxConcurrentFetches; i++ {
		<-f.slots
	}
	// Once a slot is free the same link must be fetched, not answered from
	// a cached failure.
	if _, err := f.Preview(context.Background(), srv.URL); err != nil {
		t.Fatalf("busy result was cached: %v", err)
	}
}
