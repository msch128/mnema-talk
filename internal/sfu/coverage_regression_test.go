package sfu

import (
	"context"
	"log/slog"
	"net"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
	"golang.org/x/net/dns/dnsmessage"
)

// Serve actual DNS responses on a local UDP socket. Resolver.Dial is the
// standard library's public transport seam; no external DNS is contacted.
func localAnnounceDNS(t *testing.T) *atomic.Int32 {
	t.Helper()
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	state := &atomic.Int32{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 512)
		for {
			n, addr, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			var query dnsmessage.Message
			if query.Unpack(buf[:n]) != nil {
				continue
			}
			response := dnsmessage.Message{
				Header:    dnsmessage.Header{ID: query.ID, Response: true, Authoritative: true, RecursionAvailable: true},
				Questions: query.Questions,
			}
			current := state.Load()
			if current < 0 {
				response.Header.RCode = dnsmessage.RCodeNameError
			} else {
				for _, q := range query.Questions {
					if q.Type == dnsmessage.TypeA {
						response.Answers = append(response.Answers, dnsmessage.Resource{
							Header: dnsmessage.ResourceHeader{Name: q.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET},
							Body:   &dnsmessage.AResource{A: [4]byte{198, 51, 100, byte(current + 1)}},
						})
					}
				}
			}
			raw, err := response.Pack()
			if err == nil {
				_, _ = conn.WriteTo(raw, addr)
			}
		}
	}()
	old := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "udp4", conn.LocalAddr().String())
	}}
	t.Cleanup(func() {
		net.DefaultResolver = old
		_ = conn.Close()
		<-done
	})
	return state
}

func waitSFUCondition(t *testing.T, label string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", label)
}

func captureSFULog(t *testing.T) *synchronizedLogCapture {
	t.Helper()
	buf := &synchronizedLogCapture{}
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	return buf
}

