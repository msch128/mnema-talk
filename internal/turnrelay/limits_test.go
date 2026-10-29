package turnrelay

import (
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/metrics"
	"github.com/pion/turn/v5"
)

func TestQuotaPerMemberAndInTotal(t *testing.T) {
	h := newHarness(t, harnessOpts{limits: Limits{PerMember: 2, Total: 3, AllocationBurst: 50, AllocationsPerMinute: 600}})
	id := uuid.New()
	u, p, _ := h.srv.Credentials(id)
	for range 2 {
		if _, err := h.dial(u, p).Allocate(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.dial(u, p).Allocate(); err == nil {
		t.Fatal("a third allocation of one member was granted")
	}
	// Another member still can, until the relay's total is reached.
	a, _ := h.member()
	if _, err := a.Allocate(); err != nil {
		t.Fatal(err)
	}
	b, _ := h.member()
	if _, err := b.Allocate(); err == nil {
		t.Fatal("an allocation beyond the relay's total was granted")
	}
}

func TestQuotaIsReturnedWhenAnAllocationEnds(t *testing.T) {
	h := newHarness(t, harnessOpts{limits: Limits{PerMember: 1, AllocationBurst: 50, AllocationsPerMinute: 600}})
	u, p, _ := h.srv.Credentials(uuid.New())
	c := h.dial(u, p)
	relay, err := c.Allocate()
	if err != nil {
		t.Fatal(err)
	}
	_ = relay.Close() // refresh with lifetime 0 deallocates
	deadline := time.Now().Add(3 * time.Second)
	for h.srv.Status().Allocations != 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := h.dial(u, p).Allocate(); err != nil {
		t.Fatalf("allocation after the previous one ended: %v", err)
	}
}

func TestAllocationAttemptsAreRateLimited(t *testing.T) {
	h := newHarness(t, harnessOpts{limits: Limits{AllocationBurst: 2, AllocationsPerMinute: 1, PerMember: 100}})
	u, p, _ := h.srv.Credentials(uuid.New())
	ok := 0
	for range 6 {
		if _, err := h.dial(u, p).Allocate(); err == nil {
			ok++
		}
	}
	if ok != 2 {
		t.Fatalf("%d allocations granted in a burst of 2", ok)
	}
}

// Failed logins are counted per address and username: a spoofed source cannot
// lock out the members who share an address, only burn its own made-up names.
func TestBadCredentialsOnlyUseUpTheirOwnBudget(t *testing.T) {
	h := newHarness(t, harnessOpts{limits: Limits{FailureBurst: 3, FailuresPerMinute: 1}})
	before := metrics.TURNEvents.With("rate_limited").Load()
	for range 6 {
		_, _ = h.dial("nonsense", "nonsense").Allocate()
	}
	if metrics.TURNEvents.With("rate_limited").Load() == before {
		t.Fatal("repeated failures with one username were never throttled")
	}
	u, p, _ := h.srv.Credentials(uuid.New())
	if _, err := h.dial(u, p).Allocate(); err != nil {
		t.Fatalf("a member behind the same address was locked out: %v", err)
	}
}

func TestTotalQuotaIsReservedBeforeTheSocketExists(t *testing.T) {
	s := &Server{limits: Limits{PerMember: 4, Total: 3}.withDefaults(), perUser: map[uuid.UUID]int{}, guards: map[*guard]struct{}{}}
	s.limits.Total = 3
	var granted atomic.Int32
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s.acquire(uuid.New()) {
				granted.Add(1)
			}
		}()
	}
	wg.Wait()
	if granted.Load() != 3 {
		t.Fatalf("%d concurrent allocations passed a total of 3", granted.Load())
	}
}

// Pion runs the auth callback before it checks the message integrity, so a
// forger who knows a username can send unlimited requests: none of them may
// reach the permission lookup, which is a database query.
func TestForgedRequestsNeverReachThePermissionLookup(t *testing.T) {
	h := newHarness(t, harnessOpts{limits: Limits{AllocationBurst: 1000, AllocationsPerMinute: 6000}})
	var lookups atomic.Int32
	h.srv.SetRelayCheck(func(uuid.UUID) (bool, error) { lookups.Add(1); return true, nil })
	u, p, _ := h.srv.Credentials(uuid.New())
	for range 30 {
		_, _ = h.dial(u, p+"x").Allocate()
	}
	if n := lookups.Load(); n != 0 {
		t.Fatalf("%d permission lookups were spent on requests with a forged password", n)
	}
	if _, err := h.dial(u, p).Allocate(); err != nil {
		t.Fatal(err)
	}
	if lookups.Load() == 0 {
		t.Fatal("a genuine allocation was granted without asking the permission check")
	}
}

func TestTrickledBytesDoNotHoldATCPSlot(t *testing.T) {
	h := newHarness(t, harnessOpts{limits: Limits{HandshakeTimeout: 300 * time.Millisecond, IdleTimeout: 10 * time.Second}})
	slow, err := net.Dial("tcp4", h.addr())
	if err != nil {
		t.Fatal(err)
	}
	defer slow.Close()
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(80 * time.Millisecond):
				if _, err := slow.Write([]byte{0}); err != nil {
					return
				}
			}
		}
	}()
	_ = slow.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := slow.Read(make([]byte, 1)); err == nil {
		t.Fatal("unexpected data from the relay")
	} else if ne, ok := err.(net.Error); ok && ne.Timeout() {
		t.Fatal("a connection trickling bytes was never closed")
	}

	// A client that authenticates in time is not cut by the handshake limit.
	conn, err := net.Dial("tcp4", h.addr())
	if err != nil {
		t.Fatal(err)
	}
	u, p, _ := h.srv.Credentials(uuid.New())
	c, err := turn.NewClient(&turn.ClientConfig{TURNServerAddr: h.addr(), Conn: turn.NewSTUNConn(conn), Username: u, Password: p, Realm: Realm})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Listen(); err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	relay, err := c.Allocate()
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(700 * time.Millisecond)
	if _, err := relay.WriteTo([]byte("late"), h.sfuAddr()); err != nil {
		t.Fatal(err)
	}
	if got, _, ok := recvWithin(h.sfu, 2*time.Second); !ok || string(got) != "late" {
		t.Fatalf("the authenticated TCP connection was cut: %q (ok=%v)", got, ok)
	}
}

