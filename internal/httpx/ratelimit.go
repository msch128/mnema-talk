package httpx

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
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
					WriteRateLimited(w, retry)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// WriteRateLimited answers 429 with Retry-After.
func WriteRateLimited(w http.ResponseWriter, retry time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
	WriteError(w, ErrRateLimited())
}

// LoginFailureLimiter (adopted from mnema.xyz) locks a key after maxFailures
// consecutive failures, with an escalating lockout: each repeated breach uses
// the next, longer tier of the schedule. After a quiet period (decay) without
// failures and outside a lockout, the key starts over at the first tier.
type LoginFailureLimiter struct {
	maxFailures int
	schedule    []time.Duration
	decay       time.Duration
	now         func() time.Time
	mu          sync.Mutex
	entries     map[string]*loginFailureEntry
	maxEntries  int
	lastSweep   time.Time
}

type loginFailureEntry struct {
	count    int
	lockedAt time.Time
	lockouts int
	lastSeen time.Time
}

// NewLoginFailureLimiter escalates base → 5×base → 30×base; the tier is
// forgotten after 60×base without failures.
func NewLoginFailureLimiter(maxFailures int, base time.Duration) *LoginFailureLimiter {
	return &LoginFailureLimiter{
		maxFailures: maxFailures,
		schedule:    []time.Duration{base, 5 * base, 30 * base},
		decay:       60 * base,
		now:         time.Now,
		entries:     make(map[string]*loginFailureEntry),
		maxEntries:  defaultMaxEntries,
	}
}

func (l *LoginFailureLimiter) lockoutFor(n int) time.Duration {
	idx := n - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(l.schedule) {
		idx = len(l.schedule) - 1
	}
	return l.schedule[idx]
}

// refreshLocked ends an elapsed lockout (keeping its tier, so a renewed attack
// escalates further) and forgets the tier once the key has been quiet for the
// decay period. It reports whether e is locked at now, and for how long.
func (l *LoginFailureLimiter) refreshLocked(e *loginFailureEntry, now time.Time) (bool, time.Duration) {
	if !e.lockedAt.IsZero() {
		remaining := l.lockoutFor(e.lockouts) - now.Sub(e.lockedAt)
		if remaining > 0 {
			return true, remaining
		}
		e.lockedAt = time.Time{}
		e.count = 0
	}
	if now.Sub(e.lastSeen) >= l.decay {
		e.count = 0
		e.lockouts = 0
	}
	return false, 0
}

// IsLockedOut reports whether key is locked and for how long.
func (l *LoginFailureLimiter) IsLockedOut(key string) (bool, time.Duration) {
	key = strings.ToLower(key)
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[key]
	if !ok {
		return false, 0
	}
	return l.refreshLocked(e, l.now())
}

// RecordFailure counts a failure; crossing the threshold starts a lockout.
// Failures during an active lockout are not counted again.
func (l *LoginFailureLimiter) RecordFailure(key string) {
	l.RecordFailures(key, 1)
}

// RecordFailures counts n failures at once (a weighted failure), e.g. to
// tighten a per-client limit while the whole account is under attack.
func (l *LoginFailureLimiter) RecordFailures(key string, n int) {
	key = strings.ToLower(key)
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweepLocked(now)
	e, ok := l.entries[key]
	if !ok {
		l.makeRoomLocked(now)
		e = &loginFailureEntry{lastSeen: now}
		l.entries[key] = e
	}
	locked, _ := l.refreshLocked(e, now)
	e.lastSeen = now
	if locked {
		return
	}
	e.count += n
	if e.count >= l.maxFailures {
		e.lockouts++
		e.lockedAt = now
	}
}

// ResetFailures clears key after a successful login.
func (l *LoginFailureLimiter) ResetFailures(key string) {
	l.mu.Lock()
	delete(l.entries, strings.ToLower(key))
	l.mu.Unlock()
}

// Len reports how many keys are tracked.
func (l *LoginFailureLimiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.entries)
}

// sweepLocked forgets, at most once per sweepInterval, entries that are not
// locked and have been quiet for the decay period (they would start over
// anyway).
func (l *LoginFailureLimiter) sweepLocked(now time.Time) {
	if now.Sub(l.lastSweep) < sweepInterval {
		return
	}
	l.lastSweep = now
	for k, e := range l.entries {
		if locked, _ := l.refreshLocked(e, now); !locked && now.Sub(e.lastSeen) >= l.decay {
			delete(l.entries, k)
		}
	}
}

// makeRoomLocked enforces the hard cap before a new key is inserted. It evicts
// the stalest entry of a random sample, preferring entries that are not
// locked, so flooding the map with fresh keys does not cheaply lift a lockout.
func (l *LoginFailureLimiter) makeRoomLocked(now time.Time) {
	if len(l.entries) < l.maxEntries {
		return
	}
	var victim string
	var victimLocked bool
	var victimSeen time.Time
	n := 0
	for k, e := range l.entries {
		locked, _ := l.refreshLocked(e, now)
		if n == 0 || (victimLocked && !locked) || (victimLocked == locked && e.lastSeen.Before(victimSeen)) {
			victim, victimLocked, victimSeen = k, locked, e.lastSeen
		}
		if n++; n >= evictSample {
			break
		}
	}
	delete(l.entries, victim)
}
