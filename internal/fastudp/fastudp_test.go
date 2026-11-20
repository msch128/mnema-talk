package fastudp

import (
	"errors"
	"net"
	"net/netip"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func listen(t testing.TB, network, ip string) *net.UDPConn {
	t.Helper()
	c, err := net.ListenUDP(network, &net.UDPAddr{IP: net.ParseIP(ip)})
	if err != nil {
		t.Skipf("no %s socket on %s: %v", network, ip, err)
	}
	return c
}

// receiver counts the datagrams that arrive on a socket until it is closed.
type receiver struct {
	conn  *net.UDPConn
	count atomic.Int64
	done  chan struct{}
}

func newReceiver(t testing.TB, network, ip string) *receiver {
	t.Helper()
	r := &receiver{conn: listen(t, network, ip), done: make(chan struct{})}
	_ = r.conn.SetReadBuffer(8 << 20)
	go func() {
		defer close(r.done)
		buf := make([]byte, 2048)
		for {
			if _, _, err := r.conn.ReadFromUDPAddrPort(buf); err != nil {
				return
			}
			r.count.Add(1)
		}
	}()
	t.Cleanup(func() { _ = r.conn.Close(); <-r.done })
	return r
}

func (r *receiver) addr() netip.AddrPort { return r.conn.LocalAddr().(*net.UDPAddr).AddrPort() }

// waitFor waits until n datagrams arrived or a second passed.
func (r *receiver) waitFor(n int64) int64 {
	deadline := time.Now().Add(time.Second)
	for r.count.Load() < n && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	return r.count.Load()
}

func TestWrapTakesTheDirectPathForIPv4OnLinuxOnly(t *testing.T) {
	c := listen(t, "udp4", "127.0.0.1")
	defer c.Close()
	w := Wrap(c)
	fast, ok := w.(*Conn)
	if runtime.GOOS != "linux" {
		if ok {
			t.Fatal("wrapped on a platform without the direct path")
		}
		return
	}
	if !ok || !fast.Direct() {
		t.Fatalf("an IPv4 socket on Linux is not wrapped for direct sends: %T", w)
	}
	v6 := listen(t, "udp6", "::1")
	defer v6.Close()
	if _, ok := Wrap(v6).(*Conn); ok {
		t.Fatal("an IPv6 socket must keep the runtime's path")
	}
}

func TestEveryWriteMethodDelivers(t *testing.T) {
	r := newReceiver(t, "udp4", "127.0.0.1")
	w := Wrap(listen(t, "udp4", "127.0.0.1"))
	defer w.Close()
	payload := []byte("media")
	ua := net.UDPAddrFromAddrPort(r.addr())
	if _, err := w.WriteTo(payload, ua); err != nil {
		t.Fatal(err)
	}
	if fast, ok := w.(*Conn); ok {
		if _, err := fast.WriteToAddrPort(payload, r.addr()); err != nil {
			t.Fatal(err)
		}
		if _, err := fast.WriteToUDP(payload, ua); err != nil {
			t.Fatal(err)
		}
		mapped := netip.AddrPortFrom(netip.AddrFrom16(r.addr().Addr().As16()), r.addr().Port())
		if n, err := fast.WriteToUDPAddrPort(payload, mapped); err != nil || n != len(payload) {
			t.Fatalf("v4-mapped destination: %d, %v", n, err)
		}
		if got := r.waitFor(4); got != 4 {
			t.Fatalf("%d of 4 datagrams arrived", got)
		}
		return
	}
	if got := r.waitFor(1); got != 1 {
		t.Fatalf("%d of 1 datagram arrived", got)
	}
}

func TestConcurrentWritersAllDeliver(t *testing.T) {
	r := newReceiver(t, "udp4", "127.0.0.1")
	w := Wrap(listen(t, "udp4", "127.0.0.1"))
	defer w.Close()
	_ = w.(interface{ SetWriteBuffer(int) error }).SetWriteBuffer(8 << 20)
	const writers, each = 8, 200
	var wg sync.WaitGroup
	payload := make([]byte, 1200)
	ua := net.UDPAddrFromAddrPort(r.addr())
	for range writers {
		wg.Go(func() {
			for range each {
				if _, err := w.WriteTo(payload, ua); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	wg.Wait()
	// Loopback drops nothing while the receiver's buffer has room.
	if got := r.waitFor(writers * each); got != writers*each {
		t.Fatalf("%d of %d datagrams arrived", got, writers*each)
	}
}

func TestWritesAfterCloseFailLikeTheRuntime(t *testing.T) {
	r := newReceiver(t, "udp4", "127.0.0.1")
	w := Wrap(listen(t, "udp4", "127.0.0.1"))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	_, err := w.WriteTo([]byte("x"), net.UDPAddrFromAddrPort(r.addr()))
	if !errors.Is(err, net.ErrClosed) {
		t.Fatalf("write after close: %v, want net.ErrClosed", err)
	}
}

func TestCloseWaitsForWritesInProgress(t *testing.T) {
	r := newReceiver(t, "udp4", "127.0.0.1")
	w := Wrap(listen(t, "udp4", "127.0.0.1"))
	ua := net.UDPAddrFromAddrPort(r.addr())
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for {
				if _, err := w.WriteTo([]byte("x"), ua); err != nil {
					if !errors.Is(err, net.ErrClosed) {
						t.Errorf("write racing close: %v", err)
					}
					return
				}
			}
		})
	}
	time.Sleep(10 * time.Millisecond)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
}

func TestNonUDPAddressTakesTheRuntimePath(t *testing.T) {
	w := Wrap(listen(t, "udp4", "127.0.0.1"))
	defer w.Close()
	if _, err := w.WriteTo([]byte("x"), &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9}); err == nil {
		t.Fatal("a TCP address was accepted")
	}
}

// BenchmarkParallelSend sends from GOMAXPROCS goroutines through one socket,
// as the SFU's forwarding goroutines do through its mux socket. "runtime" is
// *net.UDPConn, whose writes queue on the descriptor's write lock; "direct" is
// Conn (the same as "runtime" on platforms other than Linux).
func BenchmarkParallelSend(b *testing.B) {
	for _, mode := range []string{"runtime", "direct"} {
		b.Run(mode, func(b *testing.B) {
			r := newReceiver(b, "udp4", "127.0.0.1")
			c := listen(b, "udp4", "127.0.0.1")
			var w net.PacketConn = c
			if mode == "direct" {
				w = Wrap(c)
			}
			defer w.Close()
			ua := net.UDPAddrFromAddrPort(r.addr())
			payload := make([]byte, 1200)
			b.SetBytes(int64(len(payload)))
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					_, _ = w.WriteTo(payload, ua)
				}
			})
		})
	}
}
