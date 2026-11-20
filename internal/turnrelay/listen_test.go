package turnrelay

import (
	"net"
	"runtime"
	"testing"

	"github.com/mnema-xyz/talk/internal/fastudp"
)

// Everything relayed to members over UDP leaves through the relay's one
// socket; on Linux its sends must not queue on the runtime's write lock.
func TestRelaySocketSendsConcurrentlyOnLinux(t *testing.T) {
	c, err := listenRelayUDP("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	fast, ok := c.(*fastudp.Conn)
	if runtime.GOOS == "linux" && (!ok || !fast.Direct()) {
		t.Fatalf("relay socket %T does not send directly", c)
	}
	if runtime.GOOS != "linux" && ok {
		t.Fatalf("relay socket wrapped on %s", runtime.GOOS)
	}
}

// Start must open the relay's UDP socket through listenRelayUDP.
func TestStartListensThroughTheFastSocket(t *testing.T) {
	var conns []net.PacketConn
	prev := relayUDP
	relayUDP = func(addr string) (net.PacketConn, error) {
		c, err := listenRelayUDP(addr)
		if err == nil {
			conns = append(conns, c)
		}
		return c, err
	}
	t.Cleanup(func() { relayUDP = prev })
	newHarness(t, harnessOpts{})
	if len(conns) == 0 {
		t.Fatal("Start opened its UDP socket without listenRelayUDP")
	}
	if _, ok := conns[len(conns)-1].(*fastudp.Conn); runtime.GOOS == "linux" && !ok {
		t.Fatalf("relay socket %T is not direct", conns[len(conns)-1])
	}
}
