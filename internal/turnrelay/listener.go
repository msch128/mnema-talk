package turnrelay

import (
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/msch128/mnema-talk/internal/metrics"
)

// limitedListener bounds the TCP connections to the relay, in total and per
// source address. A connection must get a request past authentication within
// the handshake timeout, however slowly it sends, and afterwards goes quiet at
// most for the idle timeout: one that sends nothing useful must not hold a
// place forever.
type limitedListener struct {
	net.Listener
	s *Server

	mu    sync.Mutex
	total int
	byIP  map[string]int
	conns map[string]*idleConn // by remote address
}

func newLimitedListener(l net.Listener, s *Server) *limitedListener {
	return &limitedListener{Listener: l, s: s, byIP: map[string]int{}, conns: map[string]*idleConn{}}
}

func (l *limitedListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		ip := hostOf(c.RemoteAddr())
		l.mu.Lock()
		over := l.total >= l.s.limits.TCPTotal || l.byIP[ip] >= l.s.limits.TCPPerAddress
		var ic *idleConn
		if !over {
			l.total++
			l.byIP[ip]++
			ic = &idleConn{Conn: c, l: l, ip: ip, idle: l.s.limits.IdleTimeout, deadline: time.Now().Add(l.s.limits.HandshakeTimeout)}
			l.conns[c.RemoteAddr().String()] = ic
		}
		l.mu.Unlock()
		if over {
			metrics.TURNEvents.With("rate_limited").Inc()
			l.s.debug("turn: too many TCP connections", "from", ip)
			_ = c.Close()
			continue
		}
		return ic, nil
	}
}

// authenticated lifts the handshake deadline of the connection at addr: it
// presented credentials the relay accepted.
func (l *limitedListener) authenticated(addr net.Addr) {
	if addr == nil {
		return
	}
	l.mu.Lock()
	c := l.conns[addr.String()]
	l.mu.Unlock()
	if c != nil {
		c.authed.Store(true)
	}
}

func (l *limitedListener) done(c *idleConn) {
	l.mu.Lock()
	l.total--
	if l.byIP[c.ip]--; l.byIP[c.ip] <= 0 {
		delete(l.byIP, c.ip)
	}
	delete(l.conns, c.Conn.RemoteAddr().String())
	l.mu.Unlock()
}

// idleConn closes a TCP connection that sends nothing for the idle timeout, or
// that has not authenticated by the handshake deadline.
type idleConn struct {
	net.Conn
	l        *limitedListener
	ip       string
	idle     time.Duration
	deadline time.Time // handshake deadline, until authed
	authed   atomic.Bool
	once     sync.Once
}

func (c *idleConn) Read(p []byte) (int, error) {
	d := time.Now().Add(c.idle)
	if !c.authed.Load() && c.deadline.Before(d) {
		d = c.deadline
	}
	_ = c.Conn.SetReadDeadline(d)
	return c.Conn.Read(p)
}

func (c *idleConn) Close() error {
	c.once.Do(func() { c.l.done(c) })
	return c.Conn.Close()
}
