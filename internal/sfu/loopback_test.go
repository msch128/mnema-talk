package sfu

import (
	"errors"
	"net"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

// Rewriting a LAN socket's address to loopback produces an unreachable local
// candidate. At least one advertised loopback endpoint must have a socket
// bound there. Keep the connection open while checking its gathered ports.
func TestLoopbackAnnouncementHasBoundCandidate(t *testing.T) {
	s, err := NewSFU(0, 0, []string{"127.0.0.1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	pc, err := s.currentAPI().NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio); err != nil {
		t.Fatal(err)
	}
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	done := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("ICE gathering timed out")
	}
	for line := range strings.Lines(pc.LocalDescription().SDP) {
		if !strings.HasPrefix(line, "a=candidate:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 8 || strings.ToLower(fields[2]) != "udp" || fields[4] != "127.0.0.1" {
			continue
		}
		port, err := strconv.Atoi(fields[5])
		if err != nil {
			t.Fatal(err)
		}
		conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
		if errors.Is(err, syscall.EADDRINUSE) {
			return // An actual socket occupies this advertised endpoint.
		}
		if err != nil {
			t.Fatal(err)
		}
		_ = conn.Close()
	}
	t.Fatal("no gathered loopback endpoint has a bound socket")
}
