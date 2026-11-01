package sfu

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

// publishCameraLayers adds the six video tracks a client with camera layers
// sends on the SFU's video lines: the screen and its two lower lines (silent),
// the camera and its two lower lines. start begins sending; a client that
// declares its layers does so before that.
func (c *client) publishCameraLayers(keyEvery int) (pubs []*layerPub, start func()) {
	c.t.Helper()
	stop := make(chan struct{})
	c.t.Cleanup(func() { close(stop) })
	var begin []func()
	for i, id := range []string{"screen", "camera", "low1", "low2", "cam1", "cam2"} {
		track, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8}, id, "stream-"+c.id.String())
		if err != nil {
			c.t.Fatal(err)
		}
		sender, err := c.pc.AddTrack(track)
		if err != nil {
			c.t.Fatal(err)
		}
		if id != "camera" && id != "cam1" && id != "cam2" {
			continue
		}
		layer := map[int]int{1: 0, 4: 1, 5: 2}[i]
		p := &layerPub{layer: layer, track: track, sender: sender, plis: make(chan struct{}, 64)}
		pubs = append(pubs, p)
		go startRTCPReader(sender, p.plis)
		begin = append(begin, func() { go startLayerPublisher(track, layer, keyEvery, stop) })
	}
	return pubs, func() {
		for _, f := range begin {
			f()
		}
	}
}

func TestFitLayerFor(t *testing.T) {
	ladder := []Layer{{1280, 720, 30}, {640, 360, 30}, {320, 180, 15}}
	for _, tc := range []struct {
		name   string
		layers []Layer
		pixels int
		want   int
	}{
		{"unknown size gets the best", ladder, 0, 0},
		{"a thumbnail gets the smallest", ladder, 150, 2},
		{"just inside the tolerance of the smallest", ladder, 200, 2},
		{"a tile gets the middle one", ladder, 300, 1},
		{"a big picture gets the best", ladder, 700, 0},
		{"larger than any layer gets the best", ladder, 2000, 0},
		{"one layer has nothing to choose", ladder[:1], 100, 0},
		{"no layers", nil, 100, 0},
	} {
		if got := fitLayerFor(tc.layers, tc.pixels); got != tc.want {
			t.Errorf("%s: fitLayerFor(%d) = %d, want %d", tc.name, tc.pixels, got, tc.want)
		}
	}
}

