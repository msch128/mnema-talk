package sfu

import (
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pion/interceptor"
	"github.com/pion/webrtc/v4"
)

func TestValidLayers(t *testing.T) {
	good := []Layer{{3840, 2160, 60}, {1920, 1080, 30}, {1280, 720, 30}}
	if !validLayers(good) || !validLayers(good[:1]) {
		t.Fatal("a sensible ladder was refused")
	}
	for name, l := range map[string][]Layer{
		"none":             nil,
		"too many":         {{1000, 400, 30}, {900, 300, 30}, {800, 200, 30}, {700, 100, 30}},
		"zero width":       {{0, 100, 30}},
		"huge width":       {{maxLayerDimension + 1, 100, 30}},
		"zero height":      {{100, 0, 30}},
		"huge height":      {{100, maxLayerDimension + 1, 30}},
		"zero fps":         {{100, 100, 0}},
		"huge fps":         {{100, 100, maxLayerFPS + 1}},
		"not shrinking":    {{1920, 1080, 30}, {1280, 1080, 30}},
		"growing":          {{1280, 720, 30}, {1920, 1080, 30}},
		"negative":         {{-1, -1, -1}},
		"second one wrong": {{1920, 1080, 30}, {0, 0, 0}},
	} {
		if validLayers(l) {
			t.Errorf("%s accepted", name)
		}
	}
}

// idleRemote is a publisher track that sends nothing until it is closed.
type idleRemote struct{ done chan struct{} }

func newIdleRemote(t *testing.T) *idleRemote {
	r := &idleRemote{done: make(chan struct{})}
	t.Cleanup(func() { close(r.done) })
	return r
}

func (r *idleRemote) Read([]byte) (int, interceptor.Attributes, error) {
	<-r.done
	return 0, nil, io.EOF
}

func TestReceiveScreenLayerRefusals(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()
	user := uuid.New()
	_, peer, err := s.Join(room, user, func(webrtc.SessionDescription) {}, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := s.Room(room)
	codec := webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: videoClockRate}

	// The first layer makes the group, a second one joins it.
	r.receiveScreenLayer(peer, newIdleRemote(t), "t", 11, codec, 0)
	r.receiveScreenLayer(peer, newIdleRemote(t), "t", 12, codec, 1)
	waitFor(t, func() bool { return r.sourceCount(user, SourceScreen) == 2 })
	r.mu.RLock()
	g := r.groups[user]
	r.mu.RUnlock()
	if g == nil || g.live.Load() != 0b11 {
		t.Fatalf("group %v live %b", g, func() uint32 {
			if g == nil {
				return 0
			}
			return g.live.Load()
		}())
	}

	// A layer in another codec would hand viewers pictures they cannot decode.
	r.receiveScreenLayer(peer, &fakeRemote{n: 3}, "t", 13, webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: videoClockRate}, 2)
	if g.live.Load() != 0b11 {
		t.Fatalf("an H.264 layer joined a VP8 share: %b", g.live.Load())
	}

	// A track of a peer that has since been replaced is ignored.
	_, fresh, err := s.Join(room, user, func(webrtc.SessionDescription) {}, nil)
	if err != nil || fresh == peer {
		t.Fatal(err)
	}
	r.receiveScreenLayer(peer, &fakeRemote{n: 3}, "t", 14, codec, 0)
	r.mu.RLock()
	stale := r.groups[user]
	r.mu.RUnlock()
	if stale != nil {
		t.Fatal("a replaced connection's track made a group")
	}
}

func TestScreenLayerApiRefusals(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()
	pub, viewer := newClient(t), newClient(t)
	pub.join(s, room)
	viewer.join(s, room)
	r := s.Room(room)

	if err := r.SetScreenLayers(pub.id, nil); err == nil {
		t.Error("empty layers accepted")
	}
	if err := r.SetScreenLayers(uuid.New(), []Layer{{100, 100, 30}}); err == nil {
		t.Error("layers of someone who is not in the room accepted")
	}
	// Before any packet there is no group; the layers are remembered.
	if err := r.SetScreenLayers(pub.id, []Layer{{100, 100, 30}}); err != nil {
		t.Fatal(err)
	}

	for name, args := range map[string][3]any{
		"below auto":   {viewer.id, pub.id, -2},
		"above layers": {viewer.id, pub.id, maxLayers},
		"stranger":     {uuid.New(), pub.id, 0},
		"own share":    {viewer.id, viewer.id, 0},
	} {
		if err := r.SetScreenQuality(args[0].(uuid.UUID), args[1].(uuid.UUID), args[2].(int)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	// A choice made before the share exists is kept for it.
	if err := r.SetScreenQuality(viewer.id, pub.id, 1); err != nil {
		t.Fatal(err)
	}
	if q := r.subs[viewer.id].screenQuality(pub.id); q != 1 {
		t.Fatalf("stored quality %d", q)
	}
	if _, ok := r.ScreenQuality(viewer.id, pub.id); ok {
		t.Error("quality reported without a share")
	}
	// A share without this viewer's track.
	pub2 := newClient(t)
	pub2.publishLayers(25)
	pub2.join(s, room)
	waitSources(t, r, pub2.id, SourceScreen, 3)
	if _, ok := r.ScreenQuality(viewer.id, pub2.id); ok {
		t.Error("quality reported for a share the viewer does not watch")
	}
	if err := r.SetScreenQuality(viewer.id, pub2.id, 0); err != nil {
		t.Fatal(err)
	}
}

func TestSubscriptionQuality(t *testing.T) {
	var nilSubs *Subscriptions
	if nilSubs.screenQuality(uuid.New()) != -1 {
		t.Error("nil subscriptions must be automatic")
	}
	s := &Subscriptions{}
	a := uuid.New()
	if s.screenQuality(a) != -1 {
		t.Error("default must be automatic")
	}
	s.setScreenQuality(a, 2)
	if s.screenQuality(a) != 2 {
		t.Error("choice lost")
	}
	s.setScreenQuality(a, -1)
	if s.screenQuality(a) != -1 || len(s.quality) != 0 {
		t.Error("automatic must forget the choice")
	}
	for i := 0; i < maxScreenSubs+5; i++ {
		s.setScreenQuality(uuid.New(), 1)
	}
	if len(s.quality) != maxScreenSubs {
		t.Fatalf("%d remembered choices, want at most %d", len(s.quality), maxScreenSubs)
	}
	// Existing entries can still change when the map is full.
	for id := range s.quality {
		s.setScreenQuality(id, 0)
		if s.screenQuality(id) != 0 {
			t.Error("full map refused a change")
		}
		s.dropScreen(id)
		break
	}
	if len(s.quality) != maxScreenSubs-1 {
		t.Error("dropScreen kept the choice")
	}
}

func TestTrackInfoCodecMime(t *testing.T) {
	g := newLayerGroup("g", uuid.New(), uuid.New(), nil, webrtc.RTPCodecCapability{MimeType: "video/VP8"})
	if got := (&TrackInfo{group: g}).codecMime(); got != "video/VP8" {
		t.Errorf("group codec %q", got)
	}
	if got := (&TrackInfo{}).codecMime(); got != "" {
		t.Errorf("no codec %q", got)
	}
	local, _ := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: "video/H264"}, "t", "s")
	if got := (&TrackInfo{Track: local}).codecMime(); got != "video/H264" {
		t.Errorf("track codec %q", got)
	}
}

