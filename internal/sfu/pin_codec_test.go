package sfu

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/pion/webrtc/v4"
)

// Pinning keeps the codec order of the first offer when the client answers
// with another one on its video line (the web client puts H.264 first on its
// screen line). Without it the re-offer follows the answer and a browser
// switches its running camera to a codec the forwarded track does not carry.
func TestPinCodecOrderKeepsTheFirstOffersOrderAcrossAnAnswer(t *testing.T) {
	logs := captureSFULog(t)
	newPC := func() *webrtc.PeerConnection {
		pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = pc.Close() })
		return pc
	}
	server, client := newPC(), newPC()
	tr, err := server.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly})
	if err != nil {
		t.Fatal(err)
	}
	// An absent camera or screen line is skipped, the present one still pinned.
	pinCodecOrder(slog.Default(), nil, tr)

	first, err := server.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.SetLocalDescription(first); err != nil {
		t.Fatal(err)
	}
	if err := client.SetRemoteDescription(first); err != nil {
		t.Fatal(err)
	}
	answer, err := client.CreateAnswer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SetLocalDescription(answer); err != nil {
		t.Fatal(err)
	}
	answer.SDP = preferH264OnFirstVideoLine(answer.SDP)
	if got := firstCodec(t, videoSections(answer.SDP)[0]); got != "H264" {
		t.Fatalf("the munged answer starts with %s", got)
	}
	if err := server.SetRemoteDescription(answer); err != nil {
		t.Fatal(err)
	}

	again, err := server.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	want, got := firstCodec(t, videoSections(first.SDP)[0]), firstCodec(t, videoSections(again.SDP)[0])
	if want == "H264" || got != want {
		t.Fatalf("the re-offer starts with %s, the first offer with %s", got, want)
	}
	if strings.Contains(logs.String(), "sfu pin codec order") {
		t.Fatalf("pinning failed: %s", logs.String())
	}
}
