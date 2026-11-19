// Package fastudp sends UDP datagrams from many goroutines at once on one
// socket.
//
// Go serialises every write on a socket: (*net.UDPConn).WriteTo takes the
// descriptor's write lock for the whole system call, so concurrent writers
// queue behind each other although the kernel would take their datagrams in
// parallel. The SFU sends every member's media out of one shared UDP port, and
// the built-in TURN relay every relayed packet out of its one listening
// socket, so on a slow CPU that lock, not the CPU, caps what a Talk can carry:
// on a 4-core Intel N100 a Talk with a screen share and ten cameras to 100
// members queued all its forwarding goroutines on it and lost half of the
// publishers' packets before they were read (doc/benchmark.md).
//
// Conn wraps a *net.UDPConn and sends IPv4 datagrams with sendto(2) directly
// on the descriptor, without the lock, on Linux. A send the kernel cannot take
// at once (a full send buffer) and every other platform or address family
// goes through the wrapped conn as before. Reads are untouched.
//
// A writer that falls back on a full send buffer waits for the socket through
// the runtime, while direct writers may take the room it frees first; such a
// writer is delayed, never stuck, and a UDP send buffer is rarely full.
package fastudp

import (
	"net"
	"net/netip"
	"sync"
)

// Conn is a *net.UDPConn whose IPv4 writes do not serialise. Close waits for
// writes in progress, so a send never reaches a descriptor number the kernel
// has handed out again.
type Conn struct {
	*net.UDPConn
	// fd is the socket's descriptor, -1 when writes take the wrapped conn.
	fd int
	// mu is held for reading by every direct send and for writing by Close.
	mu     sync.RWMutex
	closed bool
}

// Wrap returns c with lock-free IPv4 writes where the platform allows them,
// and c itself otherwise (other platforms, IPv6 sockets, no descriptor).
func Wrap(c *net.UDPConn) net.PacketConn {
	fd, ok := rawFD(c)
	if !ok {
		return c
	}
	return &Conn{UDPConn: c, fd: fd}
}

// Direct reports whether writes bypass the runtime's write lock.
func (c *Conn) Direct() bool { return c.fd >= 0 }

// WriteToAddrPort sends b to addr. Pion's ICE mux prefers this method.
func (c *Conn) WriteToAddrPort(b []byte, addr netip.AddrPort) (int, error) {
	if n, done, err := c.send(b, addr); done {
		return n, err
	}
	return c.UDPConn.WriteToUDPAddrPort(b, addr)
}

// WriteToUDPAddrPort is WriteToAddrPort under the name of *net.UDPConn.
func (c *Conn) WriteToUDPAddrPort(b []byte, addr netip.AddrPort) (int, error) {
	return c.WriteToAddrPort(b, addr)
}

// ReadFromAddrPort reads like (*net.UDPConn).ReadFromUDPAddrPort; with
// WriteToAddrPort it lets Pion's mux use the allocation-free path.
func (c *Conn) ReadFromAddrPort(b []byte) (int, netip.AddrPort, error) {
	return c.UDPConn.ReadFromUDPAddrPort(b)
}

// WriteTo sends b to addr, a *net.UDPAddr.
func (c *Conn) WriteTo(b []byte, addr net.Addr) (int, error) {
	if ua, ok := addr.(*net.UDPAddr); ok {
		return c.WriteToUDP(b, ua)
	}
	return c.UDPConn.WriteTo(b, addr)
}

// WriteToUDP sends b to addr.
func (c *Conn) WriteToUDP(b []byte, addr *net.UDPAddr) (int, error) {
	if addr != nil {
		if n, done, err := c.send(b, addr.AddrPort()); done {
			return n, err
		}
	}
	return c.UDPConn.WriteToUDP(b, addr)
}

// Close closes the socket once no direct send is in progress.
func (c *Conn) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	return c.UDPConn.Close()
}

// send tries the direct path. done is false when the caller must use the
// wrapped conn instead: no descriptor, not an IPv4 destination, closed (the
// wrapped conn then reports the error), or a send the kernel would block.
func (c *Conn) send(b []byte, addr netip.AddrPort) (n int, done bool, err error) {
	if c.fd < 0 {
		return 0, false, nil
	}
	ip := addr.Addr().Unmap()
	if !ip.Is4() {
		return 0, false, nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.closed {
		return 0, false, nil
	}
	return sendto(c.fd, b, ip.As4(), addr.Port())
}

// sendto is the system call; tests replace it.
var sendto = sendto4
