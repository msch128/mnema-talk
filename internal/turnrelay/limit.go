package turnrelay

import (
	"sync"
	"time"
)

// bucket is a token bucket.
type bucket struct {
	tokens float64
	last   time.Time
}

// rateLimiter keeps one token bucket per key (a source IP) and forgets idle
// keys, so a flood of distinct sources cannot grow it without bound.
type rateLimiter struct {
	mu      sync.Mutex
	rate    float64 // tokens per second
	burst   float64
	maxKeys int
	m       map[string]*bucket
}

func newRateLimiter(perMinute, burst, maxKeys int) *rateLimiter {
	return &rateLimiter{rate: float64(perMinute) / 60, burst: float64(burst), maxKeys: maxKeys, m: map[string]*bucket{}}
}

// allow takes one token of key's bucket.
func (l *rateLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.m[key]
	if !ok {
		if len(l.m) >= l.maxKeys {
			l.prune(now)
			if len(l.m) >= l.maxKeys {
				return false // saturated by distinct sources: refuse rather than grow
			}
		}
		b = &bucket{tokens: l.burst, last: now}
		l.m[key] = b
	}
	b.tokens = min(l.burst, b.tokens+now.Sub(b.last).Seconds()*l.rate)
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// available reports whether key still has a token, without taking it.
func (l *rateLimiter) available(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.m[key]
	return !ok || b.tokens+now.Sub(b.last).Seconds()*l.rate >= 1
}

// prune drops the buckets that have refilled completely: forgetting them
// changes nothing.
func (l *rateLimiter) prune(now time.Time) {
	for k, b := range l.m {
		if b.tokens+now.Sub(b.last).Seconds()*l.rate >= l.burst {
			delete(l.m, k)
		}
	}
}

// byteBudget is a token bucket for one allocation, in bytes. Its zero value
// is unusable: use newByteBudget.
type byteBudget struct {
	mu     sync.Mutex
	tokens float64
	rate   float64
	burst  float64
	last   time.Time
}

func newByteBudget(bytesPerSecond, burst int, now time.Time) *byteBudget {
	return &byteBudget{tokens: float64(burst), rate: float64(bytesPerSecond), burst: float64(burst), last: now}
}

func (b *byteBudget) allow(n int, now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tokens = min(b.burst, b.tokens+now.Sub(b.last).Seconds()*b.rate)
	b.last = now
	if b.tokens < float64(n) {
		return false
	}
	b.tokens -= float64(n)
	return true
}
