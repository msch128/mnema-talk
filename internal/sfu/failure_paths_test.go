package sfu

import (
	"testing"

	"github.com/pion/webrtc/v4"
)

func TestPinCodecOrderSkipsAbsentTransceivers(t *testing.T) {
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	transceiver, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo)
	if err != nil {
		t.Fatal(err)
	}
	// An absent camera or screen line is skipped, the present one still pinned.
	pinCodecOrder(nil, transceiver)
	pinned := transceiver.Receiver().GetParameters().Codecs
	if len(pinned) == 0 {
		t.Fatal("pinning dropped the registered codecs")
	}
}
