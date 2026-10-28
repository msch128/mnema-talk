package sfu

import (
	"errors"
	"testing"

	"github.com/pion/webrtc/v4"
)

// failingBindTrack is a TrackLocal whose negotiation always fails, like a
// viewer whose browser cannot receive the publisher's codec.
type failingBindTrack struct{ webrtc.TrackLocal }

func (failingBindTrack) Bind(webrtc.TrackLocalContext) (webrtc.RTPCodecParameters, error) {
	return webrtc.RTPCodecParameters{}, errors.New("codec rejected")
}

func TestFailedNativeBindReleasesItsSourceAuthorization(t *testing.T) {
	sources := &sourceAuthorizationRegistry{}
	guard := &authorizedTrack{TrackLocal: failingBindTrack{}, authorize: func() bool { return true }, source: func() bool { return true }, sources: sources}
	ctx := completeAuthorizationContext{authorizationContext: authorizationContext{id: "failed-bind", writer: &authorizationWriter{}}}
	if _, err := guard.Bind(ctx); err == nil || err.Error() != "codec rejected" {
		t.Fatalf("failed bind: %v", err)
	}
	if sources.capture("failed-bind") != nil {
		t.Fatal("failed bind left a pending source authorization")
	}
	if _, ok := guard.bindings.Load("failed-bind"); ok {
		t.Fatal("failed bind kept its binding")
	}
}

func TestSourceAuthorizationRemovalDropsOnlyTheMatchingBinding(t *testing.T) {
	sources := &sourceAuthorizationRegistry{}
	binding := sources.bind("exact", func() bool { return true })
	sources.remove("exact", binding)
	if sources.capture("exact") != nil {
		t.Fatal("removed binding is still pending")
	}
}

func TestNativeAPIRequiresAuthorizationAndALiveSFU(t *testing.T) {
	s := newTestSFU(t)
	if _, err := s.nativeAPI(nil); !errors.Is(err, errSFUClosed) {
		t.Fatalf("native API without authorization: %v", err)
	}
	if api, err := s.nativeAPI(func() bool { return true }); err != nil || api == nil {
		t.Fatalf("native API: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.nativeAPI(func() bool { return true }); !errors.Is(err, errSFUClosed) {
		t.Fatalf("native API on a closed SFU: %v", err)
	}
}

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
