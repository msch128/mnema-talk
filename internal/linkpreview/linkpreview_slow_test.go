package linkpreview

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A host that holds the connection past the fetch deadline costs one deadline,
// not one per request: it is not fetched again for a while, and other hosts
// keep their fetch slots.
func TestSlowHostIsNotFetchedAgain(t *testing.T) {
	old := fetchTimeout
	fetchTimeout = 150 * time.Millisecond
	defer func() { fetchTimeout = old }()

	var hits atomic.Int32
	release := make(chan struct{})
	defer close(release)
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer slow.Close()
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<title>fast</title>`))
	}))
	defer fast.Close()
	f := New()
	f.allowPrivate = true

	if _, err := f.Preview(context.Background(), slow.URL+"/a"); err == nil || errors.Is(err, errSlowHost) {
		t.Fatalf("first fetch: %v, want a timeout", err)
	}
	start := time.Now()
	if _, err := f.Preview(context.Background(), slow.URL+"/b"); !errors.Is(err, errSlowHost) {
		t.Fatalf("second fetch: %v, want errSlowHost", err)
	}
	if time.Since(start) > fetchTimeout/2 || hits.Load() != 1 {
		t.Fatalf("slow host was contacted again (hits %d, took %v)", hits.Load(), time.Since(start))
	}
	if _, err := f.Preview(context.Background(), strings.Replace(fast.URL, "127.0.0.1", "localhost", 1)); err != nil {
		t.Fatalf("other host starved: %v", err)
	}
}

func TestSlowHostEntryExpiresAndTableIsBounded(t *testing.T) {
	var s slowHosts
	s.noteTimeout("a.example", context.DeadlineExceeded)
	s.noteTimeout("b.example", errors.New("connection refused"))
	if !s.is("a.example") || s.is("b.example") {
		t.Fatal("only a timeout marks a host")
	}
	s.mu.Lock()
	s.m["a.example"] = time.Now().Add(-time.Second)
	s.mu.Unlock()
	if s.is("a.example") {
		t.Fatal("entry must expire")
	}
	for i := 0; i < slowHostMax+10; i++ {
		s.noteTimeout(strings.Repeat("h", i+1), context.DeadlineExceeded)
	}
	if len(s.m) > slowHostMax {
		t.Fatalf("table grew to %d", len(s.m))
	}
}
