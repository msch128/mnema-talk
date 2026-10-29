package turnrelay

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/metrics"
	"github.com/pion/stun/v4"
	"github.com/pion/turn/v5"
)

// harness is a relay in front of a fake SFU: one UDP socket on the allowed
// media port, and a second ("victim") on another port of the same address,
// standing for any other service on the host.
type harness struct {
	t      *testing.T
	srv    *Server
	clock  *atomic.Int64 // unix nanoseconds, moved by tests
	sfu    *net.UDPConn
	victim *net.UDPConn
	target *atomic.Pointer[Target]
}

type harnessOpts struct {
	announced []net.IP
	limits    Limits
	mayRelay  func(uuid.UUID) bool
}

func listenUDP(t *testing.T) *net.UDPConn {
	t.Helper()
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func newHarness(t *testing.T, o harnessOpts) *harness {
	t.Helper()
	h := &harness{t: t, clock: &atomic.Int64{}, sfu: listenUDP(t), victim: listenUDP(t), target: &atomic.Pointer[Target]{}}
	h.clock.Store(time.Now().UnixNano())
	port := uint16(h.sfu.LocalAddr().(*net.UDPAddr).Port)
	h.target.Store(&Target{LocalIPs: []net.IP{net.IPv4(127, 0, 0, 1)}, Announced: o.announced, PortMin: port, PortMax: port})
	probe, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	relayPort := probe.LocalAddr().(*net.UDPAddr).Port
	_ = probe.Close()
	if o.mayRelay == nil {
		o.mayRelay = func(uuid.UUID) bool { return true }
	}
	h.srv, err = Start(Options{
		Port: relayPort, BindAddr: "127.0.0.1",
		Target:   func() Target { return *h.target.Load() },
		MayRelay: o.mayRelay,
		Limits:   o.limits,
		Now:      func() time.Time { return time.Unix(0, h.clock.Load()) },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.srv.Close() })
	return h
}

func (h *harness) advance(d time.Duration) { h.clock.Add(int64(d)) }

func (h *harness) addr() string { return net.JoinHostPort("127.0.0.1", strconv.Itoa(h.srv.opts.Port)) }

// sfuAddr is the address a client knows the SFU by.
func (h *harness) sfuAddr() *net.UDPAddr {
	t := h.target.Load()
	ip := net.IPv4(127, 0, 0, 1)
	if len(t.Announced) > 0 {
		ip = t.Announced[0]
	}
	return &net.UDPAddr{IP: ip, Port: int(t.PortMin)}
}

// dial starts a TURN client with the given credentials.
func (h *harness) dial(username, password string) *turn.Client {
	h.t.Helper()
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		h.t.Fatal(err)
	}
	c, err := turn.NewClient(&turn.ClientConfig{TURNServerAddr: h.addr(), Conn: conn, Username: username, Password: password, Realm: Realm})
	if err != nil {
		h.t.Fatal(err)
	}
	if err := c.Listen(); err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { c.Close(); _ = conn.Close() })
	return c
}

// member dials with fresh valid credentials for a new member.
func (h *harness) member() (*turn.Client, uuid.UUID) {
	id := uuid.New()
	u, p, _ := h.srv.Credentials(id)
	return h.dial(u, p), id
}

func recvWithin(c net.PacketConn, d time.Duration) ([]byte, net.Addr, bool) {
	buf := make([]byte, 1500)
	_ = c.SetReadDeadline(time.Now().Add(d))
	n, from, err := c.ReadFrom(buf)
	if err != nil {
		return nil, nil, false
	}
	return buf[:n], from, true
}