func TestSetCameraLayersAndFitRefusals(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()
	pub, viewer := newClient(t), newClient(t)
	pub.join(s, room)
	viewer.join(s, room)
	r := s.Room(room)
	ladder := []Layer{{1280, 720, 30}, {640, 360, 30}}

	if err := r.SetCameraLayers(pub.id, nil); err == nil {
		t.Error("empty layers accepted")
	}
	if err := r.SetCameraLayers(uuid.New(), ladder); err == nil {
		t.Error("layers of someone who is not in the room accepted")
	}
	if err := r.SetCameraLayers(pub.id, ladder); err != nil {
		t.Fatal(err)
	}
	// The same declaration again changes nothing.
	if err := r.SetCameraLayers(pub.id, ladder); err != nil {
		t.Fatal(err)
	}
	r.mu.RLock()
	layered := r.cameraLayeredLocked(pub.id)
	screenMeta := len(r.layerMeta[pub.id])
	r.mu.RUnlock()
	if !layered || screenMeta != 0 {
		t.Fatalf("layered %v, screen layers declared %d", layered, screenMeta)
	}

	for name, args := range map[string][3]any{
		"negative":  {viewer.id, pub.id, -1},
		"huge":      {viewer.id, pub.id, maxFitPixels + 1},
		"stranger":  {uuid.New(), pub.id, 100},
		"own video": {viewer.id, viewer.id, 100},
	} {
		if err := r.SetCameraFit(args[0].(uuid.UUID), args[1].(uuid.UUID), args[2].(int)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	// A size reported before the camera exists is kept for it, and 0 forgets it.
	if err := r.SetCameraFit(viewer.id, pub.id, 150); err != nil {
		t.Fatal(err)
	}
	r.mu.RLock()
	fit := r.cameraFitLocked(viewer.id, pub.id)
	r.mu.RUnlock()
	if fit != 1 {
		t.Fatalf("a 150 px picture of a 720p/360p camera fits layer %d, want 1", fit)
	}
	if err := r.SetCameraFit(viewer.id, pub.id, 0); err != nil {
		t.Fatal(err)
	}
	r.mu.RLock()
	fit = r.cameraFitLocked(viewer.id, pub.id)
	r.mu.RUnlock()
	if fit != 0 {
		t.Fatalf("an unknown size fits layer %d, want 0", fit)
	}
}

func TestCameraLayersFollowEachViewersPicture(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()
	var mu sync.Mutex
	var wanted [][]int
	s.SetCameraLayersWantedHandler(func(_, _ uuid.UUID, l []int) {
		mu.Lock()
		wanted = append(wanted, l)
		mu.Unlock()
	})
	s.SetLayersWantedHandler(func(_, _ uuid.UUID, l []int) { t.Errorf("a camera announced screen layers %v", l) })

	pub := newClient(t)
	pubs, start := pub.publishCameraLayers(25)
	pub.join(s, room)
	r := s.Room(room)
	if err := r.SetCameraLayers(pub.id, []Layer{{1280, 720, 30}, {640, 360, 30}, {320, 180, 15}}); err != nil {
		t.Fatal(err)
	}
	start()
	waitSources(t, r, pub.id, SourceCamera, 3)
	if r.sourceCount(pub.id, SourceScreen) != 0 {
		t.Fatal("a camera layer was taken for the screen")
	}

	tile, stage := newClient(t), newClient(t)
	tile.join(s, room)
	stage.join(s, room)
	if err := r.SetCameraFit(tile.id, pub.id, 150); err != nil {
		t.Fatal(err)
	}
	if err := r.SetCameraFit(stage.id, pub.id, 900); err != nil {
		t.Fatal(err)
	}
	// Cameras are on by default: both viewers get one track named like a plain camera.
	for _, v := range []*client{tile, stage} {
		tr := waitTrack(t, v)
		if tr.StreamID() != CameraStreamPrefix+pub.id.String() {
			t.Fatalf("camera stream ID %q", tr.StreamID())
		}
	}
	checkOneStream(t, waitLayer(t, tile, 2))
	checkOneStream(t, waitLayer(t, stage, 0))
	for layer, v := range map[int]*client{2: tile, 0: stage} {
		for _, p := range gather(v, 300*time.Millisecond) {
			if layerOf(p) != layer {
				t.Fatalf("viewer on layer %d received a packet of layer %d", layer, layerOf(p))
			}
		}
	}

	// The tile is put on the stage: its picture grows, so it climbs.
	drainPLIs(pub)
	for _, l := range pubs {
		select {
		case <-l.plis:
		default:
		}
	}
	if err := r.SetCameraFit(tile.id, pub.id, 900); err != nil {
		t.Fatal(err)
	}
	checkOneStream(t, waitLayer(t, tile, 0))
	// The move asked the publisher for a keyframe of the layer it moved to.
	select {
	case <-pubs[0].plis:
	case <-time.After(3 * time.Second):
		t.Fatal("moving to the best layer did not ask the publisher for its keyframe")
	}

	// The sender was told the layers somebody needs: the thumbnail among them, and in the end the best layer.
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		if len(wanted) == 0 {
			return false
		}
		for _, l := range wanted[len(wanted)-1] {
			if l == 0 {
				return true
			}
		}
		return false
	})
	mu.Lock()
	defer mu.Unlock()
	sawThumbnail := false
	for _, mask := range wanted {
		for _, l := range mask {
			sawThumbnail = sawThumbnail || l == 2
		}
	}
	if !sawThumbnail {
		t.Fatalf("the sender was never told the thumbnail was wanted: %v", wanted)
	}
}

func TestCameraWithoutDeclaredLayersStaysOnePlainTrack(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()
	pub := newClient(t)
	// The client sends its lower camera layers too, but never declared any.
	_, start := pub.publishCameraLayers(25)
	pub.join(s, room)
	r := s.Room(room)
	start()
	waitSources(t, r, pub.id, SourceCamera, 1)
	time.Sleep(300 * time.Millisecond)
	if n := r.sourceCount(pub.id, SourceCamera); n != 1 {
		t.Fatalf("camera tracks %d, want the one plain track", n)
	}
	r.mu.RLock()
	groups := len(r.cameraGroups)
	r.mu.RUnlock()
	if groups != 0 {
		t.Fatal("a camera nobody declared layers for made a group")
	}

	viewer := newClient(t)
	viewer.join(s, room)
	// A viewer's size report is harmless: there is one layer.
	if err := r.SetCameraFit(viewer.id, pub.id, 100); err != nil {
		t.Fatal(err)
	}
	tr := waitTrack(t, viewer)
	if tr.StreamID() != CameraStreamPrefix+pub.id.String() {
		t.Fatalf("camera stream ID %q", tr.StreamID())
	}
	if len(gather(viewer, 500*time.Millisecond)) == 0 {
		t.Fatal("the plain camera was not forwarded")
	}
}

