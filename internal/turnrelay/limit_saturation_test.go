package turnrelay

import (
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

// H-L8-02: the allocation limiter used to keep one bucket per source address,
// in a table of at most 4096. A member with one valid credential could sign
// Allocate requests from thousands of forged UDP sources (the nonce is not
// bound to the source), keep the table full and so refuse every new source,
// other members' first allocation included. The budget is now the member's.

// spoofedSource is the i-th of the addresses one member forges.
func spoofedSource(i int) net.Addr {
	return &net.UDPAddr{IP: net.IPv4(198, 51, byte(i/256), byte(i%256)), Port: 40000}
}

// keys is the number of buckets held.
func (l *rateLimiter) keys() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.m)
}

// The quota handler is what pion calls for a request that passed the message
// integrity check; calling it directly stands for one signed Allocate from
// each forged source, which a test cannot send from addresses it does not own.
func TestOneMemberFromThousandsOfSourcesCannotKeepAnotherOut(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	attacker, _, _ := h.srv.Credentials(uuid.New())
	const sources = 5000 // more than the table's 4096 slots
	granted := 0
	// The refuter's sustained case: two tokens per source per second keeps a
	// per-source table from ever refilling. Each round sends one signed
	// request per source, half a second apart.
	for round := range 6 {
		for i := range sources {
			if h.srv.allowAllocation(attacker, Realm, spoofedSource(i)) {
				granted++
			}
		}
		victim, _ := h.member()
		if _, err := victim.Allocate(); err != nil {
			t.Fatalf("round %d: another member's first allocation failed: %v", round, err)
		}
		h.advance(500 * time.Millisecond)
	}
	// One member, one budget: the burst plus what refilled, not one per source.
	if limit := h.srv.limits.AllocationBurst + 6; granted > limit {
		t.Fatalf("one member was granted %d attempts from %d sources, budget %d", granted, sources, limit)
	}
	if n := h.srv.allocLimit.keys(); n > 7 {
		t.Fatalf("the limiter holds %d keys for 7 members", n)
	}
}

// The relay does not look the member up at authentication: pion runs that
// before the integrity check, so a forger could spend the lookups (see
// authenticate). The first point that knows the request is genuine is the
// quota handler, and there the account state is checked before the limiter.
func TestABannedMembersCredentialIsRefusedBeforeTheLimiter(t *testing.T) {
	var banned atomic.Bool
	h := newHarness(t, harnessOpts{mayRelay: func(uuid.UUID) bool { return !banned.Load() }})
	id := uuid.New()
	u, p, _ := h.srv.Credentials(id)
	if _, err := h.dial(u, p).Allocate(); err != nil {
		t.Fatal(err)
	}
	banned.Store(true)
	h.advance(2 * verdictTTL)
	if _, err := h.dial(u, p).Allocate(); err == nil {
		t.Fatal("a banned member's still-valid credential allocated")
	}
	for i := range 5000 {
		if h.srv.allowAllocation(u, Realm, spoofedSource(i)) {
			t.Fatal("a banned member's signed request passed the quota handler")
		}
	}
	h.srv.allocLimit.mu.Lock()
	b, charged := h.srv.allocLimit.m[id.String()]
	keys := len(h.srv.allocLimit.m)
	h.srv.allocLimit.mu.Unlock()
	if keys != 1 || !charged || b.tokens != float64(h.srv.limits.AllocationBurst-1) {
		t.Fatalf("after the ban the limiter holds %d keys (member's bucket %v): the refusals were charged", keys, b)
	}
	if n := h.srv.Status().Allocations; n != 0 {
		t.Fatalf("the banned member keeps %d allocations", n)
	}
}

// A full member table forgets the fullest bucket rather than refuse a member:
// nobody can mint member keys, so the table is full only when that many
// members draw on it at once.
func TestAFullMemberTableAdmitsANewMemberAndStaysBounded(t *testing.T) {
	const maxKeys = 4096
	now := time.Unix(0, 0)
	l := newMemberLimiter(120, 60, maxKeys)
	for i := range maxKeys * 3 {
		if !l.allow(fmt.Sprintf("member-%d", i), now) {
			t.Fatalf("member %d refused its first attempt with %d keys held", i, len(l.m))
		}
		if len(l.m) > maxKeys {
			t.Fatalf("the table grew to %d keys", len(l.m))
		}
	}
	// A member with a drawn-down bucket keeps it while fuller ones go first.
	drawn := "member-drawn"
	for range 60 {
		l.allow(drawn, now)
	}
	if !l.allow("member-new", now) {
		t.Fatal("a new member was refused at the cap")
	}
	if l.allow(drawn, now) {
		t.Fatal("eviction forgot the most drawn-down bucket and handed it a fresh budget")
	}
	if len(l.m) > maxKeys {
		t.Fatalf("the table grew to %d keys", len(l.m))
	}
}

// failLimit stays keyed by source and username: a failure has no credential
// to attribute it to. Filled with forged sources it stops recording new keys,
// and refuses nobody, since a key it does not hold has budget.
func TestAFullFailureTableRefusesNobody(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	now := h.srv.now()
	for i := range 5000 {
		h.srv.failLimit.allow(hostOf(spoofedSource(i))+"|nonsense", now)
	}
	if n := h.srv.failLimit.keys(); n > 4096 {
		t.Fatalf("failLimit grew to %d keys", n)
	}
	c, _ := h.member()
	if _, err := c.Allocate(); err != nil {
		t.Fatalf("a member was refused while the failure table was full: %v", err)
	}
}

// TestFailLimiterRefusesToGrowPastCap pins the refuse-rather-than-grow
// contract of failLimit at the production cap.
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
