package sfu

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// layerPub is one layer of a test publisher's screen share: a VP8 stream whose
// packets are one frame each, keyframes every keyEvery frames, and whose last
// payload byte names the layer so a receiver can tell where a packet came from.
type layerPub struct {
	layer  int
	track  *webrtc.TrackLocalStaticRTP
	sender *webrtc.RTPSender
	plis   chan struct{}
}

func (p *layerPub) ssrc() uint32 { return uint32(p.sender.GetParameters().Encodings[0].SSRC) }

func forwardPLIs(pkts []rtcp.Packet, plis chan<- struct{}) {
	for _, pkt := range pkts {
		if _, ok := pkt.(*rtcp.PictureLossIndication); ok {
			select {
			case plis <- struct{}{}:
			default:
			}
		}
	}
}

func startRTCPReader(sender *webrtc.RTPSender, plis chan<- struct{}) {
	for {
		pkts, _, err := sender.ReadRTCP()
		if err != nil {
			return
		}
		forwardPLIs(pkts, plis)
	}
}

func startLayerPublisher(track *webrtc.TrackLocalStaticRTP, layer, keyEvery int, stop <-chan struct{}) {
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	seq := uint16(1000 + 20000*layer)
	ts := uint32(1_000_000 * (layer + 1))
	pic := uint16(1000 * (layer + 1))
	tl0 := uint8(50 * layer)
	frame := 0
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
			key := frame%keyEvery == 0
			_ = track.WriteRTP(&rtp.Packet{
				Header:  rtp.Header{Version: 2, PayloadType: 96, SequenceNumber: seq, Timestamp: ts, Marker: true},
				Payload: vp8Payload(true, key, pic&0x7fff, true, tl0, byte(layer)),
			})
			seq++
			ts += 3000
			pic++
			tl0++
			frame++
		}
	}
}

// publishLayers adds the four video tracks a layered publisher sends on the
// SFU's video lines: the screen (layer 0), a camera that sends nothing, and
// the two lower layers. It must run before join.
func (c *client) publishLayers(keyEvery int) []*layerPub {
	c.t.Helper()
	add := func(id string) (*webrtc.TrackLocalStaticRTP, *webrtc.RTPSender) {
		track, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8}, id, "stream-"+c.id.String())
		if err != nil {
			c.t.Fatal(err)
		}
		sender, err := c.pc.AddTrack(track)
		if err != nil {
			c.t.Fatal(err)
		}
		return track, sender
	}
	var pubs []*layerPub
	for i, id := range []string{"screen", "camera", "low1", "low2"} {
		track, sender := add(id)
		if id == "camera" {
			continue
		}
		layer := i
		if layer > 1 {
			layer--
		}
		p := &layerPub{layer: layer, track: track, sender: sender, plis: make(chan struct{}, 64)}
		pubs = append(pubs, p)
		go startRTCPReader(sender, p.plis)
		stop := make(chan struct{})
		c.t.Cleanup(func() { close(stop) })
		go startLayerPublisher(track, layer, keyEvery, stop)
	}
	return pubs
}

// gather collects what a viewer receives for d.
func gather(c *client, d time.Duration) []*rtp.Packet {
	var out []*rtp.Packet
	deadline := time.After(d)
	for {
		select {
		case p := <-c.received:
			out = append(out, p)
		case <-deadline:
			return out
		}
	}
}

func layerOf(p *rtp.Packet) int { return int(p.Payload[len(p.Payload)-1]) }

// checkOneStream asserts that pkts are one continuous stream, whatever layers
// they came from: consecutive sequence numbers, timestamps that never go
// back, picture IDs that count up by one per frame.
func checkOneStream(t *testing.T, pkts []*rtp.Packet) {
	t.Helper()
	for i := 1; i < len(pkts); i++ {
		prev, cur := pkts[i-1], pkts[i]
		if cur.SequenceNumber != prev.SequenceNumber+1 {
			t.Fatalf("packet %d: sequence %d after %d (layers %d, %d)", i, cur.SequenceNumber, prev.SequenceNumber, layerOf(prev), layerOf(cur))
		}
		if int32(cur.Timestamp-prev.Timestamp) < 0 {
			t.Fatalf("packet %d: timestamp went back from %d to %d", i, prev.Timestamp, cur.Timestamp)
		}
		dp, ok := parseVP8(prev.Payload)
		if !ok {
			t.Fatalf("packet %d: parse prev vp8 failed", i-1)
		}
		dc, ok := parseVP8(cur.Payload)
		if !ok {
			t.Fatalf("packet %d: parse cur vp8 failed", i)
		}
		if dc.pic != (dp.pic+1)&0x7fff {
			t.Fatalf("packet %d: picture ID %d after %d (layers %d, %d)", i, dc.pic, dp.pic, layerOf(prev), layerOf(cur))
		}
	}
}

func waitLayer(t *testing.T, c *client, layer int) []*rtp.Packet {
	t.Helper()
	var all []*rtp.Packet
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		got := gather(c, 100*time.Millisecond)
		all = append(all, got...)
		if n := len(got); n > 0 && layerOf(got[n-1]) == layer {
			// A little more, to see the new layer carry on.
			all = append(all, gather(c, 200*time.Millisecond)...)
			return all
		}
	}
	t.Fatalf("the viewer never received layer %d", layer)
	return nil
}

