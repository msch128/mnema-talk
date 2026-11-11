package sfu

import (
	"testing"

	"time"

	"github.com/google/uuid"
	"github.com/pion/rtcp"
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

	for _, source := range []Source{SourceScreen, SourceCamera} {
		for _, info := range r.trackLocals {
			info.lastPLI.Store(0)
		}
		r.RequestSourceKeyframe(sharer, source)
		for _, info := range r.trackLocals {
			want := info.SenderID == sharer && info.Source == source
			if got := info.lastPLI.Load() != 0; got != want {
				t.Errorf("request %s: sharer=%v source=%s: keyframe requested %v, want %v", source, info.SenderID == sharer, info.Source, got, want)
			}
		}
	}
}

func TestSubscriberFullIntraRequestReachesPublisher(t *testing.T) {
	s := newTestSFU(t)
	roomID := uuid.New()
	publisher := newClient(t)
	publisher.publishVideo("feedback-screen")
	publisher.join(s, roomID)
	r := s.Room(roomID)
	viewer := newClient(t)
	viewer.join(s, roomID)
	if err := r.Subscribe(viewer.id, publisher.id, SourceScreen, false, true); err != nil {
		t.Fatal(err)
	}
	remote := waitTrack(t, viewer)
	// Wait out the keyframe throttle after the initial subscription's PLI,
	// then drain it so the assertion concerns only the subscriber feedback.
	time.Sleep(keyframeMinInterval + 50*time.Millisecond)
	for len(publisher.plis) > 0 {
		<-publisher.plis
	}
	if err := viewer.pc.WriteRTCP([]rtcp.Packet{&rtcp.FullIntraRequest{
		MediaSSRC: uint32(remote.SSRC()),
		FIR:       []rtcp.FIREntry{{SSRC: uint32(remote.SSRC()), SequenceNumber: 1}},
	}}); err != nil {
		t.Fatal(err)
	}
	select {
	case ssrc := <-publisher.plis:
		if ssrc == 0 {
			t.Fatal("feedback did not target the publisher's SSRC")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("subscriber FIR did not request a publisher keyframe")
	}
}
