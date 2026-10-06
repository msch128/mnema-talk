package update

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

const releaseJSON = `{
  "tag_name": "v0.4.0",
  "html_url": "https://github.com/msch128/mnema-talk/releases/tag/v0.4.0",
  "published_at": "2026-10-05T12:00:00Z",
  "body": "## Features\n\n* **admin:** system tab\u0007\r\n",
  "draft": false,
  "prerelease": false,
  "assets": [{"name": "ignored"}],
  "author": {"login": "github-actions[bot]"}
}`

// github fakes the releases endpoint; handler may be swapped per test.
func github(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func jsonReply(body string, hdr ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		for i := 0; i+1 < len(hdr); i += 2 {
			w.Header().Set(hdr[i], hdr[i+1])
		}
		_, _ = w.Write([]byte(body))
	}
}

func TestCheckParsesTheLatestRelease(t *testing.T) {
	var gotUA, gotAccept string
	srv, _ := github(t, func(w http.ResponseWriter, r *http.Request) {
		gotUA, gotAccept = r.Header.Get("User-Agent"), r.Header.Get("Accept")
		jsonReply(releaseJSON, "ETag", `"abc"`)(w, r)
	})
	c := NewChecker(srv.URL, "0.3.0")
	if err := c.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := c.Snapshot()
	if s.Latest == nil || s.Latest.Version != "0.4.0" || s.Error != "" || s.CheckedAt.IsZero() {
		t.Fatalf("snapshot = %+v", s)
	}
	if s.Latest.URL != "https://github.com/msch128/mnema-talk/releases/tag/v0.4.0" {
		t.Fatalf("url = %q", s.Latest.URL)
	}
	if s.Latest.Notes != "## Features\n\n* **admin:** system tab" {
		t.Fatalf("notes = %q (control characters and CR must be gone)", s.Latest.Notes)
	}
	if !s.Latest.PublishedAt.Equal(time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("published = %v", s.Latest.PublishedAt)
	}
	if !strings.HasPrefix(gotUA, "mnema-talk/0.3.0") || gotAccept != "application/vnd.github+json" {
		t.Fatalf("headers: UA %q, Accept %q", gotUA, gotAccept)
	}
}

func TestCheckUsesETag(t *testing.T) {
	srv, hits := github(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"abc"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		jsonReply(releaseJSON, "ETag", `"abc"`)(w, r)
	})
	c := NewChecker(srv.URL, "0.3.0")
	for i := 0; i < 2; i++ {
		if err := c.Check(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if hits.Load() != 2 || c.Snapshot().Latest.Version != "0.4.0" {
		t.Fatalf("hits=%d snapshot=%+v", hits.Load(), c.Snapshot())
	}
}

func TestCheckRejectsBadAnswers(t *testing.T) {
	huge := `{"tag_name":"v1.0.0","body":"` + strings.Repeat("x", maxResponseBytes) + `"}`
	cases := map[string]http.HandlerFunc{
		"malformed":     jsonReply(`{"tag_name": "v1.0.0"`),
		"trailing data": jsonReply(`{"tag_name":"v1.0.0"} {"tag_name":"v9.9.9"}`),
		"not json": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>"))
		},
		"no tag":       jsonReply(`{"name":"x"}`),
		"bad tag":      jsonReply(`{"tag_name":"latest<script>"}`),
		"draft":        jsonReply(`{"tag_name":"v1.0.0","draft":true}`),
		"oversized":    jsonReply(huge),
		"server error": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) },
		"not found":    func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) },
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			srv, _ := github(t, h)
			c := NewChecker(srv.URL, "0.3.0")
			if err := c.Check(context.Background()); err == nil {
				t.Fatal("bad answer accepted")
			}
			if s := c.Snapshot(); s.Latest != nil || s.Error == "" {
				t.Fatalf("snapshot = %+v", s)
			}
		})
	}
}

func TestCheckKeepsTheLastGoodReleaseOnFailure(t *testing.T) {
	fail := atomic.Bool{}
	srv, _ := github(t, func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		jsonReply(releaseJSON)(w, r)
	})
	c := NewChecker(srv.URL, "0.3.0")
	_ = c.Check(context.Background())
	fail.Store(true)
	if err := c.Check(context.Background()); err == nil {
		t.Fatal("want error")
	}
	if s := c.Snapshot(); s.Latest == nil || s.Latest.Version != "0.4.0" || s.Error == "" {
		t.Fatalf("snapshot = %+v", s)
	}
}

