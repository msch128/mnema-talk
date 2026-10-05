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

// client is a browser stand-in: a plain Pion PeerConnection wired to an SFU
// peer through in-memory signaling.
type client struct {
	t      *testing.T
	id     uuid.UUID
	pc     *webrtc.PeerConnection
	peer   *Peer
	tracks chan *webrtc.TrackRemote
	plis   chan uint32 // SSRCs the SFU asked this client to refresh
}

func newTestSFU(t *testing.T) *SFU {
	t.Helper()
	s, err := NewSFU(0, 0, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func newClient(t *testing.T) *client {
	t.Helper()
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	c := &client{t: t, id: uuid.New(), pc: pc,
		tracks: make(chan *webrtc.TrackRemote, 8), plis: make(chan uint32, 64)}
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
		func(offer webrtc.SessionDescription) {
			<-ready
			go c.answer(peer, offer, &mu)
		},
		func(cand *webrtc.ICECandidateInit) {
			<-ready
			_ = c.pc.AddICECandidate(*cand)
		})
	if err != nil {
		c.t.Fatal(err)
	}
	peer = p
	c.peer = p
	c.pc.OnICECandidate(func(cand *webrtc.ICECandidate) {
		if cand != nil {
			_ = p.PC.AddICECandidate(cand.ToJSON())
		}
	})
	close(ready)
}

func (c *client) answer(p *Peer, offer webrtc.SessionDescription, mu *sync.Mutex) {
	mu.Lock()
	defer mu.Unlock()
	if err := c.pc.SetRemoteDescription(offer); err != nil {
		return
	}
	ans, err := c.pc.CreateAnswer(nil)
	if err != nil {
		return
	}
	if err := c.pc.SetLocalDescription(ans); err != nil {
		return
	}
	_ = p.SetAnswer(ans)
}

// publishVideo adds a VP8 track before joining and keeps sending packets.
func (c *client) publishVideo(trackID string) *webrtc.TrackLocalStaticRTP {
	c.t.Helper()
	track, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8}, trackID, "stream-"+c.id.String())
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
					Header:  rtp.Header{Version: 2, PayloadType: 96, SequenceNumber: seq, Timestamp: uint32(seq) * 3000},
					Payload: []byte{0x10, 0x00, 0x00, 0x9d, 0x01, 0x2a},
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
	case <-time.After(10 * time.Second):
		t.Fatal("no track forwarded")
		return nil
	}
}

func waitPLI(t *testing.T, c *client) {
	t.Helper()
	select {
	case <-c.plis:
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