func assertInitialQuality(t *testing.T, r *Room, viewerID, pubID uuid.UUID) {
	t.Helper()
	st, ok := r.ScreenQuality(viewerID, pubID)
	if !ok {
		t.Fatal("quality lookup failed for initial state")
	}
	if st.Layer != 0 {
		t.Fatalf("expected initial layer 0, got %d", st.Layer)
	}
	if !st.Auto {
		t.Fatalf("expected initial quality to be auto, got manual")
	}
}

func assertLayerTransition(t *testing.T, r *Room, viewer, pub *client, layers []*layerPub, layer int) {
	t.Helper()
	drainPLIs(pub)
	for _, l := range layers {
		select {
		case <-l.plis:
		default:
		}
	}
	if err := r.SetScreenQuality(viewer.id, pub.id, layer); err != nil {
		t.Fatal(err)
	}
	pkts := waitLayer(t, viewer, layer)
	checkOneStream(t, pkts)
	select {
	case <-layers[layer].plis:
	case <-time.After(3 * time.Second):
		t.Fatalf("switching to layer %d did not ask the publisher for its keyframe", layer)
	}
	st, ok := r.ScreenQuality(viewer.id, pub.id)
	if !ok {
		t.Fatalf("after choosing %d: quality not ok", layer)
	}
	if st.Layer != layer {
		t.Fatalf("after choosing %d: got layer %d", layer, st.Layer)
	}
	if st.Auto {
		t.Fatalf("after choosing %d: expected manual quality, got auto", layer)
	}
}

func assertLayersWanted(t *testing.T, mu *sync.Mutex, wanted *[][]int) {
	t.Helper()
	mu.Lock()
	defer mu.Unlock()
	if len(*wanted) == 0 {
		t.Fatal("the sender was never told which layers are needed: empty list")
	}
	latest := (*wanted)[len(*wanted)-1]
	if len(latest) == 0 {
		t.Fatalf("the sender was never told which layers are needed: latest wanted entry empty: %v", *wanted)
	}
}

func TestViewerIsMovedBetweenLayersWithoutBreakingTheStream(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()

	var wantedMu sync.Mutex
	var wanted [][]int
	s.SetLayersWantedHandler(func(_, _ uuid.UUID, l []int) {
		wantedMu.Lock()
		wanted = append(wanted, l)
		wantedMu.Unlock()
	})

	pub := newClient(t)
	layers := pub.publishLayers(25)
	pub.join(s, room)
	viewer := newClient(t)
	viewer.join(s, room)
	r := s.Room(room)
	waitSources(t, r, pub.id, SourceScreen, 3)
	if err := r.SetScreenLayers(pub.id, []Layer{{3840, 2160, 60}, {1920, 1080, 30}, {1280, 720, 30}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Subscribe(viewer.id, pub.id, SourceScreen, false, true); err != nil {
		t.Fatal(err)
	}

	// Automatic starts on the best layer.
	waitTrack(t, viewer)
	first := waitLayer(t, viewer, 0)
	checkOneStream(t, first)
	assertInitialQuality(t, r, viewer.id, pub.id)

	for _, layer := range []int{2, 1, 0, 2} {
		assertLayerTransition(t, r, viewer, pub, layers, layer)
	}

	assertLayersWanted(t, &wantedMu, &wanted)
}

func TestSecondViewerChoosesItsOwnLayer(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()
	pub := newClient(t)
	pub.publishLayers(25)
	pub.join(s, room)
	r := s.Room(room)
	waitSources(t, r, pub.id, SourceScreen, 3)

	high, low := newClient(t), newClient(t)
	high.join(s, room)
	low.join(s, room)
	if err := r.SetScreenQuality(low.id, pub.id, 2); err != nil {
		t.Fatal(err)
	}
	for _, v := range []*client{high, low} {
		if err := r.Subscribe(v.id, pub.id, SourceScreen, false, true); err != nil {
			t.Fatal(err)
		}
	}
	waitTrack(t, high)
	waitTrack(t, low)
	checkOneStream(t, waitLayer(t, high, 0))
	checkOneStream(t, waitLayer(t, low, 2))
	// Both stay on their layer.
	for layer, v := range map[int]*client{0: high, 2: low} {
		for _, p := range gather(v, 300*time.Millisecond) {
			if layerOf(p) != layer {
				t.Fatalf("viewer on layer %d received a packet of layer %d", layer, layerOf(p))
			}
		}
	}
}

func TestLayersAreAnnouncedAndWithdrawn(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()
	var mu sync.Mutex
	var announced [][]Layer
	s.SetScreenLayersHandler(func(_, _ uuid.UUID, l []Layer) {
		mu.Lock()
		announced = append(announced, l)
		mu.Unlock()
	})
	pub := newClient(t)
	pub.publishLayers(25)
	pub.join(s, room)
	r := s.Room(room)
	waitSources(t, r, pub.id, SourceScreen, 3)

	if err := r.SetScreenLayers(pub.id, []Layer{{1920, 1080, 60}, {1280, 720, 30}}); err != nil {
		t.Fatal(err)
	}
	if got := r.ScreenLayers()[pub.id]; len(got) != 2 || got[1].Height != 720 {
		t.Fatalf("ScreenLayers = %v", got)
	}
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(announced) == 1 && len(announced[0]) == 2 })

	r.RemoveUserSource(pub.id, SourceScreen)
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(announced) >= 2 && len(announced[len(announced)-1]) == 0
	})
	if len(r.ScreenLayers()) != 0 {
		t.Fatal("layers of a stopped share are still listed")
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		if cond() {
			return
		}
		select {
		case <-timeout:
			t.Fatal("condition never became true")
		case <-time.After(10 * time.Millisecond):
		}
	}
}
