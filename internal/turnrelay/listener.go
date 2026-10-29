package turnrelay

import (
	"net"
	"sync"
	"time"

	"github.com/msch128/mnema-talk/internal/metrics"
)

// limitedListener bounds the TCP connections to the relay, in total and per
// source address, and closes the ones that go quiet: a connection that
// authenticates nothing and sends nothing must not hold a place forever.
type limitedListener struct {
	net.Listener
	s *Server

	mu    sync.Mutex
	total int
	byIP  map[string]int
}

func newLimitedListener(l net.Listener, s *Server) *limitedListener {
	return &limitedListener{Listener: l, s: s, byIP: map[string]int{}}
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
		if !over {
			l.total++
			l.byIP[ip]++
		}
		l.mu.Unlock()
		if over {
			metrics.TURNEvents.With("rate_limited").Inc()
			l.s.debug("turn: too many TCP connections", "from", ip)
			_ = c.Close()
			continue
		}
		return &idleConn{Conn: c, l: l, ip: ip, idle: l.s.limits.IdleTimeout}, nil
	}
}

func (l *limitedListener) done(ip string) {
	l.mu.Lock()
	l.total--
	if l.byIP[ip]--; l.byIP[ip] <= 0 {
		delete(l.byIP, ip)
	}
	l.mu.Unlock()
}

// idleConn closes a TCP connection that sends nothing for the idle timeout.
type idleConn struct {
	net.Conn
	l    *limitedListener
	ip   string
	idle time.Duration
	once sync.Once
}

func (c *idleConn) Read(p []byte) (int, error) {
	_ = c.Conn.SetReadDeadline(time.Now().Add(c.idle))
	return c.Conn.Read(p)
}

func (c *idleConn) Close() error {
	c.once.Do(func() { c.l.done(c.ip) })
	return c.Conn.Close()
}