// A viewer asking for a keyframe (the browser does when a video starts) is
// served by the layer it receives, not by every layer of the share.
func TestDispatchKeyframeForLayeredShare(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()
	pub := newClient(t)
	layers := pub.publishLayers(25)
	pub.join(s, room)
	viewer := newClient(t)
	viewer.join(s, room)
	r := s.Room(room)
	waitSources(t, r, pub.id, SourceScreen, 3)
	if err := r.SetScreenQuality(viewer.id, pub.id, 1); err != nil {
		t.Fatal(err)
	}
	if err := r.Subscribe(viewer.id, pub.id, SourceScreen, false, true); err != nil {
		t.Fatal(err)
	}
	waitTrack(t, viewer)
	waitLayer(t, viewer, 1)

	time.Sleep(keyframeMinInterval + 50*time.Millisecond)
	for _, l := range layers {
		for len(l.plis) > 0 {
			<-l.plis
		}
	}
	r.DispatchKeyframe(viewer.id)
	select {
	case <-layers[1].plis:
	case <-time.After(3 * time.Second):
		t.Fatal("the layer the viewer receives was not asked for a keyframe")
	}
	for _, i := range []int{0, 2} {
		select {
		case <-layers[i].plis:
			t.Fatalf("layer %d was asked for a keyframe nobody needs", i)
		case <-time.After(100 * time.Millisecond):
		}
	}
	// Someone who watches nothing triggers no request.
	other := newClient(t)
	other.join(s, room)
	r.DispatchKeyframe(other.id)
}

// Stopping and restarting a share ends and renews every viewer's track; the
// viewer's layer choice is gone with the share.
func TestStoppedLayeredShareResumesForItsViewers(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()
	pub := newClient(t)
	pub.publishLayers(25)
	pub.join(s, room)
	viewer := newClient(t)
	viewer.join(s, room)
	r := s.Room(room)
	waitSources(t, r, pub.id, SourceScreen, 3)
	if err := r.Subscribe(viewer.id, pub.id, SourceScreen, false, true); err != nil {
		t.Fatal(err)
	}
	waitTrack(t, viewer)
	waitLayer(t, viewer, 0)

	r.RemoveUserSource(pub.id, SourceScreen)
	if r.sourceCount(pub.id, SourceScreen) != 0 {
		t.Fatal("a stopped share is still published")
	}
	// The publisher's encoders keep running: the share is published again
	// and its viewer has to opt in again.
	waitSources(t, r, pub.id, SourceScreen, 3)
	if r.Receives(viewer.id, pub.id, SourceScreen) {
		t.Fatal("the opt-in survived the stop")
	}
	gather(viewer, 300*time.Millisecond) // what the old track still delivered
	if err := r.Subscribe(viewer.id, pub.id, SourceScreen, false, true); err != nil {
		t.Fatal(err)
	}
	checkOneStream(t, waitLayer(t, viewer, 0))
}

// Leaving the call releases the viewer's tracks of a layered share.
func TestLeavingReleasesLayeredViewerTracks(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()
	pub := newClient(t)
	pub.publishLayers(25)
	pub.join(s, room)
	viewer := newClient(t)
	viewer.join(s, room)
	r := s.Room(room)
	waitSources(t, r, pub.id, SourceScreen, 3)
	_ = r.Subscribe(viewer.id, pub.id, SourceScreen, false, true)
	waitTrack(t, viewer)
	r.mu.RLock()
	g := r.groups[pub.id]
	r.mu.RUnlock()
	waitFor(t, func() bool { return g.viewerFor(viewer.id) != nil })

	s.RemovePeer(room, viewer.peer)
	waitFor(t, func() bool { return g.viewerFor(viewer.id) == nil })
	// The publisher leaving ends the group.
	s.RemovePeer(room, pub.peer)
	if !g.superseded.Load() {
		t.Fatal("the group outlived its publisher")
	}
}
