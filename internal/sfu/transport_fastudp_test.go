package sfu

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/mnema-xyz/talk/internal/fastudp"
	"github.com/mnema-xyz/talk/internal/metrics"
	"github.com/pion/logging"
	"github.com/pion/transport/v5/packetio"
)

// The mux socket carries every member's media; on Linux its sends must not
// queue on the runtime's write lock (doc/benchmark.md, mixed Talk on the N100).
func TestMuxSocketSendsConcurrentlyOnLinux(t *testing.T) {
	n, err := newFastNet()
	if err != nil {
		t.Fatal(err)
	}
	// Pion's mux listens like this: network "udp", one interface address.
	c, err := n.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	fast, ok := c.(*fastudp.Conn)
	if runtime.GOOS == "linux" && (!ok || !fast.Direct()) {
		t.Fatalf("mux socket %T does not send directly", c)
	}
	if runtime.GOOS != "linux" && ok {
		t.Fatalf("mux socket wrapped on %s", runtime.GOOS)
	}
}

// A publisher's packet that does not fit the track's buffer is dropped by Pion
// without a word; the SFU counts it, so a reader that falls behind shows.
func TestFullTrackBufferCountsDrops(t *testing.T) {
	start := metrics.SFUIngressDropped.Sum(0, "buffer")
	buf := srtpBuffer(packetio.RTPBufferPacket, 1)
	defer buf.Close()
	pkt := make([]byte, 1200)
	// srtp writes through WriteWithAttributes when the buffer has it.
	aw, ok := buf.(interface {
		WriteWithAttributes([]byte, packetio.Attributes) (int, error)
	})
	if !ok {
		t.Fatal("the track buffer lost WriteWithAttributes")
	}
	for range rtpBufferBytes/len(pkt) + 10 {
		_, _ = aw.WriteWithAttributes(pkt, nil)
	}
	if metrics.SFUIngressDropped.Sum(0, "buffer") == start {
		t.Fatal("no drop counted for a full track buffer")
	}
	rtcp := srtpBuffer(packetio.RTCPBufferPacket, 1)
	defer rtcp.Close()
	before := metrics.SFUIngressDropped.Sum(0, "buffer")
	for range rtcpBufferPackets + 10 {
		_, _ = rtcp.Write(pkt[:100])
	}
	if metrics.SFUIngressDropped.Sum(0, "buffer") != before {
		t.Fatal("RTCP buffer drops counted as media drops")
	}
}

func TestMuxLoggerCountsEndpointDrops(t *testing.T) {
	before := metrics.SFUIngressDropped.Sum(0, "mux")
	c := newRTCPStreamCap(logging.NewDefaultLoggerFactory(), joinLogger(uuid.New(), ""))
	c.NewLogger("mux").Infof(pionMuxFull)
	c.NewLogger("mux").Infof("something else")
	if got := metrics.SFUIngressDropped.Sum(0, "mux") - before; got != 1 {
		t.Fatalf("mux drops = %d, want 1", got)
	}
}

// The counters match Pion's log lines word for word; a Pion that rewords one
// would make its counter read zero for ever.
func TestPionLogLinesStillMatch(t *testing.T) {
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/pion/webrtc/v4").Output()
	dir := strings.TrimSpace(string(out))
	if err != nil || dir == "" {
		t.Skipf("Pion's source is not available: %v", err)
	}
	for file, line := range map[string]string{
		"internal/mux/mux.go": `m.log.Infof("` + pionMuxFull + `")`,
		"peerconnection.go":   `"` + pionUnhandledRTCP + `"`,
	} {
		src, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			t.Skipf("Pion's source is not available: %v", err)
		}
		if !strings.Contains(string(src), line) {
			t.Errorf("%s no longer logs %s", file, line)
		}
	}
}
