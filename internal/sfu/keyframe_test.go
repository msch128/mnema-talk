package sfu

import (
	"testing"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

// RequestSourceKeyframe targets only the publisher's own video of that
// source, not the videos they watch or their other source.
func TestRequestSourceKeyframeTargetsPublisherSource(t *testing.T) {
	sharer, other := uuid.New(), uuid.New()
	r := &Room{trackLocals: map[string]*TrackInfo{}}
	peers := map[uuid.UUID]*Peer{}
	for _, id := range []uuid.UUID{sharer, other} {
		pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = pc.Close() })
		peers[id] = &Peer{ID: id, PC: pc, room: r}
	}
	for _, pub := range []struct {
		user   uuid.UUID
		source Source
	}{{sharer, SourceScreen}, {sharer, SourceCamera}, {other, SourceScreen}} {
		key := trackKey(pub.user, string(pub.source))
		r.trackLocals[key] = &TrackInfo{SenderID: pub.user, Kind: webrtc.RTPCodecTypeVideo, Source: pub.source, publisher: peers[pub.user]}
	}

	r.RequestSourceKeyframe(sharer, SourceScreen)
	for _, info := range r.trackLocals {
		want := info.SenderID == sharer && info.Source == SourceScreen
		if got := info.lastPLI.Load() != 0; got != want {
			t.Errorf("sharer=%v source=%s: keyframe requested %v, want %v", info.SenderID == sharer, info.Source, got, want)
		}
	}
}
