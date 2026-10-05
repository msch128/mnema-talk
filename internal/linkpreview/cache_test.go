package linkpreview

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDenyKeepsAddressesWhenLookupFails(t *testing.T) {
	f := New()
	fail := false
	f.lookup = func(_ context.Context, host string) ([]netip.Addr, error) {
		if fail {
			return nil, errors.New("temporary DNS failure")
		}
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	}
	own := net.ParseIP("93.184.216.34")

	f.Deny(context.Background(), "chat.example.com", "198.51.100.7")
	if !f.isDenied(own) {
		t.Fatal("resolved address not denied")
	}

	fail = true
	f.Deny(context.Background(), "chat.example.com", "198.51.100.7")
	if !f.isDenied(own) {
		t.Fatal("a failed lookup dropped the server's own address from the deny list")
	}
	if !f.isDenied(net.ParseIP("198.51.100.7")) {
		t.Fatal("literal IP lost")
	}

	// A host that is no longer listed is dropped.
	fail = false
	f.Deny(context.Background(), "198.51.100.7")
	if f.isDenied(own) {
		t.Fatal("address of a host removed from the list is still denied")
	}

	// A successful lookup replaces the old addresses (the IP changed).
	f.lookup = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.35")}, nil
	}
	f.Deny(context.Background(), "chat.example.com")
	if f.isDenied(own) || !f.isDenied(net.ParseIP("93.184.216.35")) {
		t.Fatal("new address not picked up")
	}
}

func TestConcurrentPreviewsFetchOnce(t *testing.T) {
	var hits atomic.Int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		<-release
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<title>shared</title>`))
	}))
	defer srv.Close()
	f := New()
	f.allowPrivate = true

	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := f.Preview(context.Background(), srv.URL)
			if err == nil && p.Title != "shared" {
				err = errors.New("wrong title " + p.Title)
			}
			errs <- err
		}()
	}
	time.Sleep(50 * time.Millisecond) // let the callers pile up on the first fetch
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("origin fetched %d times, want 1", n)
	}
}

func TestImagesAreCached(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write(png)
	}))
	defer srv.Close()
	f := New()
	f.allowPrivate = true
	for range 3 {
		if _, mime, err := f.Image(context.Background(), srv.URL+"/a.png"); err != nil || mime != "image/png" {
			t.Fatalf("%v %q", err, mime)
		}
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("image fetched %d times, want 1", n)
	}
}

func TestImageCacheIsBoundedAndExpires(t *testing.T) {
	now := time.Unix(0, 0)
	c := newImageCache(10, time.Minute)
	c.now = func() time.Time { return now }

	c.put("a", make([]byte, 4), "image/png")
	c.put("b", make([]byte, 4), "image/png")
	if _, _, ok := c.get("a"); !ok { // a is now most recently used
		t.Fatal("a missing")
	}
	c.put("c", make([]byte, 4), "image/png") // 12 bytes > 10: evicts b (LRU)
	if _, _, ok := c.get("b"); ok {
		t.Fatal("least recently used entry not evicted")
	}
	if _, _, ok := c.get("a"); !ok {
		t.Fatal("recently used entry evicted")
	}
	if c.bytes > 10 {
		t.Fatalf("cache holds %d bytes, limit 10", c.bytes)
	}

	c.put("huge", make([]byte, 11), "image/png")
	if _, _, ok := c.get("huge"); ok {
		t.Fatal("entry larger than the whole cache stored")
	}

	now = now.Add(2 * time.Minute)
	if _, _, ok := c.get("a"); ok {
		t.Fatal("expired entry served")
	}
}