func TestCheckTimesOut(t *testing.T) {
	release := make(chan struct{})
	srv, _ := github(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	defer close(release)
	c := NewChecker(srv.URL, "0.3.0")
	c.client.Timeout = 100 * time.Millisecond
	start := time.Now()
	err := c.Check(context.Background())
	if err == nil || time.Since(start) > 5*time.Second {
		t.Fatalf("err=%v after %v", err, time.Since(start))
	}
	if !strings.Contains(c.Snapshot().Error, "in time") {
		t.Fatalf("error = %q", c.Snapshot().Error)
	}
}

func TestCheckBacksOffOnRateLimit(t *testing.T) {
	for name, hdr := range map[string][]string{
		"retry-after": {"Retry-After", "120"},
		"reset":       {"X-RateLimit-Remaining", "0", "X-RateLimit-Reset", "<reset>"},
	} {
		t.Run(name, func(t *testing.T) {
			now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
			srv, hits := github(t, func(w http.ResponseWriter, r *http.Request) {
				for i := 0; i+1 < len(hdr); i += 2 {
					v := hdr[i+1]
					if v == "<reset>" {
						v = "1791201720" // now + 120 s
					}
					w.Header().Set(hdr[i], v)
				}
				w.WriteHeader(http.StatusForbidden)
			})
			c := NewChecker(srv.URL, "0.3.0")
			c.now = func() time.Time { return now }
			if err := c.Check(context.Background()); err == nil {
				t.Fatal("want error")
			}
			if got := c.Snapshot().RetryAt; !got.Equal(now.Add(120 * time.Second)) {
				t.Fatalf("retry at %v, want %v", got, now.Add(120*time.Second))
			}
			// No request while the limit is in force.
			if err := c.Check(context.Background()); !errors.Is(err, ErrBackoff) || hits.Load() != 1 {
				t.Fatalf("err=%v hits=%d", err, hits.Load())
			}
			now = now.Add(121 * time.Second)
			_ = c.Check(context.Background())
			if hits.Load() != 2 {
				t.Fatalf("no new request after the backoff: hits=%d", hits.Load())
			}
		})
	}
}

func TestCheckNowIsThrottled(t *testing.T) {
	srv, hits := github(t, jsonReply(releaseJSON))
	c := NewChecker(srv.URL, "0.3.0")
	now := time.Now()
	c.now = func() time.Time { return now }
	if err := c.CheckNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c.CheckNow(context.Background()); !errors.Is(err, ErrTooSoon) {
		t.Fatalf("second manual check: %v", err)
	}
	now = now.Add(ManualMinGap)
	if err := c.CheckNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 2 {
		t.Fatalf("hits = %d", hits.Load())
	}
}

func TestCheckRefusesForeignRedirects(t *testing.T) {
	other := httptest.NewServer(jsonReply(releaseJSON))
	t.Cleanup(other.Close)
	srv, _ := github(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, strings.Replace(other.URL, "127.0.0.1", "localhost", 1)+"/x", http.StatusFound)
	})
	c := NewChecker(srv.URL, "0.3.0")
	if err := c.Check(context.Background()); err == nil {
		t.Fatal("followed a redirect to another host")
	}
}

func TestReleasePageOnlyLinksToTheRepository(t *testing.T) {
	v := Semver{Major: 1}
	want := "https://github.com/msch128/mnema-talk/releases/tag/v1.0.0"
	for _, raw := range []string{
		"https://evil.example/msch128/mnema-talk/releases/tag/v1.0.0",
		"http://github.com/msch128/mnema-talk/releases/tag/v1.0.0",
		"https://github.com/someone/else/releases/tag/v1.0.0",
		"javascript:alert(1)",
		"https://user@github.com/msch128/mnema-talk/releases/tag/v1.0.0",
		"",
	} {
		if got := releasePage(raw, v); got != want {
			t.Errorf("releasePage(%q) = %q", raw, got)
		}
	}
	ok := "https://github.com/msch128/mnema-talk/releases/tag/v1.0.0"
	if got := releasePage(ok, v); got != ok {
		t.Errorf("releasePage(valid) = %q", got)
	}
}

func TestCleanNotesCapsLength(t *testing.T) {
	got := cleanNotes(strings.Repeat("ä", maxNotesRunes+50))
	if n := len([]rune(got)); n > maxNotesRunes+2 {
		t.Fatalf("notes not capped: %d runes", n)
	}
}

func TestSemver(t *testing.T) {
	for _, tc := range []struct {
		latest, current string
		want            bool
	}{
		{"0.4.0", "0.3.0", true},
		{"v0.3.1", "0.3.0", true},
		{"0.3.0", "0.3.0", false},
		{"0.2.9", "0.3.0", false},
		{"1.0.0", "0.99.99", true},
		{"0.4.0", "0.4.0-rc.1", true},
		{"0.4.0-rc.1", "0.4.0", false},
		{"0.4.0", "dev", false},
		{"garbage", "0.3.0", false},
	} {
		if got := IsNewer(tc.latest, tc.current); got != tc.want {
			t.Errorf("IsNewer(%q, %q) = %v", tc.latest, tc.current, got)
		}
	}
}
