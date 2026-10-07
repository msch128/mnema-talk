package sfu

import (
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// client is a browser stand-in: a plain Pion PeerConnection wired to an SFU
// peer through in-memory signaling.
type client struct {
	t      *testing.T
	id     uuid.UUID
	pc     *webrtc.PeerConnection
	peer   *Peer
	tracks chan *webrtc.TrackRemote
	plis   chan uint32 // SSRCs the SFU asked this client to refresh
	errors chan error
}

func newTestSFU(t *testing.T) *SFU {
	t.Helper()
	// These are in-process media tests, not a NAT/interface compatibility
	// matrix. Keep them independent of a runner's VM and VPN interfaces.
	s, err := NewSFU(0, 0, []string{"127.0.0.1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func newClient(t *testing.T) *client {
	t.Helper()
	settings := webrtc.SettingEngine{}
	settings.SetIncludeLoopbackCandidate(true)
	settings.SetIPFilter(func(ip net.IP) bool { return ip.IsLoopback() })
	settings.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	pc, err := webrtc.NewAPI(webrtc.WithSettingEngine(settings)).NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	c := &client{t: t, id: uuid.New(), pc: pc,
		tracks: make(chan *webrtc.TrackRemote, 8), plis: make(chan uint32, 64), errors: make(chan error, 8)}
	pc.OnTrack(func(tr *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		c.tracks <- tr
		go func() {
			buf := make([]byte, 1500)
			for {
				if _, _, err := tr.Read(buf); err != nil {
					return
				}
			}
		}()
	})
	return c
}

// join connects the client to the room; the SFU drives the offers.
func (c *client) join(s *SFU, room uuid.UUID) {
	c.t.Helper()
	var mu sync.Mutex
	var peer *Peer
	ready := make(chan struct{})
	_, p, err := s.Join(room, c.id,
		func(webrtc.SessionDescription) {
			<-ready
			go func() {
				if err := c.answer(peer, &mu); err != nil {
					select {
					case c.errors <- err:
					default:
					}
				}
			}()
		}, nil)
	if err != nil {
		c.t.Fatal(err)
	}
	peer = p
	c.peer = p
	close(ready)
}

// Use complete gathered descriptions. The former trickle callbacks discarded
// candidates arriving before SetRemoteDescription/SetAnswer and ignored their
// errors, making media assertions depend on goroutine scheduling.
func (c *client) answer(p *Peer, mu *sync.Mutex) error {
	mu.Lock()
	defer mu.Unlock()
	select {
	case <-webrtc.GatheringCompletePromise(p.PC):
	case <-time.After(5 * time.Second):
		return fmt.Errorf("SFU ICE gathering timed out")
	}
	offer := p.PC.LocalDescription()
	if offer == nil {
		return fmt.Errorf("SFU offer missing after gathering")
	}
	if err := c.pc.SetRemoteDescription(*offer); err != nil {
		return fmt.Errorf("client remote description: %w", err)
	}
	ans, err := c.pc.CreateAnswer(nil)
	if err != nil {
		return fmt.Errorf("client answer: %w", err)
	}
	gathered := webrtc.GatheringCompletePromise(c.pc)
	if err := c.pc.SetLocalDescription(ans); err != nil {
		return fmt.Errorf("client local description: %w", err)
	}
	select {
	case <-gathered:
	case <-time.After(5 * time.Second):
		return fmt.Errorf("client ICE gathering timed out")
	}
	if err := p.SetAnswer(*c.pc.LocalDescription()); err != nil {
		return fmt.Errorf("SFU remote answer: %w", err)
	}
	return nil
}

// publishVideo adds a VP8 track before joining and keeps sending packets.
func (c *client) publishVideo(trackID string) *webrtc.TrackLocalStaticRTP {
	c.t.Helper()
	return c.publish(trackID, webrtc.MimeTypeVP8, 96, []byte{0x10, 0x00, 0x00, 0x9d, 0x01, 0x2a})
}

// publishAudio adds an Opus track before joining and keeps sending packets.
// The client's audio tracks take the SFU's audio lines in order: the first
// is the microphone, the second the screen share's sound.
func (c *client) publishAudio(trackID string) *webrtc.TrackLocalStaticRTP {
	c.t.Helper()
	return c.publish(trackID, webrtc.MimeTypeOpus, 111, []byte{0xfc, 0xff, 0xfe})
}

func (c *client) publish(trackID, mime string, pt uint8, payload []byte) *webrtc.TrackLocalStaticRTP {
	c.t.Helper()
	track, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: mime}, trackID, "stream-"+c.id.String())
	if err != nil {
		c.t.Fatal(err)
	}
	sender, err := c.pc.AddTrack(track)
	if err != nil {
		c.t.Fatal(err)
	}
	go func() {
		for {
			pkts, _, err := sender.ReadRTCP()
			if err != nil {
				return
			}
			for _, p := range pkts {
				if pli, ok := p.(*rtcp.PictureLossIndication); ok {
					select {
					case c.plis <- pli.MediaSSRC:
					default:
					}
				}
			}
		}
	}()
	stop := make(chan struct{})
	c.t.Cleanup(func() { close(stop) })
	go func() {
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		var seq uint16
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				seq++
				_ = track.WriteRTP(&rtp.Packet{
					Header:  rtp.Header{Version: 2, PayloadType: pt, SequenceNumber: seq, Timestamp: uint32(seq) * 3000},
					Payload: payload,
				})
			}
		}
	}()
	return track
}

