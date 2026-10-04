package auth

import (
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// RateLimiter is a small in-memory fixed-window limiter keyed by an arbitrary
// string (client IP, username). Good enough for a single-server deployment.
type RateLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	entries map[string]*rateEntry
}

type rateEntry struct {
	count int
	reset time.Time
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{limit: limit, window: window, entries: make(map[string]*rateEntry)}
	go rl.cleanup()
	return rl
}

// Allow records one hit for key and reports whether it is still within the limit,
// plus the time until the window resets.
func (rl *RateLimiter) Allow(key string) (bool, time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	e, ok := rl.entries[key]
	if !ok || now.After(e.reset) {
		e = &rateEntry{reset: now.Add(rl.window)}
		rl.entries[key] = e
	}
	e.count++
	return e.count <= rl.limit, e.reset.Sub(now)
}

// Blocked reports whether key has already exceeded the limit, without recording a hit.
func (rl *RateLimiter) Blocked(key string) (bool, time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	e, ok := rl.entries[key]
	if !ok || time.Now().After(e.reset) {
		return false, 0
	}
	return e.count >= rl.limit, time.Until(e.reset)
}

func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(rl.window)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		for k, e := range rl.entries {
			if now.After(e.reset) {
				delete(rl.entries, k)
			}
		}
		rl.mu.Unlock()
	}
}

// Middleware rejects requests from a client IP that exceeded the limit.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ok, retry := rl.Allow(ClientIP(r)); !ok {
			WriteTooManyRequests(w, retry)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func WriteTooManyRequests(w http.ResponseWriter, retry time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
	http.Error(w, `{"error":"too many attempts, please try again later"}`, http.StatusTooManyRequests)
}

// ClientIP returns the request's remote IP (already rewritten by chi's RealIP middleware).
func ClientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
