package sfu

import (
	"slices"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

type viewersEvent struct {
	room, sharer uuid.UUID
	viewers      []uuid.UUID
}

// viewerLog records screen-viewer announcements. Signaling rounds started in
// the background announce too; dedupe makes each change appear exactly once
// whoever announces it.
type viewerLog struct {
	room   *Room
	mu     sync.Mutex
	events []viewersEvent
}

func recordViewers(s *SFU, room *Room) *viewerLog {
	l := &viewerLog{room: room}
	s.SetScreenViewersHandler(func(roomID, sharer uuid.UUID, viewers []uuid.UUID) {
		l.mu.Lock()
		l.events = append(l.events, viewersEvent{roomID, sharer, viewers})
		l.mu.Unlock()
	})
	return l
}

// flush announces pending changes and returns everything announced since the
// last flush. notifyMu orders it after any round that already computed.
func (l *viewerLog) flush() []viewersEvent {
	l.room.notifyMedia()
	l.mu.Lock()
	defer l.mu.Unlock()
	out := l.events
	l.events = nil
	return out
}

func sortedIDs(ids ...uuid.UUID) []uuid.UUID {
	out := append([]uuid.UUID{}, ids...)
	slices.SortFunc(out, compareIDs)
	return out
}

func wantOne(t *testing.T, got []viewersEvent, room, sharer uuid.UUID, viewers []uuid.UUID) {
	t.Helper()
	if len(got) != 1 {
		t.Fatalf("want one announcement, got %d: %+v", len(got), got)
	}
	ev := got[0]
	if ev.room != room || ev.sharer != sharer || ev.viewers == nil || !slices.Equal(ev.viewers, viewers) {
		t.Fatalf("got %+v, want room %s sharer %s viewers %v", ev, room, sharer, viewers)
	}
}

func wantNone(t *testing.T, got []viewersEvent) {
	t.Helper()
	if len(got) != 0 {
		t.Fatalf("want no announcement, got %+v", got)
	}
}

func TestScreenViewersAnnouncedOnlyOnChange(t *testing.T) {
	s, room, pub, b := joinTwo(t)
	c, idle := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{c, idle} {
		if _, err := room.JoinPeer(id, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	log := recordViewers(s, room)
	fakeTrack(room, pub, webrtc.RTPCodecTypeVideo, SourceScreen)
	wantNone(t, log.flush()) // a share nobody watches yet is no news

	if err := room.Subscribe(b, pub, SourceScreen, false, true); err != nil {
		t.Fatal(err)
	}
	wantOne(t, log.flush(), room.ID, pub, []uuid.UUID{b})

	if err := room.Subscribe(b, pub, SourceScreen, false, true); err != nil {
		t.Fatal(err)
	}
	wantNone(t, log.flush())

	// Cameras and subscriptions to someone who shares nothing are not audiences.
	if err := room.Subscribe(c, pub, SourceCamera, false, false); err != nil {
		t.Fatal(err)
	}
	if err := room.Subscribe(c, idle, SourceScreen, false, true); err != nil {
		t.Fatal(err)
	}
	wantNone(t, log.flush())

	if err := room.Subscribe(c, pub, SourceScreen, false, true); err != nil {
		t.Fatal(err)
	}
	wantOne(t, log.flush(), room.ID, pub, sortedIDs(b, c))

	// The sharer never watches their own share.
	if err := room.Subscribe(pub, pub, SourceScreen, false, true); err != nil {
		t.Fatal(err)
	}
	wantNone(t, log.flush())

	if err := room.Subscribe(b, pub, SourceScreen, false, false); err != nil {
		t.Fatal(err)
	}
	wantOne(t, log.flush(), room.ID, pub, []uuid.UUID{c})

	if err := room.Subscribe(c, pub, SourceScreen, false, false); err != nil {
		t.Fatal(err)
	}
	wantOne(t, log.flush(), room.ID, pub, []uuid.UUID{})
	wantNone(t, log.flush())
}

// A viewer's new connection (reconnect, second tab) keeps them counted once;
// leaving the room takes them off the list.
func TestScreenViewersFollowViewerConnections(t *testing.T) {
	s, room, pub, viewer := joinTwo(t)
	log := recordViewers(s, room)
	fakeTrack(room, pub, webrtc.RTPCodecTypeVideo, SourceScreen)
	if err := room.Subscribe(viewer, pub, SourceScreen, false, true); err != nil {
		t.Fatal(err)
	}
	wantOne(t, log.flush(), room.ID, pub, []uuid.UUID{viewer})

	if _, err := room.JoinPeer(viewer, nil, nil); err != nil {
		t.Fatal(err)
	}
	wantNone(t, log.flush())
	if got := room.ScreenViewers()[pub]; !slices.Equal(got, []uuid.UUID{viewer}) {
		t.Fatalf("viewer counted as %v after a second connection", got)
	}

	s.RemovePeer(room.ID, room.GetPeer(viewer))
	wantOne(t, log.flush(), room.ID, pub, []uuid.UUID{})
}

// Stopping a watched share announces an empty audience once; so does the
// sharer leaving the room.
func TestScreenViewersClearedWhenShareEnds(t *testing.T) {
	s, room, pub, viewer := joinTwo(t)
	log := recordViewers(s, room)
	watch := func() {
		t.Helper()
		fakeTrack(room, pub, webrtc.RTPCodecTypeVideo, SourceScreen)
		if err := room.Subscribe(viewer, pub, SourceScreen, false, true); err != nil {
			t.Fatal(err)
		}
		wantOne(t, log.flush(), room.ID, pub, []uuid.UUID{viewer})
	}

	watch()
	room.RemoveUserSource(pub, SourceScreen)
	wantOne(t, log.flush(), room.ID, pub, []uuid.UUID{})
	wantNone(t, log.flush())

	// A new share starts unwatched: the opt-in belonged to the old one.
	fakeTrack(room, pub, webrtc.RTPCodecTypeVideo, SourceScreen)
	wantNone(t, log.flush())
	room.RemoveUserSource(pub, SourceScreen)
	wantNone(t, log.flush()) // nobody watched it, nothing to clear

	watch()
	s.RemovePeer(room.ID, room.GetPeer(pub))
	wantOne(t, log.flush(), room.ID, pub, []uuid.UUID{})
}

// The snapshot for joiners lists every live share, watched or not.
func TestScreenViewersSnapshot(t *testing.T) {
	_, room, pub, viewer := joinTwo(t)
	other := uuid.New()
	if _, err := room.JoinPeer(other, nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(room.ScreenViewers()) != 0 {
		t.Fatal("no share, no entry")
	}
	fakeTrack(room, pub, webrtc.RTPCodecTypeVideo, SourceScreen)
	fakeTrack(room, other, webrtc.RTPCodecTypeVideo, SourceCamera)
	fakeTrack(room, viewer, webrtc.RTPCodecTypeVideo, SourceScreen)
	if err := room.Subscribe(viewer, pub, SourceScreen, false, true); err != nil {
		t.Fatal(err)
	}
	if err := room.Subscribe(other, pub, SourceScreen, false, true); err != nil {
		t.Fatal(err)
	}
	snap := room.ScreenViewers()
	if len(snap) != 2 {
		t.Fatalf("want the two screen shares, got %v", snap)
	}
	if !slices.Equal(snap[pub], sortedIDs(viewer, other)) {
		t.Fatalf("pub's viewers %v", snap[pub])
	}
	if got, ok := snap[viewer]; !ok || got == nil || len(got) != 0 {
		t.Fatalf("an unwatched share maps to an empty list, got %v (present %v)", got, ok)
	}
}

// Rooms announce independently and only their own audiences.
func TestScreenViewersStayInTheirRoom(t *testing.T) {
	s, room, pub, viewer := joinTwo(t)
	otherRoom, _, err := s.Join(uuid.New(), uuid.New(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	log := recordViewers(s, room)
	fakeTrack(room, pub, webrtc.RTPCodecTypeVideo, SourceScreen)
	if err := room.Subscribe(viewer, pub, SourceScreen, false, true); err != nil {
		t.Fatal(err)
	}
	otherRoom.notifyMedia()
	wantOne(t, log.flush(), room.ID, pub, []uuid.UUID{viewer})
	if len(otherRoom.ScreenViewers()) != 0 {
		t.Fatal("audience leaked into another room")
	}
}

// Announcements are computed under the room lock and delivered outside it,
// concurrently with subscription changes and signaling rounds.
func TestScreenViewersConcurrentChanges(t *testing.T) {
	s, room, pub, _ := joinTwo(t)
	log := recordViewers(s, room)
	fakeTrack(room, pub, webrtc.RTPCodecTypeVideo, SourceScreen)
	viewers := make([]uuid.UUID, 4)
	for i := range viewers {
		viewers[i] = uuid.New()
		if _, err := room.JoinPeer(viewers[i], nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	for _, v := range viewers {
		wg.Add(1)
		go func(v uuid.UUID) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				_ = room.Subscribe(v, pub, SourceScreen, false, i%2 == 0)
				_ = room.ScreenViewers()
				room.notifyMedia()
			}
		}(v)
	}
	wg.Wait()
	// Every viewer ended unsubscribed, so the last word is an empty audience.
	events := log.flush()
	if len(events) > 0 && len(events[len(events)-1].viewers) != 0 {
		t.Fatalf("last announcement %+v, want empty", events[len(events)-1])
	}
	if len(room.ScreenViewers()[pub]) != 0 {
		t.Fatal("viewers left after everyone unsubscribed")
	}
}
