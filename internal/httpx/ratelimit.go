package httpx

import (
	"net/http"
	"sync"
	"time"

	"github.com/msch128/mnema-talk/internal/metrics"
)

const (
	// sweepInterval bounds how often a limiter scans its whole map for expired
	// entries, so the O(n) sweep never runs on every request.
	sweepInterval = time.Minute
	// defaultMaxEntries is the hard cap on tracked keys per limiter. Beyond it a
	// new key evicts the stalest entry of a small sample instead of growing the map.
	defaultMaxEntries = 65536
	// evictSample is how many entries are inspected to pick an eviction victim.
	evictSample = 32
)

// RateLimiter is an in-memory fixed-window limiter keyed by an arbitrary string
// (client IP, user ID). Mnema Talk runs as a single instance, so process-local
// state is sufficient.
type RateLimiter struct {
	mu         sync.Mutex
	limit      int
	window     time.Duration
	now        func() time.Time
	entries    map[string]*rateEntry
	maxEntries int
	lastSweep  time.Time
	name       string
}

// Named labels the limiter's refusals in the metrics and returns it.
func (rl *RateLimiter) Named(name string) *RateLimiter {
	rl.name = name
	return rl
}

type rateEntry struct {
	count int
	reset time.Time
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		limit:      limit,
		window:     window,
		now:        time.Now,
		entries:    make(map[string]*rateEntry),
		maxEntries: defaultMaxEntries,
	}
}

// Allow records one hit for key and reports whether it is within the limit,
// plus the time until the window resets.
func (rl *RateLimiter) Allow(key string) (bool, time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := rl.now()
	e := rl.entryLocked(key, now)
	e.count++
	return e.count <= rl.limit, e.reset.Sub(now)
}

// TryAllow is Allow that only charges the hits it grants: a refused call
// leaves the count alone, so refused traffic cannot use up the budget of
// callers who come after it.
func (rl *RateLimiter) TryAllow(key string) (bool, time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := rl.now()
	e := rl.entryLocked(key, now)
	if e.count >= rl.limit {
		return false, e.reset.Sub(now)
	}
	e.count++
	return true, e.reset.Sub(now)
}

// Refund gives back one hit that TryAllow granted for key, for a request that
// another limiter then refused.
func (rl *RateLimiter) Refund(key string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if e, ok := rl.entries[key]; ok && e.count > 0 {
		e.count--
	}
}

// entryLocked returns the live window of key, starting a fresh one (after a
// sweep and, at the cap, an eviction) when there is none or it has elapsed.
func (rl *RateLimiter) entryLocked(key string, now time.Time) *rateEntry {
	rl.sweepLocked(now)
	e, ok := rl.entries[key]
	if !ok {
		rl.makeRoomLocked()
	}
	if !ok || !now.Before(e.reset) {
		e = &rateEntry{reset: now.Add(rl.window)}
		rl.entries[key] = e
	}
	return e
}

// Clear forgets every key for which match reports true and returns how many.
func (rl *RateLimiter) Clear(match func(key string) bool) int {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return clearMatching(rl.entries, match)
}

// sweepLocked drops expired entries at most once per sweepInterval, bounding
// memory without a background goroutine and without an O(n) scan per request.
func (rl *RateLimiter) sweepLocked(now time.Time) {
	if now.Sub(rl.lastSweep) < sweepInterval {
		return
	}
	rl.lastSweep = now
	dropWhere(rl.entries, func(_ string, e *rateEntry) bool { return !now.Before(e.reset) })
}

// makeRoomLocked enforces the hard cap before a new key is inserted: it evicts
// the entry with the earliest window reset among a random sample.
func (rl *RateLimiter) makeRoomLocked() {
	if len(rl.entries) < rl.maxEntries {
		return
	}
	evictSampled(rl.entries,
		func(_ string, e *rateEntry) time.Time { return e.reset },
		func(c, v time.Time) bool { return c.Before(v) })
}

// PerIP limits requests per resolved client address (IPv6 per /64, see
// ClientIPKey).
func (rl *RateLimiter) PerIP(next http.Handler) http.Handler {
	return rl.By(ClientIPKey)(next)
}

// By limits requests per key; an empty key bypasses the limiter.
func (rl *RateLimiter) By(keyFn func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if key := keyFn(r); key != "" {
				if ok, retry := rl.Allow(key); !ok {
					name := rl.name
					if name == "" {
						name = "other"
					}
					metrics.RateLimitHits.With(name).Inc()
					writeRateLimited(w, retry)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// BySafeMethod applies reads to GET, HEAD and OPTIONS requests and writes to
// every other method, so cheap reads and state changes draw on separate
// budgets: browsing never uses up what a member may post.
func BySafeMethod(reads, writes func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		readsNext, writesNext := reads(next), writes(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				readsNext.ServeHTTP(w, r)
			default:
				writesNext.ServeHTTP(w, r)
			}
		})
	}
}

// WriteRateLimited answers 429 with Retry-After.
// It counts as a hit of the unnamed "other" limiter; By counts its own.
func WriteRateLimited(w http.ResponseWriter, retry time.Duration) {
	metrics.RateLimitHits.With("other").Inc()
	writeRateLimited(w, retry)
}

func writeRateLimited(w http.ResponseWriter, retry time.Duration) {
	w.Header().Set("Retry-After", retryAfterSeconds(retry))
	// The wait is also in the sentence: clients that cannot read the header
	// still tell the member how long to hold back.
	WriteError(w, NewAPIError(http.StatusTooManyRequests, CodeRateLimited, "too many requests: retry in "+retryAfterSeconds(retry)+" s"))
}
