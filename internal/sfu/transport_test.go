package sfu

import (
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

func unusedLoopbackUDPPort(t *testing.T) uint16 {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	port := uint16(conn.LocalAddr().(*net.UDPAddr).Port)
	_ = conn.Close()
	return port
}

func newLoopbackMuxSFU(t *testing.T, port uint16) *SFU {
	t.Helper()
	s, err := NewSFU(port, port, []string{"127.0.0.1"}, nil, WithUDPMuxPort(port))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}

// Several concurrent ICE transports must use the same published UDP port;
// changing an announced address must rebuild only the API, not bind again.
func TestUDPMuxSharedPortAndAPIRebuild(t *testing.T) {
	port := unusedLoopbackUDPPort(t)
	s := newLoopbackMuxSFU(t, port)
	for i := range 12 {
		if i == 6 {
			if err := s.SetAnnouncedIPs([]string{"127.0.0.1"}); err != nil {
				t.Fatal(err)
			}
		}
		pc, err := s.currentAPI().NewPeerConnection(webrtc.Configuration{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = pc.Close() })
		if _, err := pc.CreateDataChannel("mux", nil); err != nil {
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
		case <-time.After(5 * time.Second):
			t.Fatal("gathering timed out")
		}
		candidates := 0
		for line := range strings.Lines(pc.LocalDescription().SDP) {
			fields := strings.Fields(line)
			if !strings.HasPrefix(line, "a=candidate:") || len(fields) < 8 {
				continue
			}
			if strings.ToLower(fields[2]) == "udp" {
				candidates++
				if fields[5] != strconv.Itoa(int(port)) {
					t.Fatalf("peer %d uses port %s rather than shared port %d", i, fields[5], port)
				}
			}
		}
		if candidates == 0 {
			t.Fatal("no UDP candidates")
		}
	}
}

// Multiple authenticated DTLS/SCTP connections on the shared socket must
// deliver each peer's payload to its own connection, not another peer.
func TestUDPMuxRoutesConcurrentConnections(t *testing.T) {
	s := newLoopbackMuxSFU(t, unusedLoopbackUDPPort(t))
	var settings webrtc.SettingEngine
	settings.SetIncludeLoopbackCandidate(true)
	settings.SetIPFilter(func(ip net.IP) bool { return ip.IsLoopback() })
	settings.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	clientAPI := webrtc.NewAPI(webrtc.WithSettingEngine(settings))
	for i := range 4 {
		serverPC, err := s.currentAPI().NewPeerConnection(webrtc.Configuration{})
		if err != nil {
			t.Fatal(err)
		}
		clientPC, err := clientAPI.NewPeerConnection(webrtc.Configuration{})
		if err != nil {
			_ = serverPC.Close()
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = clientPC.Close(); _ = serverPC.Close() })
		payload := "peer-" + strconv.Itoa(i)
		received := make(chan string, 1)
		serverPC.OnDataChannel(func(dc *webrtc.DataChannel) {
			dc.OnMessage(func(msg webrtc.DataChannelMessage) { received <- string(msg.Data) })
		})
		dc, err := clientPC.CreateDataChannel(payload, nil)
		if err != nil {
			t.Fatal(err)
		}
		dc.OnOpen(func() { _ = dc.SendText(payload) })
		offer, err := clientPC.CreateOffer(nil)
		if err != nil {
			t.Fatal(err)
		}
		clientDone := webrtc.GatheringCompletePromise(clientPC)
		if err := clientPC.SetLocalDescription(offer); err != nil {
			t.Fatal(err)
		}
		select {
		case <-clientDone:
		case <-time.After(5 * time.Second):
			t.Fatal("client gathering timed out")
		}
		if err := serverPC.SetRemoteDescription(*clientPC.LocalDescription()); err != nil {
			t.Fatal(err)
		}
		answer, err := serverPC.CreateAnswer(nil)
		if err != nil {
			t.Fatal(err)
		}
		serverDone := webrtc.GatheringCompletePromise(serverPC)
		if err := serverPC.SetLocalDescription(answer); err != nil {
			t.Fatal(err)
		}
		select {
		case <-serverDone:
		case <-time.After(5 * time.Second):
			t.Fatal("server gathering timed out")
		}
		if err := clientPC.SetRemoteDescription(*serverPC.LocalDescription()); err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-received:
			if got != payload {
				t.Fatalf("received %q, want %q", got, payload)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("shared-port data channel did not deliver")
		}
	}
}

func TestUDPMuxCloseReleasesPortAndRejectsJoin(t *testing.T) {
	port := unusedLoopbackUDPPort(t)
	s := newLoopbackMuxSFU(t, port)
	_, peer, err := s.Join(uuid.New(), uuid.New(), func(webrtc.SessionDescription) {}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if peer.PC.ConnectionState() != webrtc.PeerConnectionStateClosed {
		t.Fatal("peer still open after shutdown")
	}
	if err := s.Close(); err != nil {
		t.Fatal("repeated close:", err)
	}
	if _, _, err := s.Join(uuid.New(), uuid.New(), nil, nil); !errors.Is(err, errSFUClosed) {
		t.Fatalf("join after close: %v", err)
	}
	if err := s.SetAnnouncedIPs([]string{"127.0.0.1"}); !errors.Is(err, errSFUClosed) {
		t.Fatalf("API rebuild after close: %v", err)
	}
	newLoopbackMuxSFU(t, port) // Rebind proves all interface sockets were closed.
}

func TestUDPMuxStartupFailure(t *testing.T) {
	if _, err := NewSFU(50000, 50050, nil, nil, WithUDPMuxPort(50051)); err == nil {
		t.Fatal("unpublished mux port accepted")
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	port := uint16(conn.LocalAddr().(*net.UDPAddr).Port)
	if _, err := NewSFU(port, port, []string{"127.0.0.1"}, nil, WithUDPMuxPort(port)); err == nil {
		t.Fatal("occupied mux port accepted")
	}
}

func TestUDPMuxInterfaceModeNeedsRestart(t *testing.T) {
	s := newLoopbackMuxSFU(t, unusedLoopbackUDPPort(t))
	if err := s.SetAnnouncedIPs([]string{"203.0.113.1"}); err == nil {
		t.Fatal("mux cannot bind new interfaces during API rebuild")
	}
	if ips := s.AnnouncedIPs(); len(ips) != 1 || ips[0] != "127.0.0.1" {
		t.Fatal("failed API update replaced announced state")
	}
}

func TestOnlyLoopbackAnnouncements(t *testing.T) {
	for _, ips := range [][]string{nil, {}, {"203.0.113.1"}, {"127.0.0.1", "203.0.113.1"}, {"localhost"}} {
		if onlyLoopbackAnnouncements(ips) {
			t.Errorf("production or unresolved address list filtered: %v", ips)
		}
	}
	if !onlyLoopbackAnnouncements([]string{"127.0.0.1", "::1"}) {
		t.Fatal("explicit loopback list not detected")
	}
}
