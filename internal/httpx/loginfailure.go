package httpx

import (
	"strings"
	"sync"
	"time"
)

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

// CapLockout limits every lockout tier to at most max, so an attacker cannot
// keep a key locked for long by tripping it repeatedly.
func (l *LoginFailureLimiter) CapLockout(max time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i, d := range l.schedule {
		if d > max {
			l.schedule[i] = max
		}
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

// Clear forgets every key for which match reports true (failures, lockouts
// and tier) and returns how many were forgotten. Keys reach match lower-cased.
func (l *LoginFailureLimiter) Clear(match func(key string) bool) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return clearMatching(l.entries, match)
}

// LockedKeys lists the keys that are locked out right now.
func (l *LoginFailureLimiter) LockedKeys() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	var keys []string
	for k, e := range l.entries {
		locked, _ := l.refreshLocked(e, now)
		if locked {
			keys = append(keys, k)
		}
	}
	return keys
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
	dropWhere(l.entries, func(_ string, e *loginFailureEntry) bool {
		locked, _ := l.refreshLocked(e, now)
		return !locked && now.Sub(e.lastSeen) >= l.decay
	})
}

// makeRoomLocked enforces the hard cap before a new key is inserted. It evicts
// the stalest entry of a random sample, preferring entries that are not
// locked, so flooding the map with fresh keys does not cheaply lift a lockout.
func (l *LoginFailureLimiter) makeRoomLocked(now time.Time) {
	if len(l.entries) < l.maxEntries {
		return
	}
	evictSampled(l.entries,
		func(_ string, e *loginFailureEntry) evictScore {
			locked, _ := l.refreshLocked(e, now)
			return evictScore{locked: locked, seen: e.lastSeen}
		},
		func(c, v evictScore) bool {
			// An unlocked entry beats a locked one; otherwise the staler wins.
			if c.locked != v.locked {
				return v.locked
			}
			return c.seen.Before(v.seen)
		})
}

// evictScore ranks eviction candidates of the login limiter.
type evictScore struct {
	locked bool
	seen   time.Time
}
