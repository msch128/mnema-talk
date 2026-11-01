package sfu

import (
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

func cameraInfo(sender uuid.UUID) *TrackInfo {
	return &TrackInfo{SenderID: sender, Kind: webrtc.RTPCodecTypeVideo, Source: SourceCamera}
}

func TestCapCamerasKeepsTheFirstOnesAndEverythingElse(t *testing.T) {
	desired := map[string]*TrackInfo{}
	var ids []uuid.UUID
	for i := 0; i < MaxViewerCameras+3; i++ {
		id := uuid.New()
		ids = append(ids, id)
		desired[id.String()] = cameraInfo(id)
	}
	screen := &TrackInfo{SenderID: ids[0], Kind: webrtc.RTPCodecTypeVideo, Source: SourceScreen}
	mic := &TrackInfo{SenderID: ids[0], Kind: webrtc.RTPCodecTypeAudio, Source: SourceAudio}
	desired["screen"], desired["mic"] = screen, mic

	capped := capCameras(desired)

	slices.SortFunc(ids, compareIDs)
	if !slices.Equal(capped, ids[MaxViewerCameras:]) {
		t.Fatalf("capped %v, want the last three IDs %v", capped, ids[MaxViewerCameras:])
	}
	cameras := 0
	for _, info := range desired {
		if isCameraVideo(info) {
			cameras++
			if slices.Contains(capped, info.SenderID) {
				t.Fatal("a capped camera is still wanted")
			}
		}
	}
	if cameras != MaxViewerCameras || desired["screen"] != screen || desired["mic"] != mic {
		t.Fatalf("kept %d cameras, screen and audio must stay", cameras)
	}
}

func TestCapCamerasLeavesWhatFits(t *testing.T) {
	desired := map[string]*TrackInfo{}
	for i := 0; i < MaxViewerCameras; i++ {
		id := uuid.New()
		desired[id.String()] = cameraInfo(id)
	}
	if capped := capCameras(desired); capped != nil || len(desired) != MaxViewerCameras {
		t.Fatalf("nothing is capped at the limit, got %v", capped)
	}
}

func TestAnnounceCappedOnlyWhenTheSetChanges(t *testing.T) {
	room := &Room{ID: uuid.New(), notifier: &mediaNotifier{}}
	peer := &Peer{ID: uuid.New()}
	var told [][]uuid.UUID
	room.notifier.capped = func(roomID, viewer uuid.UUID, publishers []uuid.UUID) {
		if roomID != room.ID || viewer != peer.ID {
			t.Fatalf("announced for %v/%v", roomID, viewer)
		}
		told = append(told, publishers)
	}
	a, b := uuid.New(), uuid.New()

	room.announceCapped(peer, nil)
	room.announceCapped(peer, []uuid.UUID{a})
	room.announceCapped(peer, []uuid.UUID{a})
	room.announceCapped(peer, []uuid.UUID{a, b})
	room.announceCapped(peer, nil)
	room.announceCapped(peer, nil)

	if len(told) != 3 || !slices.Equal(told[0], []uuid.UUID{a}) || !slices.Equal(told[1], []uuid.UUID{a, b}) || len(told[2]) != 0 {
		t.Fatalf("announcements %v", told)
	}
}

func TestAnnounceCappedWithoutAListener(t *testing.T) {
	room := &Room{ID: uuid.New(), notifier: &mediaNotifier{}}
	peer := &Peer{ID: uuid.New()}
	room.announceCapped(peer, []uuid.UUID{uuid.New()})
	if len(peer.lastCapped) != 1 {
		t.Fatal("the set is remembered even when nobody listens")
	}
}

func TestSubscribeCamerasBatches(t *testing.T) {
	_, room, pub, viewer := joinTwo(t)
	cam := fakeTrack(room, pub, webrtc.RTPCodecTypeVideo, SourceCamera)
	want := func() bool {
		room.mu.RLock()
		defer room.mu.RUnlock()
		return room.wantsLocked(viewer, cam)
	}
	other := uuid.New()

	if err := room.SubscribeCameras(viewer, []uuid.UUID{pub, other}, false); err != nil {
		t.Fatal(err)
	}
	if want() {
		t.Fatal("the camera is off after the batch")
	}
	if err := room.SubscribeCameras(viewer, []uuid.UUID{pub, other}, true); err != nil {
		t.Fatal(err)
	}
	if !want() {
		t.Fatal("the camera is back after the batch")
	}
	if err := room.SubscribeCameras(uuid.New(), []uuid.UUID{pub}, false); !errors.Is(err, ErrNotInRoom) {
		t.Fatalf("a stranger gets %v", err)
	}
}

func TestSubscribeCamerasKeepsWhatFitsWhenTooMany(t *testing.T) {
	_, room, _, viewer := joinTwo(t)
	var ids []uuid.UUID
	for i := 0; i < maxHiddenCameras+2; i++ {
		ids = append(ids, uuid.New())
	}
	if err := room.SubscribeCameras(viewer, ids, false); !errors.Is(err, ErrTooManySubscriptions) {
		t.Fatalf("got %v", err)
	}
	room.mu.RLock()
	kept := len(room.subs[viewer].hiddenCameras)
	room.mu.RUnlock()
	if kept != maxHiddenCameras {
		t.Fatalf("kept %d choices, want %d", kept, maxHiddenCameras)
	}
}

func TestADesiredSetOfManyCamerasIsCapped(t *testing.T) {
	viewer := uuid.New()
	peer := &Peer{ID: viewer}
	room := &Room{ID: uuid.New(), peers: map[uuid.UUID]*Peer{viewer: peer}, trackLocals: map[string]*TrackInfo{}, subs: map[uuid.UUID]*Subscriptions{}}
	for i := 0; i < MaxViewerCameras+2; i++ {
		id := uuid.New()
		room.trackLocals[id.String()] = cameraInfo(id)
	}

	desired, ok := room.desiredTracks(peer)
	if !ok || len(desired) != MaxViewerCameras+2 {
		t.Fatalf("the viewer asked for %d cameras, got %d (ok=%v)", MaxViewerCameras+2, len(desired), ok)
	}
	if capped := capCameras(desired); len(capped) != 2 || len(desired) != MaxViewerCameras {
		t.Fatalf("capped %d, left %d", len(capped), len(desired))
	}
}

func TestCapCamerasCountsPublishersNotTracks(t *testing.T) {
	desired := map[string]*TrackInfo{}
	for i := 0; i < MaxViewerCameras; i++ {
		id := uuid.New()
		// Two tracks of one camera (a replaced capture) are one camera.
		desired[id.String()+"-a"] = cameraInfo(id)
		desired[id.String()+"-b"] = cameraInfo(id)
	}
	if capped := capCameras(desired); capped != nil || len(desired) != 2*MaxViewerCameras {
		t.Fatalf("%d publishers fit the limit, capped %v", MaxViewerCameras, capped)
	}
}
