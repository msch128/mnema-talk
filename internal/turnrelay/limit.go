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

// rateLimiter keeps one token bucket per key and forgets idle keys, so it
// never holds more than maxKeys. What happens to a new key when every slot
// holds a bucket that is still drawing down depends on who can make keys:
//   - newRateLimiter refuses it. For keys anyone can make (a source address,
//     which UDP lets a sender forge) growing would hand the memory to the
//     flood and evicting would let it erase real counts, so it suits only a
//     caller to whom a refused unknown key costs nothing: failLimit records
//     failures with allow and asks available, which is true for a key it does
//     not hold, so a table full of forged sources stops counting new failures
//     but refuses nobody.
//   - newMemberLimiter forgets the fullest bucket and admits it. Its keys are
//     members who proved a credential, which nobody can make up, so a full
//     table means maxKeys members at once, and a member must not be refused
//     because of the others; the forgotten bucket had lost the least.
type rateLimiter struct {
	mu      sync.Mutex
	rate    float64 // tokens per second
	burst   float64
	maxKeys int
	evict   bool
	m       map[string]*bucket
}

func newRateLimiter(perMinute, burst, maxKeys int) *rateLimiter {
	return &rateLimiter{rate: float64(perMinute) / 60, burst: float64(burst), maxKeys: maxKeys, m: map[string]*bucket{}}
}

func newMemberLimiter(perMinute, burst, maxKeys int) *rateLimiter {
	l := newRateLimiter(perMinute, burst, maxKeys)
	l.evict = true
	return l
}

// allow takes one token of key's bucket.
func (l *rateLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.m[key]
	if !ok {
		if !l.makeRoom(now) {
			return false // saturated by distinct sources: refuse rather than grow
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

// makeRoom frees a slot for a new key; false when the table stays full.
func (l *rateLimiter) makeRoom(now time.Time) bool {
	if len(l.m) < l.maxKeys {
		return true
	}
	l.prune(now)
	if len(l.m) < l.maxKeys {
		return true
	}
	if !l.evict {
		return false
	}
	fullest, most := "", -1.0
	for k, b := range l.m {
		if t := b.tokens + now.Sub(b.last).Seconds()*l.rate; t > most {
			fullest, most = k, t
		}
	}
	delete(l.m, fullest)
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
