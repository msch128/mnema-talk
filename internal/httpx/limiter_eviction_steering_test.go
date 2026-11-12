package httpx

import (
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"testing"
	"time"
)

// Oracle for inventory I6 rows 1 and 2: "flooding the limiter / login-failure
// table with fresh keys evicts established entries; a victim's failure count or
// a throttled key may be reset. The existing tests (TestRateLimiterHardCap,
// TestLoginFailureLimiterSweepAndCap, TestLimiterCapsRemainBoundedBeyondEviction
// Sample) assert only the SIZE and that the newest key survives; none asserts
// what happens to an established victim under eviction pressure."
//
// These tests measure, over many trials, two questions the size tests leave open:
//
//  P1 (defended): can a flood evict a key that is currently LOCKED OUT (login
//     failure limiter prefers unlocked victims)? The design comment on
//     makeRoomLocked claims it cannot be cheaply done.
//  P2 (the gap):  can a flood evict a key that holds an established *partial*
//     count but is not yet locked / not yet over its limit, resetting it?
//
// Each trial counts the perturbation it performed (fresh inserts at the cap =
// evictions attempted) so the sweep cannot pass by doing nothing.
//
// Smoke uses a small cap so the O(cap) fills run in well under a minute; set
// RL_FLOOD_DEEP=1 for the production cap (defaultMaxEntries = 65536). The shape
// is scale-invariant: with a random sample of evictSample out of cap entries,
// the stalest unlocked victim is expected to be evicted after about cap/
// evictSample fresh inserts.

func floodCap(t *testing.T) int {
	t.Helper()
	if os.Getenv("RL_FLOOD_DEEP") == "1" {
		return defaultMaxEntries
	}
	if v := os.Getenv("RL_FLOOD_CAP"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("RL_FLOOD_CAP: %v", err)
		}
		return n
	}
	return 4096
}

// TestLoginFailureLockedVictimSurvivesFlood is P1: a locked key must not be
// evicted by fresh-key pressure. This encodes the makeRoomLocked design intent
// as an assertion (today only its size is tested). It is the "defended" half.
func TestLoginFailureLockedVictimSurvivesFlood(t *testing.T) {
	cap := floodCap(t)
	// The clock is held fixed inside the lockout window for the whole flood, so
	// the victim stays locked throughout and this measures pure eviction
	// resistance (not expiry). A long base keeps the tier-1 lockout far longer
	// than any wall-clock the flood takes.
	now := time.Unix(1_000_000, 0)
	l := NewLoginFailureLimiter(10, time.Hour)
	l.now = func() time.Time { return now }
	l.maxEntries = cap

	// Victim: locked out right now (10 failures => locked at tier 1).
	l.RecordFailures("victim", 10)
	if locked, _ := l.IsLockedOut("victim"); !locked {
		t.Fatal("setup: victim should be locked")
	}

	// Fill to the cap with fresh unlocked keys, then flood well past cap/sample.
	inserts := 0
	for i := 0; l.Len() < cap; i++ {
		l.RecordFailures(fmt.Sprintf("fill-%d", i), 0)
		inserts++
	}
	floods := 40 * (cap / evictSample) // ~40x the expected-eviction horizon
	for i := 0; i < floods; i++ {
		l.RecordFailures(fmt.Sprintf("flood-%d", i), 0)
		inserts++
	}
	t.Logf("P1 cap=%d inserts=%d floods=%d victimLocked=%v", cap, inserts, floods, func() bool { lk, _ := l.IsLockedOut("victim"); return lk }())
	if locked, _ := l.IsLockedOut("victim"); !locked {
		t.Fatalf("locked victim was evicted/reset by a flood of %d fresh keys: "+
			"the lockout protection is not durable under eviction pressure", floods)
	}
}