func TestStoppedCameraForgetsItsLayers(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()
	pub := newClient(t)
	_, start := pub.publishCameraLayers(25)
	pub.join(s, room)
	r := s.Room(room)
	if err := r.SetCameraLayers(pub.id, []Layer{{1280, 720, 30}, {640, 360, 30}, {320, 180, 15}}); err != nil {
		t.Fatal(err)
	}
	start()
	waitSources(t, r, pub.id, SourceCamera, 3)
	r.mu.RLock()
	g := r.cameraGroups[pub.id]
	r.mu.RUnlock()
	if g == nil || g.source != SourceCamera {
		t.Fatalf("camera group %v", g)
	}

	r.RemoveUserSource(pub.id, SourceCamera)
	r.mu.RLock()
	declared := len(r.cameraLayerMeta[pub.id])
	r.mu.RUnlock()
	if declared != 0 || g.declared.Load() != 0 || g.live.Load() != 0 {
		t.Fatalf("stopped camera: declared %d, group declared %d live %b", declared, g.declared.Load(), g.live.Load())
	}

	// Leaving ends the group.
	pub2 := newClient(t)
	pub2.join(s, room)
	r.removePeer(pub.peer)
	r.mu.RLock()
	left := r.cameraGroups[pub.id]
	r.mu.RUnlock()
	if left != nil || !g.superseded.Load() {
		t.Fatal("the camera group outlived its publisher")
	}
}

func TestTestHooksReportCameraState(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()
	pub, viewer := newClient(t), newClient(t)
	pub.join(s, room)
	viewer.join(s, room)
	r := s.Room(room)

	if got := r.CameraLayers(pub.id); len(got) != 0 {
		t.Fatalf("camera layers before a declaration: %v", got)
	}
	ladder := []Layer{{1280, 720, 30}, {640, 360, 30}, {320, 180, 15}}
	if err := r.SetCameraLayers(pub.id, ladder); err != nil {
		t.Fatal(err)
	}
	got := r.CameraLayers(pub.id)
	if len(got) != 3 || got[2] != ladder[2] {
		t.Fatalf("camera layers %v", got)
	}
	// The copy is the caller's: changing it changes nothing.
	got[0].Height = 1
	if r.CameraLayers(pub.id)[0].Height != 720 {
		t.Fatal("CameraLayers handed out the stored slice")
	}
	if err := r.SetCameraFit(viewer.id, pub.id, 150); err != nil {
		t.Fatal(err)
	}
	if fit := r.CameraFit(viewer.id, pub.id); fit != 2 {
		t.Fatalf("fit %d", fit)
	}
	if err := r.PublishTestVideo(pub.id, SourceAudio); err == nil {
		t.Error("an audio source was published as video")
	}
	if err := r.PublishTestVideo(uuid.New(), SourceCamera); err == nil {
		t.Error("a video of someone outside the room was published")
	}
}

// A camera that began as one plain track cannot become layers in the middle of
// its video: a lower layer joining would replace the best one for good. The
// declaration is ignored, the lower layers are drained and viewers keep the
// plain camera.
func TestLayersDeclaredInTheMiddleOfAPlainCameraAreIgnored(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()
	pub := newClient(t)
	_, start := pub.publishCameraLayers(25)
	pub.join(s, room)
	r := s.Room(room)
	start()
	waitSources(t, r, pub.id, SourceCamera, 1)

	if err := r.SetCameraLayers(pub.id, []Layer{{1280, 720, 30}, {640, 360, 30}, {320, 180, 15}}); err != nil {
		t.Fatal(err)
	}
	if got := r.CameraLayers(pub.id); len(got) != 0 {
		t.Fatalf("a late declaration was kept: %v", got)
	}
	time.Sleep(300 * time.Millisecond)
	if n := r.sourceCount(pub.id, SourceCamera); n != 1 {
		t.Fatalf("camera tracks %d, want the one plain track", n)
	}
	r.mu.RLock()
	groups := len(r.cameraGroups)
	r.mu.RUnlock()
	if groups != 0 {
		t.Fatal("a plain camera became a group")
	}

	viewer := newClient(t)
	viewer.join(s, room)
	waitTrack(t, viewer)
	if len(gather(viewer, 500*time.Millisecond)) == 0 {
		t.Fatal("viewers lost the plain camera")
	}
}

// Declaring a different ladder while viewers are on a camera recomputes the
// layers their pictures fit.
func TestChangedLadderRecomputesWhatViewersPicturesFit(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()
	pub := newClient(t)
	_, start := pub.publishCameraLayers(25)
	pub.join(s, room)
	r := s.Room(room)
	if err := r.SetCameraLayers(pub.id, []Layer{{1280, 720, 30}, {640, 360, 30}, {320, 180, 15}}); err != nil {
		t.Fatal(err)
	}
	start()
	waitSources(t, r, pub.id, SourceCamera, 3)

	viewer := newClient(t)
	viewer.join(s, room)
	if err := r.SetCameraFit(viewer.id, pub.id, 150); err != nil {
		t.Fatal(err)
	}
	waitTrack(t, viewer)
	r.mu.RLock()
	g := r.cameraGroups[pub.id]
	r.mu.RUnlock()
	fitOf := func() int {
		vt := g.viewerFor(viewer.id)
		if vt == nil {
			return -1
		}
		vt.mu.Lock()
		defer vt.mu.Unlock()
		return vt.fit
	}
	waitFor(t, func() bool { return fitOf() == 2 })

	// The camera is sent as two layers now: the same picture fits the second.
	if err := r.SetCameraLayers(pub.id, []Layer{{1280, 720, 30}, {640, 360, 30}}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return fitOf() == 1 })
}