func TestIdleAndExpiredAllocationsAreClosed(t *testing.T) {
	h := newHarness(t, harnessOpts{limits: Limits{IdleTimeout: time.Minute}})
	c, _ := h.member()
	if _, err := c.Allocate(); err != nil {
		t.Fatal(err)
	}
	h.srv.reap()
	if n := h.srv.Status().Allocations; n != 1 {
		t.Fatalf("allocations = %d right after use, want 1", n)
	}
	h.advance(2 * time.Minute)
	h.srv.reap()
	if n := h.srv.Status().Allocations; n != 0 {
		t.Fatalf("allocations = %d after the idle timeout, want 0", n)
	}

	// An allocation does not outlive its credential.
	c2, _ := h.member()
	if _, err := c2.Allocate(); err != nil {
		t.Fatal(err)
	}
	h.advance(CredentialTTL + time.Second)
	h.srv.reap()
	if n := h.srv.Status().Allocations; n != 0 {
		t.Fatalf("allocations = %d after the credential expired, want 0", n)
	}
}

func TestTCPConnectionsAreCappedAndTimedOut(t *testing.T) {
	h := newHarness(t, harnessOpts{limits: Limits{TCPPerAddress: 2, IdleTimeout: 300 * time.Millisecond}})
	var conns []net.Conn
	for range 3 {
		c, err := net.Dial("tcp4", h.addr())
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		conns = append(conns, c)
	}
	closed := func(c net.Conn, within time.Duration) bool {
		_ = c.SetReadDeadline(time.Now().Add(within))
		_, err := c.Read(make([]byte, 1))
		var ne net.Error
		return err != nil && !(errors.As(err, &ne) && ne.Timeout())
	}
	// The third connection from one address is closed at once; the idle ones
	// follow after the idle timeout.
	if !closed(conns[2], 2*time.Second) {
		t.Fatal("a connection over the per-address cap stayed open")
	}
	if !closed(conns[0], 3*time.Second) {
		t.Fatal("an idle connection stayed open")
	}
}

func TestForgedCredentialsDoNotLiftTheTCPHandshakeLimit(t *testing.T) {
	h := newHarness(t, harnessOpts{limits: Limits{HandshakeTimeout: 400 * time.Millisecond, IdleTimeout: 10 * time.Second}})
	u, _, _ := h.srv.Credentials(uuid.New())
	conn, err := net.Dial("tcp4", h.addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	c, err := turn.NewClient(&turn.ClientConfig{TURNServerAddr: h.addr(), Conn: turn.NewSTUNConn(conn), Username: u, Password: "not-the-password", Realm: Realm})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Listen(); err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Allocate(); err == nil {
		t.Fatal("an allocation with a forged password was granted")
	}
	// The connection is closed by the server at the handshake limit although it
	// presented a well-formed, unexpired username.
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		if _, err := conn.Read(make([]byte, 512)); err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				t.Fatal("the connection outlived the handshake limit")
			}
			return
		}
	}
}

// A member's first Allocate costs exactly one permission lookup, although both
// the verified request (OnAuth) and the allocation itself ask.
func TestFirstAllocateCostsOneLookup(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	var lookups atomic.Int32
	h.srv.SetRelayCheck(func(uuid.UUID) (bool, error) {
		lookups.Add(1)
		time.Sleep(50 * time.Millisecond) // long enough for the two askers to overlap
		return true, nil
	})
	c, _ := h.member()
	if _, err := c.Allocate(); err != nil {
		t.Fatal(err)
	}
	h.srv.awaitLookups()
	if n := lookups.Load(); n != 1 {
		t.Fatalf("a first Allocate cost %d lookups, want 1", n)
	}
}

// A definite "no" is final: a cached refusal still ends the allocations.
func TestCachedRefusalStillEndsAllocations(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	c, id := h.member()
	if _, err := c.Allocate(); err != nil {
		t.Fatal(err)
	}
	h.srv.SetRelayCheck(func(uuid.UUID) (bool, error) { return false, nil })
	h.advance(2 * verdictTTL)
	if ok, err := h.srv.mayJoin(id); ok || err != nil {
		t.Fatalf("mayJoin = %v, %v", ok, err)
	}
	if n := h.srv.Status().Allocations; n != 0 {
		t.Fatalf("allocations = %d after a refusal", n)
	}
	// The same when the refusal is answered from the cache.
	h.srv.SetRelayCheck(func(uuid.UUID) (bool, error) { return true, nil })
	c2, id2 := h.member()
	if _, err := c2.Allocate(); err != nil {
		t.Fatal(err)
	}
	h.srv.mu.Lock()
	h.srv.verdicts[id2] = verdict{ok: false, at: h.srv.now()}
	h.srv.mu.Unlock()
	if ok, _ := h.srv.mayJoin(id2); ok {
		t.Fatal("a cached refusal was answered with a yes")
	}
	if n := h.srv.Status().Allocations; n != 0 {
		t.Fatalf("allocations = %d after a cached refusal", n)
	}
}
