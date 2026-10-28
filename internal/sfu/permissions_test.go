package sfu

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

func TestSourceSet(t *testing.T) {
	var set SourceSet
	for _, s := range []Source{SourceAudio, SourceScreen, SourceCamera, SourceScreenAudio} {
		if set.Has(s) {
			t.Fatalf("empty set has %s", s)
		}
		set = set.With(s)
		if !set.Has(s) {
			t.Fatalf("set lost %s", s)
		}
	}
	if set.Has("bogus") || set.With("bogus") != set {
		t.Fatal("an unknown source must change nothing")
	}
	p := &Peer{}
	if p.sourceDenied(SourceAudio) {
		t.Fatal("nothing is denied by default")
	}
	p.SetDeniedSources(SourceSet(0).With(SourceCamera))
	if !p.sourceDenied(SourceCamera) || p.sourceDenied(SourceAudio) {
		t.Fatal("denied sources")
	}
}

// A user without the right to send something never gets it published, and
// what they may send is unaffected.
func TestDeniedSourcesAreNeverPublished(t *testing.T) {
	logs := captureSFULog(t)
	s := newTestSFU(t)
	room := uuid.New()

	pub := newClient(t)
	pub.publishAudio("mic")
	pub.publishVideo("screen")
	pub.joinWith(func(offer func(webrtc.SessionDescription)) (*Peer, error) {
		_, p, err := s.Join(room, pub.id, offer, nil)
		if p != nil {
			// Before the first offer is answered, like the hub does on joining.
			p.SetDeniedSources(SourceSet(0).With(SourceAudio))
		}
		return p, err
	})
	r := s.Room(room)
	waitSources(t, r, pub.id, SourceScreen, 1)
	// The microphone is handled on its own goroutine. Wait until that handler
	// has logged the track, and fail immediately if it was published.
	waitSFUCondition(t, "denied microphone to be handled", func() bool {
		if n := r.sourceCount(pub.id, SourceAudio); n != 0 {
			t.Fatalf("a denied microphone was published (%d tracks)", n)
		}
		return strings.Contains(logs.String(), "source=audio")
	})
}

// Taking a permission away unpublishes the live source and drops its packets;
// giving it back publishes it again.
func TestRevokedSourceIsUnpublishedAndComesBack(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()

	pub := newClient(t)
	pub.publishAudio("mic")
	pub.join(s, room)
	r := s.Room(room)
	waitSources(t, r, pub.id, SourceAudio, 1)

	pub.peer.SetDeniedSources(SourceSet(0).With(SourceAudio))
	r.RevokeUserSource(pub.id, SourceAudio)
	if n := r.sourceCount(pub.id, SourceAudio); n != 0 {
		t.Fatalf("the revoked microphone is still published (%d)", n)
	}
	// Packets keep arriving but must not bring it back.
	time.Sleep(resumeHoldoff + 700*time.Millisecond)
	if n := r.sourceCount(pub.id, SourceAudio); n != 0 {
		t.Fatalf("the revoked microphone came back by itself (%d)", n)
	}

	pub.peer.SetDeniedSources(0)
	waitSources(t, r, pub.id, SourceAudio, 1)
}

// A track that arrives while its source is denied is not lost: pion reports a
// track once, so the SFU keeps reading it and publishes it as soon as the
// permission is back.
func TestTrackArrivingWhileDeniedIsPublishedOnceAllowed(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()

	pub := newClient(t)
	pub.publishAudio("mic")
	pub.joinWith(func(offer func(webrtc.SessionDescription)) (*Peer, error) {
		_, p, err := s.Join(room, pub.id, offer, nil)
		if p != nil {
			p.SetDeniedSources(SourceSet(0).With(SourceAudio))
		}
		return p, err
	})
	r := s.Room(room)
	// Packets arrive (the track is handled) but nothing is published.
	time.Sleep(time.Second)
	if n := r.sourceCount(pub.id, SourceAudio); n != 0 {
		t.Fatalf("a denied microphone was published (%d tracks)", n)
	}

	pub.peer.SetDeniedSources(0)
	waitSources(t, r, pub.id, SourceAudio, 1)
}

func TestForwardDropsPacketsOfDeniedSources(t *testing.T) {
	user := uuid.New()
	peer := &Peer{ID: user}
	r := &Room{
		peers:       map[uuid.UUID]*Peer{},
		trackLocals: map[string]*TrackInfo{},
		subs:        map[uuid.UUID]*Subscriptions{},
		lastMedia:   map[uuid.UUID]MediaState{},
		notifier:    &mediaNotifier{},
	}
	key := trackKey(user, "audio")
	info := &TrackInfo{SenderID: user, Kind: webrtc.RTPCodecTypeAudio, Source: SourceAudio, publisher: peer}
	r.trackLocals[key] = info

	peer.SetDeniedSources(SourceSet(0).With(SourceAudio))
	local := &failingLocal{}
	r.forward(&fakeRemote{n: 5}, local, key, info)
	if local.writes != 0 {
		t.Fatalf("%d packets of a denied source were forwarded", local.writes)
	}

	// Another source of the same user is not affected.
	other := &TrackInfo{SenderID: user, Kind: webrtc.RTPCodecTypeVideo, Source: SourceCamera, publisher: peer}
	r.trackLocals[trackKey(user, "cam")] = other
	local = &failingLocal{}
	r.forward(&fakeRemote{n: 3}, local, trackKey(user, "cam"), other)
	if local.writes != 3 {
		t.Fatalf("camera packets forwarded: %d, want 3", local.writes)
	}
}

func TestRevokeUserSourceCoversTheVoiceButTheClientsStopDoesNot(t *testing.T) {
	r := &Room{
		peers:       map[uuid.UUID]*Peer{},
		trackLocals: map[string]*TrackInfo{},
		subs:        map[uuid.UUID]*Subscriptions{},
		lastMedia:   map[uuid.UUID]MediaState{},
		notifier:    &mediaNotifier{},
	}
	user := uuid.New()
	key := trackKey(user, "mic")
	r.trackLocals[key] = &TrackInfo{SenderID: user, Kind: webrtc.RTPCodecTypeAudio, Source: SourceAudio}
	r.RemoveUserSource(user, SourceAudio)
	if r.sourceCount(user, SourceAudio) != 1 {
		t.Fatal("a client cannot unpublish its voice with a stop message")
	}
	r.RevokeUserSource(user, SourceScreenAudio) // not a revocable source on its own
	if r.sourceCount(user, SourceAudio) != 1 {
		t.Fatal("unrelated source revoked")
	}
	r.RevokeUserSource(user, SourceAudio)
	if r.sourceCount(user, SourceAudio) != 0 {
		t.Fatal("revoking the voice must unpublish it")
	}
}
