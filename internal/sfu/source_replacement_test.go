package sfu

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

func TestSourceReplacementCannotResumeOldCapture(t *testing.T) {
	user := uuid.New()
	r := &Room{peers: map[uuid.UUID]*Peer{}, trackLocals: map[string]*TrackInfo{}, notifier: &mediaNotifier{}}
	track := func(source Source) *TrackInfo {
		return &TrackInfo{SenderID: user, Kind: webrtc.RTPCodecTypeAudio, Source: source}
	}
	mic, old, current := track(SourceAudio), track(SourceScreenAudio), track(SourceScreenAudio)
	r.addTrack("mic", mic)
	r.addTrack("old-screen-audio", old)
	// An announced stop removes subscriptions while keeping the RTP reader alive.
	r.mu.Lock()
	delete(r.trackLocals, "old-screen-audio")
	old.stoppedAt.Store(time.Now().Add(-time.Second).UnixNano())
	r.mu.Unlock()
	r.addTrack("current-screen-audio", current)
	r.addTrack("old-screen-audio", old)
	r.removeTrack("old-screen-audio", old)
	r.mu.RLock()
	if len(r.trackLocals) != 2 || r.trackLocals["mic"] != mic || r.trackLocals["current-screen-audio"] != current {
		t.Error("late old capture displaced its successor or the independent microphone")
	}
	r.mu.RUnlock()
	local := &failingLocal{}
	r.forward(&fakeRemote{n: 3}, local, "old-screen-audio", old)
	if local.writes != 0 {
		t.Fatal("superseded source still forwards packets")
	}
	// Peer removal releases the bounded latest-source references.
	r.mu.Lock()
	r.removePeerTracksLocked(user)
	if len(r.trackLocals) != 0 {
		t.Error("source references survive publisher removal")
	}
	r.mu.Unlock()
}

func TestReplacingActiveSourcePreservesOtherSources(t *testing.T) {
	user := uuid.New()
	r := &Room{peers: map[uuid.UUID]*Peer{}, trackLocals: map[string]*TrackInfo{}, notifier: &mediaNotifier{}}
	old := &TrackInfo{SenderID: user, Source: SourceScreenAudio}
	current := &TrackInfo{SenderID: user, Source: SourceScreenAudio}
	camera := &TrackInfo{SenderID: user, Source: SourceCamera}
	r.addTrack("old", old)
	r.addTrack("camera", camera)
	r.addTrack("current", current)
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.trackLocals) != 2 || r.trackLocals["old"] != nil || r.trackLocals["camera"] != camera || r.trackLocals["current"] != current {
		t.Fatal("replacement must leave only the latest source and independent camera")
	}
}
