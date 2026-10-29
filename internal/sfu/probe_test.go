package sfu

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

func newLoopbackSFU(t *testing.T) *SFU {
	t.Helper()
	s, err := NewSFU(0, 0, []string{"127.0.0.1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// dataChannelOffer builds a browser-like offer with one data channel and all
// candidates gathered.
func dataChannelOffer(t *testing.T, from *SFU) (*webrtc.PeerConnection, string) {
	t.Helper()
	pc, err := from.currentAPI().NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	if _, err := pc.CreateDataChannel("probe", nil); err != nil {
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
	return pc, pc.LocalDescription().SDP
}

func TestProbeConnectsAndTellsTheObserverWithoutCreatingARoom(t *testing.T) {
	server, client := newLoopbackSFU(t), newLoopbackSFU(t)
	seen := make(chan ICEConnected, 4)
	server.SetICEConnectedHandler(func(ev ICEConnected) { seen <- ev })

	pc, offer := dataChannelOffer(t, client)
	connected := make(chan struct{})
	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		if s == webrtc.PeerConnectionStateConnected {
			close(connected)
		}
	})

	answer, err := server.Probe(context.Background(), offer)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(answer, "a=candidate:") {
		t.Fatalf("answer carries no candidates:\n%s", answer)
	}
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: answer}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-connected:
	case <-time.After(15 * time.Second):
		t.Fatal("the probe never connected")
	}
	select {
	case ev := <-seen:
		if !ev.Remote.IsLoopback() || ev.RemoteType == "" {
			t.Fatalf("observer got %+v, want a loopback remote with a type", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the ICE observer heard nothing")
	}
	if n := len(server.rooms); n != 0 {
		t.Fatalf("a probe created %d rooms", n)
	}
}

func TestProbeRefusesWhatIsNotADataChannelOffer(t *testing.T) {
	s := newLoopbackSFU(t)
	pc, err := s.currentAPI().NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pc.Close() }()
	if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio); err != nil {
		t.Fatal(err)
	}
	offer, _ := pc.CreateOffer(nil)
	for name, sdp := range map[string]string{"audio": offer.SDP, "empty": "", "garbage": "v=0\nnot sdp", "huge": strings.Repeat("a", maxProbeOfferBytes+1)} {
		if _, err := s.Probe(context.Background(), sdp); !errors.Is(err, ErrProbeInvalid) {
			t.Errorf("%s: err = %v, want ErrProbeInvalid", name, err)
		}
	}
}

func TestProbeDropsCandidatesTheTricklePathRefuses(t *testing.T) {
	const offer = "v=0\r\no=- 1 2 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\na=group:BUNDLE 0\r\n" +
		"m=application 9 UDP/DTLS/SCTP webrtc-datachannel\r\nc=IN IP4 0.0.0.0\r\na=mid:0\r\na=sctp-port:5000\r\n" +
		"a=ice-ufrag:abcd\r\na=ice-pwd:abcdefghijklmnopqrstuvwx\r\na=fingerprint:sha-256 00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF\r\na=setup:actpass\r\n" +
		"a=candidate:1 1 udp 2130706431 203.0.113.9 4000 typ host\r\n" +
		"a=candidate:2 1 udp 2130706431 169.254.1.1 4000 typ host\r\n" +
		"a=candidate:3 1 udp 2130706431 abc.local 4000 typ host\r\n" +
		"a=candidate:4 1 udp 2130706431 127.0.0.1 4000 typ host\r\n"
	got, err := vetProbeOffer(offer, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "203.0.113.9") {
		t.Error("the public candidate was dropped")
	}
	for _, bad := range []string{"169.254.1.1", "abc.local", "127.0.0.1 4000"} {
		if strings.Contains(got, bad) {
			t.Errorf("candidate %q survived the vetting", bad)
		}
	}
}

func TestProbeIsCappedAndNeverOnAClosedSFU(t *testing.T) {
	s := newLoopbackSFU(t)
	_, offer := dataChannelOffer(t, newLoopbackSFU(t))
	s.probes.Store(maxProbes)
	if _, err := s.Probe(context.Background(), offer); !errors.Is(err, ErrProbeBusy) {
		t.Fatalf("err = %v, want ErrProbeBusy", err)
	}
	if got := s.probes.Load(); got != maxProbes {
		t.Fatalf("a refused probe left the counter at %d", got)
	}
	s.probes.Store(0)
	_ = s.Close()
	if _, err := s.Probe(context.Background(), offer); err == nil {
		t.Fatal("a closed SFU accepted a probe")
	}
}
