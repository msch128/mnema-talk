package turnrelay

import (
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