func waitTrack(t *testing.T, c *client) *webrtc.TrackRemote {
	t.Helper()
	select {
	case tr := <-c.tracks:
		return tr
	case err := <-c.errors:
		t.Fatalf("signaling failed: %v", err)
		return nil
	case <-time.After(10 * time.Second):
		t.Fatal("no track forwarded")
		return nil
	}
}

func waitPLI(t *testing.T, c *client) {
	t.Helper()
	select {
	case <-c.plis:
	case err := <-c.errors:
		t.Fatalf("signaling failed: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("publisher never received a keyframe request")
	}
}

func drainPLIs(c *client) {
	for {
		select {
		case <-c.plis:
		default:
			return
		}
	}
}

func TestForwardsVideoAndRequestsKeyframeFromPublisher(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()

	pub := newClient(t)
	pub.publishVideo("screen")
	pub.join(s, room)

	sub := newClient(t)
	sub.join(s, room)
	// Screen shares are opt-in.
	_ = s.Room(room).Subscribe(sub.id, pub.id, SourceScreen, false, true)

	waitTrack(t, sub)
	// A new viewer must get a keyframe from the publisher without waiting
	// for the next natural one.
	waitPLI(t, pub)

	// An explicit request from the viewer (webrtc_request_keyframe) goes to
	// the publisher too, not back to the viewer.
	time.Sleep(keyframeMinInterval + 50*time.Millisecond)
	drainPLIs(pub)
	s.Room(room).DispatchKeyframe(sub.id)
	waitPLI(t, pub)
}

func TestRejoinKeepsTheNewPeer(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()
	user := uuid.New()
	noop := func(webrtc.SessionDescription) {}
	noICE := func(*webrtc.ICECandidateInit) {}

	_, old, err := s.Join(room, user, noop, noICE)
	if err != nil {
		t.Fatal(err)
	}
	r, fresh, err := s.Join(room, user, noop, noICE)
	if err != nil {
		t.Fatal(err)
	}

	// The old connection's close callback and a late unregister of the old
	// socket must not tear down the replacement.
	time.Sleep(200 * time.Millisecond)
	s.RemovePeer(room, old)

	if got := r.GetPeer(user); got != fresh {
		t.Fatalf("peer after rejoin = %p, want the new peer %p", got, fresh)
	}
	if fresh.PC.ConnectionState() == webrtc.PeerConnectionStateClosed {
		t.Fatal("new peer connection was closed")
	}
	if s.Room(room) == nil {
		t.Fatal("room was dropped while a peer is still in it")
	}

	s.RemovePeer(room, fresh)
	if s.Room(room) != nil {
		t.Fatal("empty room was not dropped")
	}
}

func TestSameTrackIDFromTwoUsersIsForwardedSeparately(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()

	a := newClient(t)
	a.publishVideo("video")
	a.join(s, room)
	b := newClient(t)
	b.publishVideo("video")
	b.join(s, room)

	viewer := newClient(t)
	viewer.join(s, room)
	_ = s.Room(room).Subscribe(viewer.id, a.id, SourceScreen, false, true)
	_ = s.Room(room).Subscribe(viewer.id, b.id, SourceScreen, false, true)

	first := waitTrack(t, viewer)
	second := waitTrack(t, viewer)
	if first.StreamID() == second.StreamID() {
		t.Fatalf("both tracks share stream %q; one user's track overwrote the other's", first.StreamID())
	}
	for _, tr := range []*webrtc.TrackRemote{first, second} {
		if tr.StreamID() != a.id.String() && tr.StreamID() != b.id.String() {
			t.Fatalf("stream id %q does not name the publishing user", tr.StreamID())
		}
	}
}

func (r *Room) sourceCount(userID uuid.UUID, source Source) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	n := 0
	for _, t := range r.trackLocals {
		if t.SenderID == userID && t.Source == source {
			n++
		}
	}
	return n
}

