package httpx

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RateLimiter is an in-memory fixed-window limiter keyed by an arbitrary string
// (client IP, user ID). Mnema Talk runs as a single instance, so process-local
// state is sufficient.
type RateLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	now     func() time.Time
	entries map[string]*rateEntry
}

type rateEntry struct {
	count int
	reset time.Time
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{limit: limit, window: window, now: time.Now, entries: make(map[string]*rateEntry)}
}

// Allow records one hit for key and reports whether it is within the limit,
// plus the time until the window resets.
func (rl *RateLimiter) Allow(key string) (bool, time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := rl.now()
	rl.sweepLocked(now)
	e, ok := rl.entries[key]
	if !ok || !now.Before(e.reset) {
		e = &rateEntry{reset: now.Add(rl.window)}
		rl.entries[key] = e
	}
	e.count++
	return e.count <= rl.limit, e.reset.Sub(now)
}

// sweepLocked drops expired entries once the map grows, bounding memory
// without a background goroutine.
func (rl *RateLimiter) sweepLocked(now time.Time) {
	if len(rl.entries) < 4096 {
		return
	}
	for k, e := range rl.entries {
		if !now.Before(e.reset) {
			delete(rl.entries, k)
		}
	}
}

// PerIP limits requests per resolved client IP.
func (rl *RateLimiter) PerIP(next http.Handler) http.Handler {
	return rl.By(ClientIP)(next)
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

// LoginFailureLimiter (adopted from mnema.xyz) locks an account key after
// maxFailures consecutive failures, with an escalating lockout: each repeated
// breach uses the next, longer tier of the schedule. It complements the per-IP
// limit by stopping distributed guessing against a single account.
type LoginFailureLimiter struct {
	maxFailures int
	schedule    []time.Duration
	now         func() time.Time
	mu          sync.Mutex
	entries     map[string]*loginFailureEntry
}

type loginFailureEntry struct {
	count    int
	lockedAt time.Time
	lockouts int
	lastSeen time.Time
}

// NewLoginFailureLimiter escalates base → 5×base → 30×base.
func NewLoginFailureLimiter(maxFailures int, base time.Duration) *LoginFailureLimiter {
	return &LoginFailureLimiter{
		maxFailures: maxFailures,
		schedule:    []time.Duration{base, 5 * base, 30 * base},
		now:         time.Now,
		entries:     make(map[string]*loginFailureEntry),
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

// IsLockedOut reports whether key is locked and for how long. An elapsed
// lockout keeps its tier so a renewed attack escalates further.
func (l *LoginFailureLimiter) IsLockedOut(key string) (bool, time.Duration) {
	key = strings.ToLower(key)
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[key]
	if !ok || e.lockedAt.IsZero() {
		return false, 0
	}
	remaining := l.lockoutFor(e.lockouts) - l.now().Sub(e.lockedAt)
	if remaining <= 0 {
		e.lockedAt = time.Time{}
		e.count = 0
		return false, 0
	}
	return true, remaining
}

// RecordFailure counts a failure; crossing the threshold starts a lockout.
func (l *LoginFailureLimiter) RecordFailure(key string) {
	key = strings.ToLower(key)
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweepLocked(now)
	e, ok := l.entries[key]
	if !ok {
		e = &loginFailureEntry{}
		l.entries[key] = e
	}
	e.lastSeen = now
	if !e.lockedAt.IsZero() {
		return
	}
	e.count++
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

// sweepLocked forgets entries idle for longer than the longest tier.
func (l *LoginFailureLimiter) sweepLocked(now time.Time) {
	if len(l.entries) < 4096 {
		return
	}
	maxTier := l.schedule[len(l.schedule)-1]
	for k, e := range l.entries {
		if now.Sub(e.lastSeen) > 2*maxTier {
			delete(l.entries, k)
		}
	}
}
