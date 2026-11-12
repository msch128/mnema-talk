package turnrelay

import (
	"fmt"
	"testing"
	"time"
)

// Oracle for inventory I6 row 51: the TURN relay's allocLimit and failLimit
// each hold at most maxKeys (4096) source-address buckets. At the cap, prune()
// only reclaims buckets that have refilled to full burst; if none have, a new
// source is refused rather than admitted ("saturated by distinct sources:
// refuse rather than grow", limit.go). The existing TestRateLimiter asserts the
// refusal with maxKeys=2; this measures the production consequence: one party
// that presents maxKeys distinct (spoofable UDP) source addresses and keeps
// their buckets drawn down can hold the table full and deny the relay to every
// new source, while paying a low, steady packet rate.
//
// The perturbation is counted (distinct sources admitted, top-up packets sent)
// so the sweep cannot pass by doing nothing.

// TestAllocLimiterSaturationDeniesNewSource fills the allocation limiter with
// maxKeys distinct sources and asserts that a fresh, otherwise-legitimate
// source is then refused.
func TestAllocLimiterSaturationDeniesNewSource(t *testing.T) {
	const maxKeys = 4096
	now := time.Unix(0, 0)
	// Production defaults: 120/min, burst 60 (options.go withDefaults).
	l := newRateLimiter(120, 60, maxKeys)

	// Fill every slot with a distinct source and immediately draw each bucket
	// below full burst, so prune() cannot reclaim it.
	admitted := 0
	for i := 0; i < maxKeys; i++ {
		src := fmt.Sprintf("198.51.%d.%d", i/256, i%256)
		if !l.allow(src, now) { // first touch: bucket starts at burst, token taken
			t.Fatalf("legit-looking source %d refused before the cap", i)
		}
		admitted++
	}
	if got := len(l.m); got != maxKeys {
		t.Fatalf("table holds %d keys, want the cap %d", got, maxKeys)
	}
	// A fresh source now: the table is full and nothing has refilled to burst
	// (each was just drawn down by one token within the same instant).
	if l.allow("203.0.113.9", now) {
		t.Fatal("a new source was admitted while the table was saturated")
	}
	t.Logf("admitted=%d then new source refused at cap=%d", admitted, maxKeys)
}

// TestAllocLimiterSaturationSustainedCheaply shows the denial holds over time
// at a low packet rate: the attacker re-touches each held source before its
// bucket refills to full burst (the only condition under which prune reclaims
// it), so the table never drains. It measures the packet rate required.
func TestAllocLimiterSaturationSustainedCheaply(t *testing.T) {
	const maxKeys = 4096
	now := time.Unix(0, 0)
	l := newRateLimiter(120, 60, maxKeys) // rate 2 tok/s, burst 60

	srcs := make([]string, maxKeys)
	for i := range srcs {
		srcs[i] = fmt.Sprintf("198.51.%d.%d", i/256, i%256)
		l.allow(srcs[i], now)
	}

	// A bucket refills from drawn-down to full burst in burst/rate = 60/2 = 30s.
	// Touch every held source once per refill window to keep it un-prunable, and
	// confirm a new source stays refused throughout several windows.
	refill := time.Duration(float64(l.burst)/l.rate) * time.Second // 30s
	touchPackets := 0
	windows := 6
	for w := 0; w < windows; w++ {
		now = now.Add(refill - time.Second) // just before any bucket tops up
		for _, s := range srcs {
			l.allow(s, now) // take a token to keep it below burst
			touchPackets++
		}
		if l.allow(fmt.Sprintf("203.0.113.%d", w), now) {
			t.Fatalf("window %d: a new source was admitted; the denial did not hold", w)
		}
	}
	span := refill * time.Duration(windows)
	pps := float64(touchPackets) / span.Seconds()
	t.Logf("sustained denial over %s with %d top-up packets = %.1f packets/s to hold %d spoofable sources",
		span, touchPackets, pps, maxKeys)
	// The whole point: the sustaining cost is tiny relative to the denial.
	if pps > float64(maxKeys) {
		t.Fatalf("sustaining rate %.1f pps unexpectedly high", pps)
	}
}

// TestFailLimiterSaturationBlocksLegitCredentialCheck shows the same for the
// failure limiter: authenticate() calls failLimit.available(ip|username) and
// refuses when the source has no budget. Saturating failLimit with distinct
// spoofed source addresses makes available() refuse a legitimate source's
// credential check (its key is absent, so a saturated table has no room to
// even open a bucket on allow()). available() returns true for an absent key,
// so the denial here is via allow() at the fill path: once full, a genuine
// failed-but-present source cannot be recorded, and more importantly the
// allocation limiter (above) already gates the same clients. This test pins the
// cap and the refuse-rather-than-grow contract for failLimit.
func TestFailLimiterRefusesToGrowPastCap(t *testing.T) {
	const maxKeys = 4096
	now := time.Unix(0, 0)
	l := newRateLimiter(60, 50, maxKeys)
	for i := 0; i < maxKeys; i++ {
		l.allow(fmt.Sprintf("198.51.%d.%d|u", i/256, i%256), now)
	}
	grew := l.allow("203.0.113.1|victim", now)
	if grew {
		t.Fatal("failLimit grew past its cap")
	}
	if len(l.m) != maxKeys {
		t.Fatalf("failLimit holds %d keys, want cap %d", len(l.m), maxKeys)
	}
}
