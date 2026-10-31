package turnrelay

import (
	"errors"
	"net"

	"github.com/msch128/mnema-talk/internal/metrics"
	"github.com/pion/turn/v5"
)

var errRefused = errors.New("turn: not supported")

// generator creates the relayed sockets. They live on the server, on the
// address the SFU's sockets are reachable on, so the relay needs no public
// port range of its own: the relayed address is only ever used between the
// relay and the SFU on the same machine.
type generator struct{ s *Server }

// relaySocketBuffer is the kernel buffer asked for on each relay socket; the
// kernel grants at most net.core.rmem_max / wmem_max.
const relaySocketBuffer = 4 << 20

func (g *generator) Validate() error { return nil }

// AllocatePacketConn opens the relayed UDP socket of one allocation.
func (g *generator) AllocatePacketConn(conf turn.AllocateListenerConfig) (net.PacketConn, net.Addr, error) {
	if conf.Network != "udp4" {
		return nil, nil, errRefused
	}
	s := g.s
	user, expires, err := parseUsername(conf.UserID)
	if err != nil {
		return nil, nil, err
	}
	// The request has passed the integrity check by now, so the lookup is not
	// spent on a forger. A failed lookup refuses the new allocation.
	if ok, err := s.mayJoin(user); err != nil || !ok {
		metrics.TURNEvents.With("forbidden").Inc()
		s.debug("turn: member may not join voice", "user", user, "err", err)
		return nil, nil, errors.New("turn: not allowed")
	}
	if !s.acquire(user) {
		return nil, nil, errors.New("turn: allocation quota reached")
	}
	t := s.target()
	bind := t.relayIP()
	if bind == nil {
		s.unacquire(user)
		return nil, nil, errors.New("turn: SFU has no local address")
	}
	// The kernel picks the port. One inside the SFU's media range is not ours
	// to take (it would let a client relay to its own or another relay), so
	// try again.
	var conn *net.UDPConn
	for range 16 {
		c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: bind})
		if err != nil {
			s.unacquire(user)
			return nil, nil, err
		}
		if !t.portOK(c.LocalAddr().(*net.UDPAddr).Port) {
			conn = c
			break
		}
		_ = c.Close()
	}
	if conn == nil {
		s.unacquire(user)
		return nil, nil, errors.New("turn: no relay port outside the SFU range")
	}
	// A relayed screen frame is a burst of packets; see the SFU's media sockets.
	_ = conn.SetReadBuffer(relaySocketBuffer)
	_ = conn.SetWriteBuffer(relaySocketBuffer)
	port := conn.LocalAddr().(*net.UDPAddr).Port
	gd := &guard{
		PacketConn: conn, srv: s, user: user, expires: expires, bind: bind, port: port,
		budget: newByteBudget(s.limits.BytesPerSecond, s.limits.BurstBytes, s.now()),
	}
	gd.touch()
	s.register(gd)
	metrics.TURNEvents.With("relay_opened").Inc()
	return gd, &net.UDPAddr{IP: bind, Port: port}, nil
}

// AllocateListener refuses TCP allocations (RFC 6062): the SFU takes media over
// UDP only, so nothing may be relayed over TCP.
func (g *generator) AllocateListener(turn.AllocateListenerConfig) (net.Listener, net.Addr, error) {
	return nil, nil, errRefused
}

// AllocateConn refuses outbound TCP connections for the same reason.
func (g *generator) AllocateConn(turn.AllocateConnConfig) (net.Conn, error) {
	return nil, errRefused
}
