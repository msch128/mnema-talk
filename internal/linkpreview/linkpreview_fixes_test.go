package linkpreview

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestOversizedOGImageIsDropped(t *testing.T) {
	base, _ := url.Parse("https://example.com/")
	long := "https://cdn.example.com/" + strings.Repeat("a", maxURLLen)
	p := parseHTML([]byte(`<head><title>t</title><meta property="og:image" content="`+long+`">`), "text/html", base)
	if p.Image != "" {
		t.Fatalf("image kept, len %d", len(p.Image))
	}
	p = parseHTML([]byte(`<head><title>t</title><meta property="og:image" content="/i.png">`), "text/html", base)
	if p.Image != "https://example.com/i.png" {
		t.Fatalf("image %q", p.Image)
	}
}

func TestPageCharsetIsHonoured(t *testing.T) {
	base, _ := url.Parse("https://example.com/")
	latin1 := []byte("<head><title>Caf\xe9</title>")
	if p := parseHTML(latin1, "text/html; charset=iso-8859-1", base); p.Title != "Café" {
		t.Fatalf("header charset: %q", p.Title)
	}
	meta := []byte(`<head><meta charset="windows-1252"><title>Caf` + "\xe9" + `</title>`)
	if p := parseHTML(meta, "text/html", base); p.Title != "Café" {
		t.Fatalf("meta charset: %q", p.Title)
	}
	if p := parseHTML([]byte("<head><title>Café</title>"), "text/html; charset=utf-8", base); p.Title != "Café" {
		t.Fatalf("utf-8: %q", p.Title)
	}
}

func TestImageFailuresAreCached(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("not an image"))
	}))
	defer srv.Close()
	f := New()
	f.allowPrivate = true
	for range 3 {
		if _, _, err := f.Image(context.Background(), srv.URL+"/x.png"); err == nil {
			t.Fatal("want an error")
		}
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("origin fetched %d times, want 1", n)
	}
}

func TestFlightWaiterDoesNotInheritLeaderBusy(t *testing.T) {
	var g flightGroup[string]
	started := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_, _ = g.do("k", func() (string, error) {
			close(started)
			<-release
			return "", errBusy
		})
	}()
	<-started
	res := make(chan error, 1)
	go func() {
		v, err := g.do("k", func() (string, error) { return "ok", nil })
		if err == nil && v != "ok" {
			err = context.Canceled
		}
		res <- err
	}()
	time.Sleep(50 * time.Millisecond) // let the waiter join the leader's flight
	close(release)
	if err := <-res; err != nil {
		t.Fatalf("waiter got %v, want its own result", err)
	}
}

func TestCheckURLSharesSSRFShape(t *testing.T) {
	f := New()
	for _, raw := range []string{"https://example.com/a b", "https://example.com/\x00", "mailto:a@example.com"} {
		if _, err := f.parse(raw); err == nil {
			t.Errorf("%q must be refused", raw)
		}
	}
}