func TestRelaysBothWaysToTheSFUOnly(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	c, _ := h.member()
	relay, err := c.Allocate()
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()

	// client -> SFU
	if _, err := relay.WriteTo([]byte("to-sfu"), h.sfuAddr()); err != nil {
		t.Fatal(err)
	}
	got, from, ok := recvWithin(h.sfu, 2*time.Second)
	if !ok || string(got) != "to-sfu" {
		t.Fatalf("SFU received %q (ok=%v), want the client's datagram", got, ok)
	}

	// SFU -> client, from the media port
	if _, err := h.sfu.WriteTo([]byte("to-client"), from); err != nil {
		t.Fatal(err)
	}
	got, src, ok := recvWithin(relay, 2*time.Second)
	if !ok || string(got) != "to-client" {
		t.Fatalf("client received %q (ok=%v)", got, ok)
	}
	if src.String() != h.sfuAddr().String() {
		t.Fatalf("client sees the SFU at %s, want %s", src, h.sfuAddr())
	}

	// SFU host, any other port: not relayed, neither way.
	before := metrics.TURNEvents.With("packet_blocked").Load()
	victimAddr := h.victim.LocalAddr().(*net.UDPAddr)
	if _, err := relay.WriteTo([]byte("probe"), victimAddr); err != nil {
		t.Fatal(err)
	}
	if got, _, ok := recvWithin(h.victim, 300*time.Millisecond); ok {
		t.Fatalf("a datagram reached another port of the host: %q", got)
	}
	if _, err := h.victim.WriteTo([]byte("spoof"), from); err != nil {
		t.Fatal(err)
	}
	if got, _, ok := recvWithin(relay, 300*time.Millisecond); ok {
		t.Fatalf("a datagram from another port reached the client: %q", got)
	}
	if metrics.TURNEvents.With("packet_blocked").Load() < before+2 {
		t.Fatal("blocked datagrams were not counted")
	}
}

func TestRefusesPeersThatAreNotTheSFU(t *testing.T) {
	h := newHarness(t, harnessOpts{announced: []net.IP{net.ParseIP("203.0.113.7").To4()}})
	c, _ := h.member()
	if _, err := c.Allocate(); err != nil {
		t.Fatal(err)
	}
	before := metrics.TURNEvents.With("peer_denied").Load()
	for _, ip := range []string{
		"8.8.8.8", "10.0.0.5", "192.168.1.1", "169.254.169.254", "0.0.0.0", "255.255.255.255", "224.0.0.1",
		"127.0.0.2", "::1", "2001:db8::1",
	} {
		if err := c.CreatePermission(&net.UDPAddr{IP: net.ParseIP(ip), Port: 5000}); err == nil {
			t.Errorf("permission for %s was granted", ip)
		}
	}
	if metrics.TURNEvents.With("peer_denied").Load() == before {
		t.Error("refused peers were not counted")
	}
	// The SFU's announced and local addresses are the only ones.
	for _, ip := range []string{"203.0.113.7", "127.0.0.1"} {
		if err := c.CreatePermission(&net.UDPAddr{IP: net.ParseIP(ip), Port: 5000}); err != nil {
			t.Errorf("permission for the SFU address %s: %v", ip, err)
		}
	}
}

func TestAnnouncedAddressReachesTheLocalSocket(t *testing.T) {
	h := newHarness(t, harnessOpts{announced: []net.IP{net.ParseIP("203.0.113.7").To4()}})
	c, _ := h.member()
	relay, err := c.Allocate()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := relay.WriteTo([]byte("hi"), h.sfuAddr()); err != nil {
		t.Fatal(err)
	}
	if got, _, ok := recvWithin(h.sfu, 2*time.Second); !ok || string(got) != "hi" {
		t.Fatalf("SFU received %q (ok=%v) through the announced address", got, ok)
	}
}

func TestRefusesTCPAllocations(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	c, _ := h.member()
	if _, err := c.AllocateTCP(); err == nil {
		t.Fatal("a TCP allocation (RFC 6062) was granted")
	}
	if n := h.srv.Status().Allocations; n != 0 {
		t.Fatalf("allocations = %d after a refused TCP allocation", n)
	}
}