func TestAnnounceRefreshTracksDNSAndPreservesAddressOnFailure(t *testing.T) {
	dns := localAnnounceDNS(t)
	logs := captureSFULog(t)
	s, err := NewSFU(0, 0, []string{"198.51.100.1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s.KeepAnnounceCurrent(ctx, []string{"announce.test"}, 5*time.Millisecond)
	// Repeated resolutions of an unchanged address must leave the API intact.
	api := s.currentAPI()
	time.Sleep(30 * time.Millisecond)
	if s.currentAPI() != api {
		t.Fatal("unchanged DNS response rebuilt the WebRTC API")
	}
	dns.Store(1)
	waitSFUCondition(t, "changed DNS announcement", func() bool {
		return slices.Equal(s.AnnouncedIPs(), []string{"198.51.100.2"})
	})
	if s.currentAPI() == api {
		t.Fatal("changed DNS address did not rebuild the API")
	}
	dns.Store(-1)
	waitSFUCondition(t, "DNS failure warning", func() bool {
		return strings.Contains(logs.String(), "resolve failed, keeping current addresses")
	})
	if !slices.Equal(s.AnnouncedIPs(), []string{"198.51.100.2"}) {
		t.Fatal("failed DNS resolution discarded the working address")
	}
	cancel()
	// Static announcements do not start a ticker, including when interval is zero.
	s.KeepAnnounceCurrent(context.Background(), []string{"198.51.100.2"}, 0)
}

func TestAnnounceRefreshReportsShutdownAndStopsOnCancellation(t *testing.T) {
	dns := localAnnounceDNS(t)
	logs := captureSFULog(t)
	s, err := NewSFU(0, 0, []string{"198.51.100.1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	dns.Store(1)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s.KeepAnnounceCurrent(ctx, []string{"announce.test"}, 5*time.Millisecond)
	waitSFUCondition(t, "closed SFU announcement warning", func() bool {
		return strings.Contains(logs.String(), "webrtc announce: update failed")
	})
	cancel()
	if !slices.Equal(s.AnnouncedIPs(), []string{"198.51.100.1"}) {
		t.Fatal("refresh changed a shut down SFU")
	}
}

func TestInvalidTransportRangeRejected(t *testing.T) {
	if _, err := NewSFU(50001, 50000, nil, nil); err == nil || !strings.Contains(err.Error(), "UDP port range") {
		t.Fatalf("inverted range error = %v", err)
	}
	if _, err := buildAPI(0, 0, []string{"127.0.0.1"}); err != nil {
		t.Fatalf("default API builder: %v", err)
	}
}

// A publisher can send a truncated RTP datagram before the next good packet.
// Keep this fixture at the existing byte-reader boundary, so the test checks
// the actual forwarding parser rather than an artificial TrackRemote state.
type truncatedThenValidRTP struct {
	truncated bool
	valid     fakeRemote
}

func (r *truncatedThenValidRTP) Read(buf []byte) (int, interceptor.Attributes, error) {
	if !r.truncated {
		r.truncated = true
		return copy(buf, []byte{0x80}), nil, nil
	}
	return r.valid.Read(buf)
}

func TestForwardSkipsTruncatedRTPAndContinuesWithValidPackets(t *testing.T) {
	user := uuid.New()
	r := &Room{peers: map[uuid.UUID]*Peer{}, trackLocals: map[string]*TrackInfo{}, notifier: &mediaNotifier{}}
	key := trackKey(user, "datagram-input")
	info := &TrackInfo{SenderID: user, Kind: webrtc.RTPCodecTypeAudio, Source: SourceAudio}
	r.trackLocals[key] = info
	local := &failingLocal{}
	r.forward(&truncatedThenValidRTP{valid: fakeRemote{n: 3}}, local, key, info)
	if local.writes != 3 {
		t.Fatalf("forwarded %d packets, want the three valid packets after truncation", local.writes)
	}
	r.mu.RLock()
	_, stillPublished := r.trackLocals[key]
	r.mu.RUnlock()
	if stillPublished {
		t.Fatal("EOF after valid packets left the track published")
	}
}

func TestJoinFailureDropsEmptyRoom(t *testing.T) {
	// TURN URLs without a credential are rejected by Pion's public
	// configuration validation when a connection is created.
	s, err := NewSFU(0, 0, nil, []string{"turn:127.0.0.1:3478"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	roomID := uuid.New()
	if _, _, err := s.Join(roomID, uuid.New(), nil, nil); err == nil {
		t.Fatal("TURN configuration without credentials accepted")
	}
	if s.Room(roomID) != nil {
		t.Fatal("failed first join left an empty room")
	}
}

func TestRemovePeerWithoutRoomStillClosesTransport(t *testing.T) {
	s := newTestSFU(t)
	roomID := uuid.New()
	_, peer, err := s.Join(roomID, uuid.New(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.RemovePeer(roomID, peer)
	if s.Room(roomID) != nil {
		t.Fatal("last peer removal did not drop the room")
	}
	s.RemovePeer(roomID, nil)
	s.RemovePeer(roomID, peer)
	if peer.PC.ConnectionState() != webrtc.PeerConnectionStateClosed {
		t.Fatal("stale transport still open")
	}
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	s.RemovePeer(uuid.New(), &Peer{ID: uuid.New(), PC: pc})
	if pc.ConnectionState() != webrtc.PeerConnectionStateClosed {
		t.Fatal("unknown-room peer transport still open")
	}
}

func TestOfferWatchdogStopsAfterResendLimit(t *testing.T) {
	logs := captureSFULog(t)
	peer, offers := joinWithOffers(t, 15*time.Millisecond)
	first := nextOffer(t, offers, "initial offer")
	for range maxOfferResends {
		resend := nextOffer(t, offers, "bounded resend")
		if originLine(resend.SDP) != originLine(first.SDP) {
			t.Fatal("watchdog replaced rather than resent the outstanding offer")
		}
	}
	waitSFUCondition(t, "watchdog exhaustion", func() bool {
		return strings.Contains(logs.String(), "sfu offer never answered, giving up")
	})
	select {
	case <-offers:
		t.Fatal("watchdog exceeded the resend limit")
	case <-time.After(60 * time.Millisecond):
	}
	if peer.PC.SignalingState() != webrtc.SignalingStateHaveLocalOffer {
		t.Fatal("watchdog exhaustion corrupted pending offer state")
	}
}

func TestPublishTestVideoRejectsUnsupportedSourceAndMissingPeer(t *testing.T) {
	s := newTestSFU(t)
	r, peer, err := s.Join(uuid.New(), uuid.New(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.PublishTestVideo(peer.ID, SourceAudio); err == nil {
		t.Fatal("audio accepted as video")
	}
	if err := r.PublishTestVideo(uuid.New(), SourceScreen); err == nil {
		t.Fatal("unjoined publisher accepted")
	}
	var subs Subscriptions
	if subs.Wants(peer.ID, webrtc.RTPCodecTypeVideo, SourceAudio) {
		t.Fatal("unknown video source forwarded")
	}
}

func TestSelectedPairWithoutTransceiver(t *testing.T) {
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	pair, err := selectedPair(pc)
	if err != nil || pair != nil {
		t.Fatalf("unconnected peer pair = %v, %v", pair, err)
	}
	if err := pc.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSubscriberFullIntraRequestReachesPublisher(t *testing.T) {
	s := newTestSFU(t)
	roomID := uuid.New()
	publisher := newClient(t)
	publisher.publishVideo("feedback-screen")
	publisher.join(s, roomID)
	r := s.Room(roomID)
	viewer := newClient(t)
	if err := r.Subscribe(viewer.id, publisher.id, SourceScreen, false, true); err != nil {
		t.Fatal(err)
	}
	viewer.join(s, roomID)
	remote := waitTrack(t, viewer)
	// Wait out the keyframe throttle after the initial subscription's PLI,
	// then drain it so the assertion concerns only the subscriber feedback.
	time.Sleep(keyframeMinInterval + 50*time.Millisecond)
	for len(publisher.plis) > 0 {
		<-publisher.plis
	}
	if err := viewer.pc.WriteRTCP([]rtcp.Packet{&rtcp.FullIntraRequest{
		MediaSSRC: uint32(remote.SSRC()),
		FIR:       []rtcp.FIREntry{{SSRC: uint32(remote.SSRC()), SequenceNumber: 1}},
	}}); err != nil {
		t.Fatal(err)
	}
	select {
	case ssrc := <-publisher.plis:
		if ssrc == 0 {
			t.Fatal("feedback did not target the publisher's SSRC")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("subscriber FIR did not request a publisher keyframe")
	}
}

func TestMultipleSharesAnnounceSortedAudiencesAndGlobalSnapshot(t *testing.T) {
	s := newTestSFU(t)
	r, first, err := s.Join(uuid.New(), uuid.MustParse("00000000-0000-0000-0000-000000000002"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, second, err := s.Join(r.ID, uuid.MustParse("00000000-0000-0000-0000-000000000001"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Join(uuid.New(), uuid.New(), nil, nil); err != nil {
		t.Fatal(err)
	}
	log := recordViewers(s, r)
	// Pause delivery while both shares become watched. The ensuing snapshot
	// must announce a deterministic order even though tracks are held in maps.
	r.notifyMu.Lock()
	func() {
		defer r.notifyMu.Unlock()
		for _, publisher := range []*Peer{first, second} {
			if err := r.PublishTestVideo(publisher.ID, SourceScreen); err != nil {
				t.Fatal(err)
			}
		}
		if err := r.Subscribe(first.ID, second.ID, SourceScreen, false, true); err != nil {
			t.Fatal(err)
		}
		if err := r.Subscribe(second.ID, first.ID, SourceScreen, false, true); err != nil {
			t.Fatal(err)
		}
	}()
	events := log.flush()
	if len(events) != 2 || events[0].sharer != second.ID || events[1].sharer != first.ID {
		t.Fatalf("audiences not ordered by sharer ID: %+v", events)
	}
	states := s.AllMediaStates()
	if len(states) != 1 || len(states[r.ID]) != 2 || !states[r.ID][first.ID].Screen || !states[r.ID][second.ID].Screen {
		t.Fatalf("global active media snapshot = %+v", states)
	}
	// Mutating a snapshot must not corrupt the room's subsequent snapshot.
	delete(states[r.ID], first.ID)
	if !s.AllMediaStates()[r.ID][first.ID].Screen {
		t.Fatal("global snapshot aliases room state")
	}
	s.CloseRoom(uuid.New()) // Unknown channels are an idempotent no-op.
	if s.Room(r.ID) != r {
		t.Fatal("closing an unknown channel removed an active room")
	}
}

func TestConcurrentViewerCloseAndSubscriptionsPreservePublisher(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	s := newTestSFU(t)
	roomID := uuid.New()
	publisher := newClient(t)
	publisher.publishVideo("close-race-screen")
	publisher.publishVideo("close-race-camera")
	publisher.join(s, roomID)
	r := s.Room(roomID)
	awaitTrack := func(viewer *client) *webrtc.TrackRemote {
		t.Helper()
		select {
		case track := <-viewer.tracks:
			return track
		case err := <-viewer.errors:
			t.Fatalf("live viewer signaling failed: %v", err)
		case <-ctx.Done():
			t.Fatal("close/renegotiation regression exceeded its 15 second deadline")
		}
		return nil
	}
	for range 4 {
		viewer := newClient(t)
		viewer.join(s, roomID)
		if got := awaitTrack(viewer).StreamID(); got != CameraStreamPrefix+publisher.id.String() {
			t.Fatalf("initial viewer received %q, want publisher camera", got)
		}
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		errs := make(chan error, 2)
		go func() {
			defer wg.Done()
			<-start
			for i := range 48 {
				if err := r.Subscribe(viewer.id, publisher.id, SourceScreen, false, i%2 == 0); err != nil {
					errs <- err
					return
				}
				if err := r.Subscribe(viewer.id, publisher.id, SourceCamera, false, i%2 != 0); err != nil {
					errs <- err
					return
				}
			}
		}()
		go func() {
			defer wg.Done()
			<-start
			errs <- viewer.peer.PC.Close()
		}()
		close(start)
		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-ctx.Done():
			t.Fatal("subscription changes or peer close did not terminate")
		}
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		waitSFUCondition(t, "closed viewer removal", func() bool { return r.GetPeer(viewer.id) == nil })
		if err := viewer.pc.Close(); err != nil {
			t.Fatal(err)
		}
		if r.GetPeer(publisher.id) != publisher.peer || s.Stats().Peers != 1 {
			t.Fatal("closing a viewer removed the publisher or leaked a peer")
		}
		state := r.MediaStates()[publisher.id]
		if !state.Camera || !state.Screen || len(r.ScreenViewers()[publisher.id]) != 0 {
			t.Fatalf("closed viewer corrupted publisher media or audience: %+v", state)
		}
	}
	// A new viewer must receive the still-live publisher's two independent
	// sources after the closed viewers' offers, answers and removals settle.
	replacement := newClient(t)
	if err := r.Subscribe(replacement.id, publisher.id, SourceScreen, false, true); err != nil {
		t.Fatal(err)
	}
	replacement.join(s, roomID)
	streams := map[string]bool{}
	for range 2 {
		streams[awaitTrack(replacement).StreamID()] = true
	}
	if !streams[publisher.id.String()] || !streams[CameraStreamPrefix+publisher.id.String()] {
		t.Fatalf("replacement viewer did not receive screen and camera: %v", streams)
	}
}

func TestICEReportsDisconnectedAfterRealClientTransportCloses(t *testing.T) {
	logs := captureSFULog(t)
	s := newTestSFU(t)
	publisher := newClient(t)
	publisher.publishAudio("disconnect-microphone")
	publisher.join(s, uuid.New())
	waitSFUCondition(t, "connected publisher ICE transport", func() bool {
		return publisher.peer.PC.ICEConnectionState() == webrtc.ICEConnectionStateConnected
	})
	if err := publisher.pc.Close(); err != nil {
		t.Fatal(err)
	}
	// Pion's default ICE disconnection timeout is five seconds. Closing the
	// actual client's socket makes consent checks fail without synthetic ICE
	// callbacks or modified transport settings.
	deadline := time.NewTimer(9 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(10 * time.Millisecond)
	defer poll.Stop()
	for {
		select {
		case <-deadline.C:
			t.Fatalf("closed client did not produce an ICE disconnected warning; state=%s", publisher.peer.PC.ICEConnectionState())
		case <-poll.C:
			if strings.Contains(logs.String(), "msg=\"sfu ice disconnected\"") {
				if publisher.peer.PC.ICEConnectionState() != webrtc.ICEConnectionStateDisconnected {
					t.Fatalf("disconnect warning disagrees with ICE state: %s", publisher.peer.PC.ICEConnectionState())
				}
				return
			}
		}
	}
}
