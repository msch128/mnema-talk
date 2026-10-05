package sfu

import (
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

func TestSubscriptionsDefaults(t *testing.T) {
	pub := uuid.New()
	for _, s := range []*Subscriptions{nil, {}} {
		if !s.Wants(pub, webrtc.RTPCodecTypeAudio, SourceAudio) {
			t.Error("audio must always be forwarded")
		}
		if !s.Wants(pub, webrtc.RTPCodecTypeVideo, SourceCamera) {
			t.Error("cameras are on by default")
		}
		if s.Wants(pub, webrtc.RTPCodecTypeVideo, SourceScreen) {
			t.Error("screen shares are opt-in")
		}
		if s.Wants(pub, webrtc.RTPCodecTypeAudio, SourceScreenAudio) {
			t.Error("a screen share's sound comes with watching it")
		}
	}
}

func TestScreenAudioFollowsTheScreenSubscription(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	s := &Subscriptions{}
	_ = s.SetScreen(a, true)
	if !s.Wants(a, webrtc.RTPCodecTypeAudio, SourceScreenAudio) || s.Wants(b, webrtc.RTPCodecTypeAudio, SourceScreenAudio) {
		t.Error("only the sound of the watched share")
	}
	s.SetAllCameras(false)
	if !s.Wants(a, webrtc.RTPCodecTypeAudio, SourceScreenAudio) {
		t.Error("cameras off leaves the screen's sound alone")
	}
	_ = s.SetScreen(a, false)
	if s.Wants(a, webrtc.RTPCodecTypeAudio, SourceScreenAudio) {
		t.Error("unwatched: no sound either")
	}
	if !s.Wants(a, webrtc.RTPCodecTypeAudio, SourceAudio) {
		t.Error("the voice is always forwarded")
	}
}

func TestSubscriptionsOptOutAndIn(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	s := &Subscriptions{}

	_ = s.SetCamera(a, false)
	if s.Wants(a, webrtc.RTPCodecTypeVideo, SourceCamera) || !s.Wants(b, webrtc.RTPCodecTypeVideo, SourceCamera) {
		t.Error("only a's camera should be hidden")
	}
	_ = s.SetCamera(a, true)
	if !s.Wants(a, webrtc.RTPCodecTypeVideo, SourceCamera) {
		t.Error("camera should be back")
	}

	s.SetAllCameras(false)
	if s.Wants(b, webrtc.RTPCodecTypeVideo, SourceCamera) {
		t.Error("all cameras off")
	}
	if !s.Wants(b, webrtc.RTPCodecTypeAudio, SourceAudio) {
		t.Error("audio unaffected")
	}
	s.SetAllCameras(true)
	if !s.Wants(b, webrtc.RTPCodecTypeVideo, SourceCamera) {
		t.Error("cameras back on")
	}

	_ = s.SetScreen(a, true)
	if !s.Wants(a, webrtc.RTPCodecTypeVideo, SourceScreen) || s.Wants(b, webrtc.RTPCodecTypeVideo, SourceScreen) {
		t.Error("only a's screen is subscribed")
	}
	_ = s.SetScreen(a, false)
	if s.Wants(a, webrtc.RTPCodecTypeVideo, SourceScreen) {
		t.Error("screen unsubscribed")
	}
}

func TestSubscriptionsLimits(t *testing.T) {
	s := &Subscriptions{}
	for i := 0; i < maxScreenSubs; i++ {
		if err := s.SetScreen(uuid.New(), true); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetScreen(uuid.New(), true); err != ErrTooManySubscriptions {
		t.Fatalf("want limit error, got %v", err)
	}
	for i := 0; i < maxHiddenCameras; i++ {
		if err := s.SetCamera(uuid.New(), false); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetCamera(uuid.New(), false); err != ErrTooManySubscriptions {
		t.Fatalf("want limit error, got %v", err)
	}
}

// fakeTrack registers a published track without any real media.
func fakeTrack(r *Room, sender uuid.UUID, kind webrtc.RTPCodecType, src Source) *TrackInfo {
	codec := webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus}
	if kind == webrtc.RTPCodecTypeVideo {
		codec.MimeType = webrtc.MimeTypeVP8
	}
	local, _ := webrtc.NewTrackLocalStaticRTP(codec, trackKey(sender, string(src)), sender.String())
	info := &TrackInfo{Track: local, SenderID: sender, Kind: kind, Source: src}
	r.mu.Lock()
	info.publisher = r.peers[sender]
	r.trackLocals[trackKey(sender, string(src))] = info
	r.mu.Unlock()
	return info
}

func joinTwo(t *testing.T) (*SFU, *Room, uuid.UUID, uuid.UUID) {
	t.Helper()
	s := newTestSFU(t)
	ch := uuid.New()
	a, b := uuid.New(), uuid.New()
	room, _, err := s.Join(ch, a, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := room.JoinPeer(b, nil, nil); err != nil {
		t.Fatal(err)
	}
	return s, room, a, b
}

func TestRoomSubscribeAndFiltering(t *testing.T) {
	_, room, pub, viewer := joinTwo(t)
	cam := fakeTrack(room, pub, webrtc.RTPCodecTypeVideo, SourceCamera)
	scr := fakeTrack(room, pub, webrtc.RTPCodecTypeVideo, SourceScreen)
	mic := fakeTrack(room, pub, webrtc.RTPCodecTypeAudio, SourceAudio)

	want := func(info *TrackInfo) bool {
		room.mu.RLock()
		defer room.mu.RUnlock()
		return room.wantsLocked(viewer, info)
	}
	if !want(cam) || want(scr) || !want(mic) {
		t.Fatal("defaults: camera and audio yes, screen no")
	}
	if room.wantsLocked(pub, cam) {
		t.Fatal("never forward a track to its own publisher")
	}

	if err := room.Subscribe(viewer, pub, SourceScreen, false, true); err != nil {
		t.Fatal(err)
	}
	if !want(scr) || !room.Receives(viewer, pub, SourceScreen) {
		t.Fatal("screen subscribed")
	}
	if err := room.Subscribe(viewer, pub, SourceCamera, false, false); err != nil {
		t.Fatal(err)
	}
	if want(cam) {
		t.Fatal("camera hidden")
	}
	if err := room.Subscribe(viewer, uuid.Nil, SourceCamera, true, false); err != nil {
		t.Fatal(err)
	}
	if err := room.Subscribe(viewer, pub, Source("bogus"), false, true); err == nil {
		t.Fatal("unknown kind must be rejected")
	}

	// Stopping the share ends the opt-in: a new share starts unsubscribed.
	room.RemoveUserSource(pub, SourceScreen)
	if room.Receives(viewer, pub, SourceScreen) {
		t.Fatal("opt-in must not survive the share")
	}
}

func TestRoomScreenAudioGoesWithTheShare(t *testing.T) {
	_, room, pub, viewer := joinTwo(t)
	mic := fakeTrack(room, pub, webrtc.RTPCodecTypeAudio, SourceAudio)
	sound := fakeTrack(room, pub, webrtc.RTPCodecTypeAudio, SourceScreenAudio)
	fakeTrack(room, pub, webrtc.RTPCodecTypeVideo, SourceScreen)
	want := func(info *TrackInfo) bool {
		room.mu.RLock()
		defer room.mu.RUnlock()
		return room.wantsLocked(viewer, info)
	}
	if !want(mic) || want(sound) {
		t.Fatal("before watching: the voice, not the screen's sound")
	}
	if err := room.Subscribe(viewer, pub, SourceScreen, false, true); err != nil {
		t.Fatal(err)
	}
	if !want(mic) || !want(sound) {
		t.Fatal("watching: the voice and the screen's sound")
	}
	if states := room.MediaStates(); states[pub] != (MediaState{Screen: true}) {
		t.Fatalf("the screen's sound is no video of its own: %+v", states[pub])
	}

	// Stopping the share unpublishes its sound too; a camera stop does not.
	room.RemoveUserSource(pub, SourceCamera)
	if room.sourceCount(pub, SourceScreenAudio) != 1 {
		t.Fatal("a camera stop must keep the screen's sound")
	}
	room.RemoveUserSource(pub, SourceScreen)
	if room.sourceCount(pub, SourceScreenAudio) != 0 || room.sourceCount(pub, SourceAudio) != 1 {
		t.Fatal("stopping the share must unpublish its sound and keep the voice")
	}
	if sound.stoppedAt.Load() == 0 {
		t.Fatal("the sound is published again once its packets resume")
	}
	// Only screen or camera can be stopped this way.
	room.RemoveUserSource(pub, SourceAudio)
	if room.sourceCount(pub, SourceAudio) != 1 {
		t.Fatal("the voice is never removed by a source stop")
	}
}

func TestRoomMediaStateNotifications(t *testing.T) {
	s, room, pub, _ := joinTwo(t)
	var mu sync.Mutex
	got := map[uuid.UUID]MediaState{}
	calls := 0
	s.SetMediaStateHandler(func(_, user uuid.UUID, st MediaState) {
		mu.Lock()
		got[user] = st
		calls++
		mu.Unlock()
	})

	fakeTrack(room, pub, webrtc.RTPCodecTypeAudio, SourceAudio)
	scr := fakeTrack(room, pub, webrtc.RTPCodecTypeVideo, SourceScreen)
	room.notifyMedia()
	room.notifyMedia() // no change, no second event
	mu.Lock()
	if !got[pub].Screen || got[pub].Camera || calls != 1 {
		t.Fatalf("want one screen-only announcement, got %+v calls=%d", got[pub], calls)
	}
	mu.Unlock()
	if st := room.MediaStates()[pub]; !st.Screen {
		t.Fatal("snapshot for late joiners should list the share")
	}

	room.mu.Lock()
	delete(room.trackLocals, trackKey(pub, string(scr.Source)))
	room.mu.Unlock()
	room.notifyMedia()
	mu.Lock()
	defer mu.Unlock()
	if got[pub].Screen || calls != 2 {
		t.Fatalf("want stop announcement, got %+v calls=%d", got[pub], calls)
	}
}