func TestCredentials(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	id := uuid.New()
	u, p, expires := h.srv.Credentials(id)

	t.Run("valid", func(t *testing.T) {
		if _, err := h.dial(u, p).Allocate(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("expires after the TTL", func(t *testing.T) {
		if want := time.Unix(0, h.clock.Load()).Add(CredentialTTL); expires.Sub(want) > time.Second || want.Sub(expires) > time.Second {
			t.Fatalf("expires %v, want about %v", expires, want)
		}
		late := newHarness(t, harnessOpts{})
		lu, lp, _ := late.srv.Credentials(id)
		late.advance(CredentialTTL + time.Minute)
		if _, err := late.dial(lu, lp).Allocate(); err == nil {
			t.Fatal("an expired credential allocated")
		}
	})
	t.Run("wrong password", func(t *testing.T) {
		if _, err := h.dial(u, p+"x").Allocate(); err == nil {
			t.Fatal("a wrong password allocated")
		}
	})
	t.Run("credential of another relay instance", func(t *testing.T) {
		other := newHarness(t, harnessOpts{})
		ou, op, _ := other.srv.Credentials(id)
		if _, err := h.dial(ou, op).Allocate(); err == nil {
			t.Fatal("credentials signed by another secret allocated")
		}
	})
	t.Run("forged username with the member swapped", func(t *testing.T) {
		forged := u[:len(u)-36] + uuid.NewString()
		if _, err := h.dial(forged, p).Allocate(); err == nil {
			t.Fatal("a username for another member reused the password")
		}
	})
	t.Run("malformed usernames", func(t *testing.T) {
		for _, bad := range []string{"", "alice", "123:not-a-uuid", ":" + id.String(), "-5:" + id.String(), "x:" + id.String()} {
			if _, err := h.dial(bad, p).Allocate(); err == nil {
				t.Errorf("username %q allocated", bad)
			}
		}
	})
}

func TestOnlyMembersWhoMayJoinVoiceAllocate(t *testing.T) {
	var allowed atomic.Bool
	h := newHarness(t, harnessOpts{mayRelay: func(uuid.UUID) bool { return allowed.Load() }})
	id := uuid.New()
	u, p, _ := h.srv.Credentials(id)
	if _, err := h.dial(u, p).Allocate(); err == nil {
		t.Fatal("allocated although the member may not join voice")
	}
	allowed.Store(true)
	if _, err := h.dial(u, p).Allocate(); err != nil {
		t.Fatalf("allocation after permission: %v", err)
	}
}

func TestNobodyMayRelayUntilTheCheckIsInstalled(t *testing.T) {
	s := &Server{}
	s.SetMayRelay(nil)
	if s.MayRelay(uuid.New()) {
		t.Fatal("the relay allows members before it has a permission check")
	}
}

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

func TestAMemberWhosePermissionCannotBeCheckedKeepsRunningCalls(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	c, id := h.member()
	if _, err := c.Allocate(); err != nil {
		t.Fatal(err)
	}
	u, _, _ := h.srv.Credentials(id)
	src := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 4000}
	req := func(method int) bool {
		_, _, ok := h.srv.authenticate(&turn.RequestAttributes{Username: u, Realm: Realm, SrcAddr: src, Method: stun.Method(method)})
		return ok
	}
	h.srv.SetRelayCheck(func(uuid.UUID) (bool, error) { return false, errors.New("database down") })
	if !req(methodRefresh) {
		t.Error("a refresh was refused because the permission lookup failed")
	}
	if req(methodAllocate) {
		t.Error("a new allocation was granted although the permission lookup failed")
	}
	if n := h.srv.Status().Allocations; n != 1 {
		t.Fatalf("allocations = %d, the running call was dropped", n)
	}
	// A definite no still ends it.
	h.srv.SetRelayCheck(func(uuid.UUID) (bool, error) { return false, nil })
	if req(methodRefresh) {
		t.Error("a refresh was granted to a member who may not join voice")
	}
	if n := h.srv.Status().Allocations; n != 0 {
		t.Fatalf("allocations = %d after the member lost the right", n)
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

func TestDropMemberClosesTheirAllocations(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	c, id := h.member()
	if _, err := c.Allocate(); err != nil {
		t.Fatal(err)
	}
	other, _ := h.member()
	if _, err := other.Allocate(); err != nil {
		t.Fatal(err)
	}
	h.srv.DropMember(id)
	if n := h.srv.Status().Allocations; n != 1 {
		t.Fatalf("allocations = %d, want only the other member's", n)
	}
}

func TestRelayPortNeverLandsInTheSFURange(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	for range 5 {
		c, _ := h.member()
		relay, err := c.Allocate()
		if err != nil {
			t.Fatal(err)
		}
		if port := relay.LocalAddr().(*net.UDPAddr).Port; h.target.Load().portOK(port) {
			t.Fatalf("relayed port %d is an SFU media port", port)
		}
	}
}

func TestStartRefusesUnsafeSetups(t *testing.T) {
	ip := []net.IP{net.IPv4(127, 0, 0, 1)}
	for name, tc := range map[string]Target{
		"no fixed ports":   {LocalIPs: ip},
		"no local address": {PortMin: 50000, PortMax: 50010},
	} {
		if _, err := Start(Options{Port: 3478, Target: func() Target { return tc }}); err == nil {
			t.Errorf("%s: started", name)
		}
	}
	if _, err := Start(Options{Port: 50005, Target: func() Target { return Target{LocalIPs: ip, PortMin: 50000, PortMax: 50010} }}); err == nil {
		t.Error("started on a port inside the SFU range")
	}
}

func TestOneTransportMayFailToBind(t *testing.T) {
	probe, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()
	port := probe.Addr().(*net.TCPAddr).Port // TCP taken, UDP free (most likely)
	sfu := listenUDP(t)
	p := uint16(sfu.LocalAddr().(*net.UDPAddr).Port)
	srv, err := Start(Options{Port: port, BindAddr: "127.0.0.1", Target: func() Target {
		return Target{LocalIPs: []net.IP{net.IPv4(127, 0, 0, 1)}, PortMin: p, PortMax: p}
	}})
	if err != nil {
		t.Skipf("UDP port was taken as well: %v", err)
	}
	defer srv.Close()
	st := srv.Status()
	if !st.UDP || st.TCP || st.TCPErr == "" {
		t.Fatalf("status = %+v, want UDP only with the TCP error reported", st)
	}
	if urls := srv.URLs("turn.example.test"); len(urls) != 1 || urls[0] != "turn:turn.example.test:"+strconv.Itoa(port)+"?transport=udp" {
		t.Fatalf("urls = %v, want only the UDP one", urls)
	}
}

func TestStartFailsWhenNothingCanBind(t *testing.T) {
	u, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer u.Close()
	port := u.LocalAddr().(*net.UDPAddr).Port
	tcp, err := net.Listen("tcp4", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		t.Skip("TCP side of the port is taken")
	}
	defer tcp.Close()
	sfu := listenUDP(t)
	p := uint16(sfu.LocalAddr().(*net.UDPAddr).Port)
	if _, err := Start(Options{Port: port, BindAddr: "127.0.0.1", Target: func() Target {
		return Target{LocalIPs: []net.IP{net.IPv4(127, 0, 0, 1)}, PortMin: p, PortMax: p}
	}}); err == nil {
		t.Fatal("started although neither UDP nor TCP could bind")
	}
}

func TestURLs(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	port := strconv.Itoa(h.srv.opts.Port)
	want := []string{"turn:turn.example.test:" + port + "?transport=udp", "turn:turn.example.test:" + port + "?transport=tcp"}
	got := h.srv.URLs("turn.example.test")
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("urls = %v, want %v", got, want)
	}
	if v6 := h.srv.URLs("2001:db8::1"); v6[0] != "turn:[2001:db8::1]:"+port+"?transport=udp" {
		t.Fatalf("IPv6 host not bracketed: %v", v6)
	}
}

func TestRepliesComeFromTheAddressTheClientHoldsAPermissionFor(t *testing.T) {
	a, b := net.ParseIP("203.0.113.7").To4(), net.ParseIP("192.0.2.9").To4()
	h := newHarness(t, harnessOpts{announced: []net.IP{a, b}})
	c, _ := h.member()
	relay, err := c.Allocate()
	if err != nil {
		t.Fatal(err)
	}
	port := h.sfuAddr().Port
	// Only the second announced address is known to this client.
	if _, err := relay.WriteTo([]byte("hello"), &net.UDPAddr{IP: b, Port: port}); err != nil {
		t.Fatal(err)
	}
	_, from, ok := recvWithin(h.sfu, 2*time.Second)
	if !ok {
		t.Fatal("SFU received nothing")
	}
	if _, err := h.sfu.WriteTo([]byte("reply"), from); err != nil {
		t.Fatal(err)
	}
	_, src, ok := recvWithin(relay, 2*time.Second)
	if !ok {
		t.Fatal("client received nothing")
	}
	if got := src.(*net.UDPAddr).IP; !got.Equal(b) {
		t.Fatalf("reply appears to come from %v, want %v (the address with a permission)", got, b)
	}
}

func TestWorksOverTCP(t *testing.T) {
	h := newHarness(t, harnessOpts{})
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
	if _, err := relay.WriteTo([]byte("over-tcp"), h.sfuAddr()); err != nil {
		t.Fatal(err)
	}
	if got, _, ok := recvWithin(h.sfu, 2*time.Second); !ok || string(got) != "over-tcp" {
		t.Fatalf("SFU received %q (ok=%v)", got, ok)
	}
	// Relaying to anything else is as impossible over TCP as over UDP.
	if err := c.CreatePermission(&net.UDPAddr{IP: net.ParseIP("10.1.2.3"), Port: 9}); err == nil {
		t.Fatal("permission for a private address granted over TCP")
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

func TestProbe(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.srv.opts.PublicHost = "127.0.0.1"
	if got := h.srv.probeOnce(context.Background()); got != "reachable" {
		t.Fatalf("probe of a listening relay = %q", got)
	}
	_ = h.srv.Close()
	if got := h.srv.probeOnce(context.Background()); got != "unreachable" {
		t.Fatalf("probe of a closed relay = %q", got)
	}
}

func TestGeneratorRefusesEverythingButUDP(t *testing.T) {
	g := &generator{s: &Server{}}
	if _, _, err := g.AllocatePacketConn(turn.AllocateListenerConfig{Network: "udp6"}); err == nil {
		t.Error("an IPv6 allocation was granted")
	}
	if _, _, err := g.AllocatePacketConn(turn.AllocateListenerConfig{Network: "udp4", UserID: "not-a-credential"}); err == nil {
		t.Error("an allocation without a credential identity was granted")
	}
	if _, _, err := g.AllocateListener(turn.AllocateListenerConfig{Network: "tcp4"}); err == nil {
		t.Error("a TCP listener was allocated")
	}
	if _, err := g.AllocateConn(turn.AllocateConnConfig{Network: "tcp4"}); err == nil {
		t.Error("a TCP connection was allocated")
	}
}

func TestProbeLoopReportsInStatus(t *testing.T) {
	probeFirstDelay = 10 * time.Millisecond
	t.Cleanup(func() { probeFirstDelay = 3 * time.Second })
	port, sfu := freePort(t), listenUDP(t)
	p := uint16(sfu.LocalAddr().(*net.UDPAddr).Port)
	srv, err := Start(Options{Port: port, BindAddr: "127.0.0.1", PublicHost: "127.0.0.1", MayRelay: func(uuid.UUID) bool { return true },
		Target: func() Target { return Target{LocalIPs: []net.IP{net.IPv4(127, 0, 0, 1)}, PortMin: p, PortMax: p} }})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	if got := srv.Status().Probe; got != "unknown" {
		t.Fatalf("probe before the first run = %q", got)
	}
	deadline := time.Now().Add(5 * time.Second)
	for srv.Status().Probe == "unknown" && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if got := srv.Status().Probe; got != "reachable" {
		t.Fatalf("probe = %q, want reachable", got)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	c, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).Port
}

func TestHostOf(t *testing.T) {
	if got := hostOf(nil); got != "" {
		t.Errorf("hostOf(nil) = %q", got)
	}
	if got := hostOf(&net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 9}); got != "192.0.2.1" {
		t.Errorf("hostOf = %q", got)
	}
}

// A username that looks valid but whose password is wrong does not lift the
// handshake limit of a TCP connection: only a request that passed the integrity
// check does.
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
