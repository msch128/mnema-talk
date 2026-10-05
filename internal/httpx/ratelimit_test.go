package httpx

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiterSweepsAtMostOncePerInterval(t *testing.T) {
	now := time.Unix(1000, 0)
	rl := NewRateLimiter(5, time.Second)
	rl.now = func() time.Time { return now }

	rl.Allow("a")
	now = now.Add(2 * time.Second) // "a" expired, but the last sweep was just now
	rl.Allow("b")
	if _, ok := rl.entries["a"]; !ok {
		t.Fatal("swept before the sweep interval elapsed")
	}
	now = now.Add(sweepInterval)
	rl.Allow("c")
	if _, ok := rl.entries["a"]; ok {
		t.Fatal("expired entry survived the periodic sweep")
	}
}

func TestRateLimiterHardCap(t *testing.T) {
	now := time.Unix(1000, 0)
	rl := NewRateLimiter(5, time.Hour)
	rl.now = func() time.Time { return now }
	rl.maxEntries = 10
	for i := 0; i < 100; i++ {
		rl.Allow(fmt.Sprintf("k%d", i))
	}
	if len(rl.entries) > 10 {
		t.Fatalf("map grew past the cap: %d", len(rl.entries))
	}
	if _, ok := rl.entries["k99"]; !ok {
		t.Fatal("newest key must be tracked")
	}
}

func TestLoginFailureLimiterTierDecays(t *testing.T) {
	now := time.Unix(1000, 0)
	l := NewLoginFailureLimiter(2, time.Minute)
	l.now = func() time.Time { return now }
	fail := func() { l.RecordFailure("k"); l.RecordFailure("k") }

	fail()
	now = now.Add(time.Minute)
	fail()
	if _, remaining := l.IsLockedOut("k"); remaining != 5*time.Minute {
		t.Fatalf("second lockout should be 5m, got %v", remaining)
	}
	// Quiet for the decay period: the next breach starts at the first tier.
	now = now.Add(l.decay)
	if locked, _ := l.IsLockedOut("k"); locked {
		t.Fatal("still locked after decay")
	}
	fail()
	if _, remaining := l.IsLockedOut("k"); remaining != time.Minute {
		t.Fatalf("tier did not decay, lockout %v", remaining)
	}
}

func TestLoginFailureLimiterCountsDecay(t *testing.T) {
	now := time.Unix(1000, 0)
	l := NewLoginFailureLimiter(3, time.Minute)
	l.now = func() time.Time { return now }
	l.RecordFailure("k")
	l.RecordFailure("k")
	now = now.Add(l.decay)
	l.RecordFailure("k")
	if locked, _ := l.IsLockedOut("k"); locked {
		t.Fatal("stale failures counted toward a lockout")
	}
}

func TestLoginFailureLimiterWeightedFailures(t *testing.T) {
	l := NewLoginFailureLimiter(10, time.Minute)
	l.RecordFailures("k", 5)
	if locked, _ := l.IsLockedOut("k"); locked {
		t.Fatal("locked early")
	}
	l.RecordFailures("k", 5)
	if locked, _ := l.IsLockedOut("k"); !locked {
		t.Fatal("weighted failures did not lock")
	}
}

func TestLoginFailureLimiterSweepAndCap(t *testing.T) {
	now := time.Unix(1000, 0)
	l := NewLoginFailureLimiter(1, time.Minute)
	l.now = func() time.Time { return now }
	l.maxEntries = 8

	l.RecordFailure("locked") // locks immediately (maxFailures 1)
	for i := 0; i < 50; i++ {
		l.RecordFailures(fmt.Sprintf("k%d", i), 0)
	}
	if len(l.entries) > 8 {
		t.Fatalf("map grew past the cap: %d", len(l.entries))
	}
	if locked, _ := l.IsLockedOut("locked"); !locked {
		t.Fatal("eviction lifted an active lockout while unlocked entries were available")
	}

	now = now.Add(l.decay + sweepInterval)
	l.RecordFailures("fresh", 0)
	if len(l.entries) != 1 {
		t.Fatalf("sweep kept %d stale entries", len(l.entries)-1)
	}
}

func TestResetFailuresClearsTier(t *testing.T) {
	l := NewLoginFailureLimiter(1, time.Minute)
	l.RecordFailure("K")
	l.ResetFailures("k")
	if locked, _ := l.IsLockedOut("k"); locked {
		t.Fatal("reset did not clear lockout")
	}
	l.RecordFailure("k")
	if _, remaining := l.IsLockedOut("k"); remaining > time.Minute {
		t.Fatalf("tier survived reset: %v", remaining)
	}
}

func TestIPKeyMasksIPv6To64(t *testing.T) {
	cases := map[string]string{
		"203.0.113.5":                "203.0.113.5",
		"::ffff:203.0.113.5":         "203.0.113.5",
		"2001:db8:1:2:aaaa::1":       "2001:db8:1:2::/64",
		"2001:db8:1:2:ffff:ffff::99": "2001:db8:1:2::/64",
		"[2001:db8::1]":              "2001:db8::/64",
		"fe80::1%eth0":               "fe80::/64",
		"not-an-ip":                  "not-an-ip",
	}
	for in, want := range cases {
		if got := IPKey(in); got != want {
			t.Errorf("IPKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPerIPGroupsIPv6Slash64(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute)
	h := rl.PerIP(okHandler)
	do := func(remote string) int {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = remote
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec.Code
	}
	if code := do("[2001:db8:1:2::1]:1000"); code != http.StatusOK {
		t.Fatalf("first: %d", code)
	}
	if code := do("[2001:db8:1:2::2]:1000"); code != http.StatusTooManyRequests {
		t.Fatalf("same /64 must share the limit: %d", code)
	}
	if code := do("[2001:db8:1:3::1]:1000"); code != http.StatusOK {
		t.Fatalf("other /64 must be independent: %d", code)
	}
}

func TestClientIPJoinsForwardedForLines(t *testing.T) {
	trusted, _ := ParseCIDRs([]string{"172.16.0.0/12"})
	var got string
	h := ClientIPMiddleware(trusted)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = ClientIP(r) }))
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "172.21.0.1:999"
	// The client sends its own line; the proxy appends the real hop as a second line.
	r.Header.Add("X-Forwarded-For", "1.1.1.1")
	r.Header.Add("X-Forwarded-For", "198.51.100.7")
	h.ServeHTTP(httptest.NewRecorder(), r)
	if got != "198.51.100.7" {
		t.Fatalf("ClientIP = %q, want the proxy-appended hop", got)
	}
}
