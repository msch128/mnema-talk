package turnrelay

import (
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/metrics"
)

// guard is the relayed socket of one allocation. It is the last line of
// defence: whatever the TURN protocol layer was talked into, a datagram only
// leaves this socket for one of the SFU's media ports and only enters it from
// one. It also translates between the address browsers know the SFU by and the
// local socket it really is.
type guard struct {
	net.PacketConn
	srv     *Server
	user    uuid.UUID
	expires time.Time
	bind    net.IP
	port    int

	budget     *byteBudget
	lastActive atomic.Int64 // unix nanoseconds of the last forwarded datagram

	mu        sync.RWMutex
	permitted []net.IP // peers the client holds a permission for, in grant order

	closeOnce sync.Once
}

func (g *guard) touch() { g.lastActive.Store(g.srv.now().UnixNano()) }

// permit records that the client may reach peer, so replies can be shown as
// coming from an address the client knows.
func (g *guard) permit(ip net.IP) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !containsIP(g.permitted, ip) && len(g.permitted) < 16 {
		g.permitted = append(g.permitted, ip)
	}
}

func (g *guard) holds(ip net.IP) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return containsIP(g.permitted, ip)
}

// virtualFor is the address the client sees a packet from the local socket src
// as coming from. Without announced addresses the sockets are announced as
// they are. Otherwise it is the first announced address the client may talk
// to, so the replies to its connectivity checks always come from one fixed
// peer address, which the client matches to a candidate it knows.
func (g *guard) virtualFor(t Target, src net.IP) net.IP {
	if len(t.Announced) == 0 {
		return src
	}
	for _, a := range t.Announced {
		if g.holds(a) {
			return a
		}
	}
	return t.Announced[0]
}

// WriteTo relays a datagram of the client to the SFU.
func (g *guard) WriteTo(p []byte, addr net.Addr) (int, error) {
	udp, ok := addr.(*net.UDPAddr)
	if !ok {
		return g.block(len(p), "non-udp", addr)
	}
	dst, ok := g.srv.target().concrete(&net.UDPAddr{IP: udp.IP.To4(), Port: udp.Port}, g.bind)
	if !ok {
		return g.block(len(p), "not an SFU media address", addr)
	}
	now := g.srv.now()
	if !g.budget.allow(len(p), now) {
		metrics.TURNEvents.With("throttled").Inc()
		return len(p), nil
	}
	g.touch()
	n, err := g.PacketConn.WriteTo(p, dst)
	metrics.TURNRelayedBytes.With("to_sfu").Add(uint64(n))
	metrics.TURNRelayedPackets.With("to_sfu").Inc()
	return n, err
}

func (g *guard) block(n int, why string, addr net.Addr) (int, error) {
	metrics.TURNEvents.With("packet_blocked").Inc()
	g.srv.debug("turn: blocked datagram from relay", "why", why, "peer", addr.String())
	return n, nil
}

// ReadFrom returns the next datagram the SFU sent to the relayed address, as
// coming from the address the client knows the SFU by. Anything else that
// reaches the socket is dropped.
func (g *guard) ReadFrom(p []byte) (int, net.Addr, error) {
	for {
		n, from, err := g.PacketConn.ReadFrom(p)
		if err != nil {
			return 0, nil, err
		}
		udp, ok := from.(*net.UDPAddr)
		t := g.srv.target()
		if !ok || !t.isLocal(udp.IP.To4()) || !t.portOK(udp.Port) {
			metrics.TURNEvents.With("packet_blocked").Inc()
			g.srv.debug("turn: dropped datagram to relay from a non-SFU source", "from", from.String())
			continue
		}
		g.touch()
		metrics.TURNRelayedBytes.With("from_sfu").Add(uint64(n))
		metrics.TURNRelayedPackets.With("from_sfu").Inc()
		return n, &net.UDPAddr{IP: g.virtualFor(t, udp.IP.To4()), Port: udp.Port}, nil
	}
}

// Close releases the socket and the member's quota. It is safe to call twice.
func (g *guard) Close() error {
	var err error
	g.closeOnce.Do(func() {
		err = g.PacketConn.Close()
		g.srv.release(g)
	})
	return err
}
