package sfu

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

// A native viewer's authorization latch gates a layered share like any other
// track: it receives while the latch is open and nothing once it closes.
func TestNativeViewerOfLayeredShare(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()
	pub := newClient(t)
	pub.publishLayers(25)
	pub.join(s, room)
	viewer := newClient(t)
	var live atomic.Bool
	live.Store(true)
	viewer.joinNative(s, room, live.Load)
	r := s.Room(room)
	waitSources(t, r, pub.id, SourceScreen, 3)
	if err := r.Subscribe(viewer.id, pub.id, SourceScreen, false, true); err != nil {
		t.Fatal(err)
	}
	waitTrack(t, viewer)
	checkOneStream(t, waitLayer(t, viewer, 0))
	if err := r.SetScreenQuality(viewer.id, pub.id, 2); err != nil {
		t.Fatal(err)
	}
	checkOneStream(t, waitLayer(t, viewer, 2))

	live.Store(false)
	gather(viewer, 300*time.Millisecond)
	if got := gather(viewer, 300*time.Millisecond); len(got) != 0 {
		t.Fatalf("%d packets reached a viewer whose authorization ended", len(got))
	}
}
