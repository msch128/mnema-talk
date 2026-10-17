package sfu

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Layers of several shares that change together are announced in a fixed
// order (by sharer), like the audiences.
func TestLayerAnnouncementsAreOrdered(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()
	var mu sync.Mutex
	var order []uuid.UUID
	s.SetScreenLayersHandler(func(_, sharer uuid.UUID, _ []Layer) {
		mu.Lock()
		order = append(order, sharer)
		mu.Unlock()
	})
	var pubs []*client
	for range 3 {
		pub := newClient(t)
		pub.publishLayers(25)
		pub.join(s, room)
		pubs = append(pubs, pub)
	}
	r := s.Room(room)
	for _, pub := range pubs {
		waitSources(t, r, pub.id, SourceScreen, 3)
	}
	// Let everything announced so far settle, then change all at once.
	time.Sleep(300 * time.Millisecond)
	mu.Lock()
	order = nil
	mu.Unlock()
	r.mu.Lock()
	r.layerMeta = map[uuid.UUID][]Layer{}
	for _, pub := range pubs {
		r.layerMeta[pub.id] = []Layer{{1280, 720, 30}}
	}
	r.mu.Unlock()
	r.notifyMedia()
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 3 {
		t.Fatalf("announced %d shares, want 3", len(order))
	}
	for i := 1; i < len(order); i++ {
		if compareIDs(order[i-1], order[i]) >= 0 {
			t.Fatalf("announcements out of order: %v", order)
		}
	}
}

// A camera is forwarded as it always was: one track for everyone, asked for
// a keyframe when a viewer's browser starts the video.
func TestDispatchKeyframeForCamera(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()
	pub := newClient(t)
	pub.publishVideo("screen")
	pub.publishVideo("camera")
	pub.join(s, room)
	viewer := newClient(t)
	viewer.join(s, room)
	r := s.Room(room)
	waitSources(t, r, pub.id, SourceCamera, 1)
	waitTrack(t, viewer)
	time.Sleep(keyframeMinInterval + 50*time.Millisecond)
	drainPLIs(pub)
	r.DispatchKeyframe(viewer.id)
	waitPLI(t, pub)
}