func waitSources(t *testing.T, r *Room, user uuid.UUID, source Source, want int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if r.sourceCount(user, source) == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s tracks of %s = %d, want %d", source, user, r.sourceCount(user, source), want)
}

func TestCameraAndScreenAreForwardedAsSeparateSources(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()

	pub := newClient(t)
	pub.publishVideo("screen") // first video m-line: the screen
	pub.publishVideo("camera") // second video m-line: the camera
	pub.join(s, room)

	viewer := newClient(t)
	viewer.join(s, room)
	_ = s.Room(room).Subscribe(viewer.id, pub.id, SourceScreen, false, true)

	streams := map[string]bool{}
	for range 2 {
		streams[waitTrack(t, viewer).StreamID()] = true
	}
	if !streams[pub.id.String()] || !streams[CameraStreamPrefix+pub.id.String()] {
		t.Fatalf("viewer got streams %v, want the screen under the user ID and the camera under %q", streams, CameraStreamPrefix)
	}

	r := s.Room(room)
	waitSources(t, r, pub.id, SourceScreen, 1)
	waitSources(t, r, pub.id, SourceCamera, 1)

	// Stopping the camera keeps the screen share.
	r.RemoveUserSource(pub.id, SourceCamera)
	if r.sourceCount(pub.id, SourceCamera) != 0 || r.sourceCount(pub.id, SourceScreen) != 1 {
		t.Fatal("stopping the camera must leave the screen share published")
	}

	// The publisher keeps sending on the same transceiver (stop and start
	// again): the camera is published again without a new OnTrack.
	waitSources(t, r, pub.id, SourceCamera, 1)
}

func TestScreenAudioReachesOnlyTheViewersOfTheShare(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()

	pub := newClient(t)
	pub.publishAudio("mic")          // first audio m-line: the microphone
	pub.publishAudio("screen-audio") // second audio m-line: the screen's sound
	pub.join(s, room)

	watcher := newClient(t)
	watcher.join(s, room)
	listener := newClient(t)
	listener.join(s, room)

	r := s.Room(room)
	waitSources(t, r, pub.id, SourceAudio, 1)
	waitSources(t, r, pub.id, SourceScreenAudio, 1)
	_ = r.Subscribe(watcher.id, pub.id, SourceScreen, false, true)

	streams := map[string]bool{}
	for range 2 {
		streams[waitTrack(t, watcher).StreamID()] = true
	}
	if !streams[pub.id.String()] || !streams[ScreenAudioStreamPrefix+pub.id.String()] {
		t.Fatalf("watcher got streams %v, want the voice under the user ID and the screen's sound under %q", streams, ScreenAudioStreamPrefix)
	}

	// Someone who does not watch only hears the voice.
	if got := waitTrack(t, listener).StreamID(); got != pub.id.String() {
		t.Fatalf("listener got stream %q, want only the voice", got)
	}
	select {
	case tr := <-listener.tracks:
		t.Fatalf("listener also got stream %q", tr.StreamID())
	case <-time.After(1500 * time.Millisecond):
	}

	// Stopping the share takes its sound along, the voice stays.
	r.RemoveUserSource(pub.id, SourceScreen)
	if r.sourceCount(pub.id, SourceScreenAudio) != 0 || r.sourceCount(pub.id, SourceAudio) != 1 {
		t.Fatal("stopping the share must unpublish its sound and keep the voice")
	}
}