// TestLoginFailurePartialVictimEvictedByFlood is P2: a key below its lockout
// threshold (an established partial count) is a preferred eviction victim and a
// flood resets it. This is the measured gap. The account-wide guess cap
// (accountFailures, threshold 100) is exactly such an unlocked, long-lived
// entry while an attack is building toward a lockout.
func TestLoginFailurePartialVictimEvictedByFlood(t *testing.T) {
	cap := floodCap(t)
	rng := rand.New(rand.NewSource(1))
	trials := 24
	if cap >= defaultMaxEntries {
		trials = 3
	}
	evicted := 0
	totalInserts := 0
	for tr := 0; tr < trials; tr++ {
		now := time.Unix(1_000_000+int64(tr)*10_000, 0)
		l := NewLoginFailureLimiter(100, time.Minute)
		l.now = func() time.Time { return now }
		l.maxEntries = cap

		// Victim: 99 of 100 failures, established earliest (stalest lastSeen),
		// NOT locked. One more failure would lock the account.
		l.RecordFailures("victim", 99)
		victimCount0 := l.entries["victim"].count
		now = now.Add(time.Second) // victim is now the stalest entry

		// Fill to cap with fresher unlocked keys.
		for i := 0; l.Len() < cap; i++ {
			l.RecordFailures(fmt.Sprintf("fill-%d-%d", tr, i), 0)
			now = now.Add(time.Millisecond)
		}
		// Flood past the expected-eviction horizon with fresh keys.
		floods := 8 * (cap / evictSample)
		reset := false
		for i := 0; i < floods; i++ {
			l.RecordFailures(fmt.Sprintf("flood-%d-%d-%d", tr, i, rng.Int()), 0)
			totalInserts++
			now = now.Add(time.Millisecond)
			e, ok := l.entries["victim"]
			if !ok || e.count < victimCount0 {
				reset = true
				break
			}
		}
		if reset {
			evicted++
		}
	}
	t.Logf("P2 cap=%d trials=%d evicted=%d totalInserts=%d (expected horizon ~cap/evictSample=%d)",
		cap, trials, evicted, totalInserts, cap/evictSample)
	// The gap: an unlocked, established, stalest victim is evictable, resetting
	// its count. If this ever stops holding (e.g. a future change protects
	// established partial counts), this test turns red and the finding is moot.
	if evicted == 0 {
		t.Fatalf("no partial victim was evicted across %d trials: the gap in "+
			"inventory I6 row 2 no longer reproduces", trials)
	}
}

// TestRateLimiterStalestVictimEvictedByFlood is the same gap for the plain
// fixed-window RateLimiter (I6 row 1): its makeRoomLocked evicts the entry with
// the earliest window reset among a sample, so an established key (older window)
// is the preferred victim and a flood resets its count, handing the victim a
// fresh budget. Only its size is asserted today.
func TestRateLimiterStalestVictimEvictedByFlood(t *testing.T) {
	cap := floodCap(t)
	rng := rand.New(rand.NewSource(2))
	trials := 24
	if cap >= defaultMaxEntries {
		trials = 3
	}
	evicted := 0
	for tr := 0; tr < trials; tr++ {
		now := time.Unix(2_000_000+int64(tr)*10_000, 0)
		rl := NewRateLimiter(5, time.Hour)
		rl.now = func() time.Time { return now }
		rl.maxEntries = cap

		// Victim: at its limit (5/5), window opened earliest -> earliest reset.
		for i := 0; i < 5; i++ {
			rl.Allow("victim")
		}
		if ok, _ := rl.Allow("victim"); ok {
			t.Fatal("setup: victim should be at its limit")
		}
		now = now.Add(time.Second) // later keys get a later reset

		for i := 0; rl.Len() < cap; i++ {
			rl.Allow(fmt.Sprintf("fill-%d-%d", tr, i))
		}
		floods := 8 * (cap / evictSample)
		for i := 0; i < floods; i++ {
			rl.Allow(fmt.Sprintf("flood-%d-%d-%d", tr, i, rng.Int()))
			if _, ok := rl.entries["victim"]; !ok {
				evicted++
				break
			}
		}
	}
	t.Logf("row1 cap=%d trials=%d evicted=%d", cap, trials, evicted)
	if evicted == 0 {
		t.Fatalf("no over-limit victim was evicted across %d trials: I6 row 1 gap gone", trials)
	}
}

// Len for the plain limiter (test helper; the login limiter already has Len).
func (rl *RateLimiter) Len() int {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return len(rl.entries)
}
