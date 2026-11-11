package sfu

import (
	"testing"

	"github.com/pion/webrtc/v4"
)

func TestSelectedPairWithoutTransceiver(t *testing.T) {
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	pair, err := selectedPair(pc)
	if err != nil || pair != nil {
		t.Fatalf("unconnected peer pair = %v, %v", pair, err)
	}
	if err := pc.Close(); err != nil {
		t.Fatal(err)
	}
}
