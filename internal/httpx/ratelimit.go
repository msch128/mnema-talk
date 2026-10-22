package httpx

import (
	"net/http"
	"strconv"
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
	rl.sweepLocked(now)
	e, ok := rl.entries[key]
	if !ok {
		rl.makeRoomLocked()
	}
	if !ok || !now.Before(e.reset) {
		e = &rateEntry{reset: now.Add(rl.window)}
		rl.entries[key] = e
	}
	e.count++
	return e.count <= rl.limit, e.reset.Sub(now)
}

// Exceeded reports, without charging anything, whether key has used up its
// budget in the current window, and when the window ends.
func (rl *RateLimiter) Exceeded(key string) (bool, time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := rl.now()
	e, ok := rl.entries[key]
	if !ok || !now.Before(e.reset) || e.count < rl.limit {
		return false, 0
	}
	return true, e.reset.Sub(now)
}

// TryAllow is Allow that only charges the hits it grants: a refused call
// leaves the count alone, so refused traffic cannot use up the budget of
// callers who come after it.
func (rl *RateLimiter) TryAllow(key string) (bool, time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := rl.now()
	rl.sweepLocked(now)
	e, ok := rl.entries[key]
	if !ok {
		rl.makeRoomLocked()
	}
	if !ok || !now.Before(e.reset) {
		e = &rateEntry{reset: now.Add(rl.window)}
		rl.entries[key] = e
	}
	if e.count >= rl.limit {
		return false, e.reset.Sub(now)
	}
	e.count++
	return true, e.reset.Sub(now)
}

// Clear forgets every key for which match reports true and returns how many.
func (rl *RateLimiter) Clear(match func(key string) bool) int {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	n := 0
	for k := range rl.entries {
		if match(k) {
			delete(rl.entries, k)
			n++
		}
	}
	return n
}

// sweepLocked drops expired entries at most once per sweepInterval, bounding
// memory without a background goroutine and without an O(n) scan per request.
func (rl *RateLimiter) sweepLocked(now time.Time) {
	if now.Sub(rl.lastSweep) < sweepInterval {
		return
	}
	rl.lastSweep = now
	for k, e := range rl.entries {
		if !now.Before(e.reset) {
			delete(rl.entries, k)
		}
	}
}

// makeRoomLocked enforces the hard cap before a new key is inserted: it evicts
// the entry with the earliest window reset among a random sample.
func (rl *RateLimiter) makeRoomLocked() {
	if len(rl.entries) < rl.maxEntries {
		return
	}
	var victim string
	var earliest time.Time
	n := 0
	for k, e := range rl.entries {
		if n == 0 || e.reset.Before(earliest) {
			victim, earliest = k, e.reset
		}
		if n++; n >= evictSample {
			break
		}
	}
	delete(rl.entries, victim)
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

// WriteRateLimited answers 429 with Retry-After.
// It counts as a hit of the unnamed "other" limiter; By counts its own.
func WriteRateLimited(w http.ResponseWriter, retry time.Duration) {
	metrics.RateLimitHits.With("other").Inc()
	writeRateLimited(w, retry)
}

func writeRateLimited(w http.ResponseWriter, retry time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
	WriteError(w, ErrRateLimited())
}
