package update

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type edgeUpdateTransport func(*http.Request) (*http.Response, error)

func (f edgeUpdateTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type edgeUpdateBody struct {
	io.Reader
	close func()
}

func (b edgeUpdateBody) Close() error {
	if b.close != nil {
		b.close()
	}
	return nil
}

type edgeUpdateReadFailure struct{}

func (edgeUpdateReadFailure) Read([]byte) (int, error) {
	return 0, errors.New("test-only upstream body error")
}

func TestCheckerFollowsOnlyBoundedSameOriginRedirects(t *testing.T) {
	for _, loop := range []bool{false, true} {
		t.Run(strconv.FormatBool(loop), func(t *testing.T) {
			srv, hits := github(t, func(w http.ResponseWriter, r *http.Request) {
				if loop || r.URL.Path != "/release" {
					http.Redirect(w, r, "/release", http.StatusFound)
					return
				}
				jsonReply(releaseJSON)(w, r)
			})
			c := NewChecker(srv.URL+"/start", "0.3.0")
			err := c.Check(context.Background())
			if loop {
				if err == nil || hits.Load() != 3 || c.Snapshot().Latest != nil {
					t.Fatalf("redirect loop: err=%v hits=%d snapshot=%+v", err, hits.Load(), c.Snapshot())
				}
			} else if err != nil || hits.Load() != 2 || c.Snapshot().Latest == nil {
				t.Fatalf("same-origin redirect: err=%v hits=%d snapshot=%+v", err, hits.Load(), c.Snapshot())
			}
		})
	}
}

func TestCheckerRunStopsAfterCompletedOrFailedCheck(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(strconv.FormatBool(fail), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var hits atomic.Int32
			c := NewChecker(DefaultReleaseURL, "0.3.0")
			c.client.Transport = edgeUpdateTransport(func(r *http.Request) (*http.Response, error) {
				hits.Add(1)
				if fail {
					cancel()
					return nil, context.Canceled
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": {"application/json"}},
					Body:       edgeUpdateBody{Reader: strings.NewReader(releaseJSON), close: cancel},
					Request:    r,
				}, nil
			})
			done := make(chan struct{})
			go func() { c.Run(ctx, 0); close(done) }()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("background checker did not stop after cancellation")
			}
			s := c.Snapshot()
			if hits.Load() != 1 {
				t.Fatalf("performed %d checks, want exactly one before cancellation", hits.Load())
			}
			if fail {
				if s.Error != context.Canceled.Error() || s.Latest != nil {
					t.Fatalf("failed cancelled check snapshot=%+v", s)
				}
			} else if s.Latest == nil || s.Latest.Version != "0.4.0" || s.Error != "" {
				t.Fatalf("completed check was lost on cancellation: %+v", s)
			}
		})
	}
}

func TestCheckerRunCancelledBeforeInitialDelayDoesNotFetch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := NewChecker(DefaultReleaseURL, "0.3.0")
	c.client.Transport = edgeUpdateTransport(func(*http.Request) (*http.Response, error) {
		t.Error("cancelled background checker performed an HTTP request")
		return nil, context.Canceled
	})
	c.Run(ctx, time.Hour)
	if s := c.Snapshot(); !s.CheckedAt.IsZero() || s.Latest != nil || s.Error != "" {
		t.Fatalf("cancelled-before-start checker changed state: %+v", s)
	}
}

func TestCheckerReportsRequestAndBodyFailuresWithoutPublishingRelease(t *testing.T) {
	t.Run("invalid configured URL", func(t *testing.T) {
		c := NewChecker("http://%invalid", "0.3.0")
		if err := c.Check(context.Background()); err == nil || !strings.Contains(err.Error(), "build request") {
			t.Fatalf("malformed configured release URL: %v", err)
		}
		if c.Snapshot().Latest != nil {
			t.Fatal("malformed release URL published a release")
		}
	})
	t.Run("body read failure", func(t *testing.T) {
		c := NewChecker(DefaultReleaseURL, "0.3.0")
		closed := false
		c.client.Transport = edgeUpdateTransport(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
				Body: edgeUpdateBody{Reader: edgeUpdateReadFailure{}, close: func() { closed = true }}, Request: r}, nil
		})
		if err := c.Check(context.Background()); err == nil || err.Error() != "reading GitHub's answer failed" {
			t.Fatalf("body error should be sanitized: %v", err)
		}
		if !closed || c.Snapshot().Latest != nil {
			t.Fatalf("body closed=%v snapshot=%+v", closed, c.Snapshot())
		}
	})
}

func TestCheckerExpiredOrInvalidRateLimitResetUsesDefaultBackoff(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	for _, reset := range []string{strconv.FormatInt(now.Add(-time.Minute).Unix(), 10), "not-a-time"} {
		t.Run(reset, func(t *testing.T) {
			srv, hits := github(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.Header().Set("X-RateLimit-Reset", reset)
				w.WriteHeader(http.StatusTooManyRequests)
			})
			c := NewChecker(srv.URL, "0.3.0")
			c.now = func() time.Time { return now }
			if err := c.Check(context.Background()); err == nil {
				t.Fatal("rate-limited response accepted")
			}
			if got := c.Snapshot().RetryAt; !got.Equal(now.Add(defaultBackoff)) {
				t.Fatalf("retry=%v, want %v", got, now.Add(defaultBackoff))
			}
			if err := c.Check(context.Background()); !errors.Is(err, ErrBackoff) || hits.Load() != 1 {
				t.Fatalf("backoff bypassed: err=%v hits=%d", err, hits.Load())
			}
		})
	}
}

func TestCheckerClampsExcessiveRetryAfter(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	srv, hits := github(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", strconv.FormatInt(int64((maxBackoff+time.Hour)/time.Second), 10))
		w.WriteHeader(http.StatusTooManyRequests)
	})
	c := NewChecker(srv.URL, "0.3.0")
	c.now = func() time.Time { return now }
	if err := c.Check(context.Background()); err == nil {
		t.Fatal("rate-limited response accepted")
	}
	if retry := c.Snapshot().RetryAt; !retry.Equal(now.Add(maxBackoff)) {
		t.Fatalf("excessive server backoff should be capped: %v", retry)
	}
	if err := c.Check(context.Background()); !errors.Is(err, ErrBackoff) || hits.Load() != 1 {
		t.Fatalf("clamped backoff bypassed: err=%v hits=%d", err, hits.Load())
	}
}

func TestPrereleaseFormattingAndOrdering(t *testing.T) {
	a, ok := ParseSemver("v1.2.3-rc.1")
	if !ok || a.String() != "1.2.3-rc.1" {
		t.Fatalf("prerelease round trip: %+v parsed=%v", a, ok)
	}
	b, ok := ParseSemver("1.2.3-rc.2")
	if !ok || a.Compare(b) != -1 || b.Compare(a) != 1 || a.Compare(a) != 0 {
		t.Fatalf("prerelease ordering: a=%+v b=%+v parsed=%v", a, b, ok)
	}
	if !IsNewer("1.2.3-rc.2", "1.2.3-rc.1") || IsNewer("1.2.3-rc.1", "1.2.3-rc.2") {
		t.Fatal("prerelease ordering was not applied to update status")
	}
}

func TestImageTrackingTagCannotPromiseAnInvalidLatestRelease(t *testing.T) {
	for _, image := range []string{"ghcr.io/example/mnema:0", "ghcr.io/example/mnema:0.6"} {
		if reach, reason := ImageReach(image, "dev"); reach != ReachUnknown || reason != ReasonCustomTag {
			t.Fatalf("%q invalid latest: reach=%q reason=%q", image, reach, reason)
		}
	}
}
