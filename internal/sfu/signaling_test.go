package sfu

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

// A stalled peer's signaling must not block membership, media snapshots or
// another peer's initial offer. Repeated changes coalesce behind that peer.
func TestBlockedOfferDoesNotHoldRoomOrOtherPeers(t *testing.T) {
	s, err := NewSFU(0, 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	channel := uuid.New()
	t.Cleanup(func() { s.CloseRoom(channel) })
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	var offers atomic.Int32
	r, peer, err := s.Join(channel, uuid.New(), func(webrtc.SessionDescription) {
		offers.Add(1)
		entered <- struct{}{}
		<-release
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("initial offer not sent")
	}
	available := make(chan struct{})
	go func() {
		r.MediaStates()
		close(available)
	}()
	select {
	case <-available:
	case <-time.After(time.Second):
		t.Fatal("stalled offer holds the room lock")
	}
	otherOffer := make(chan struct{}, 1)
	joined := make(chan error, 1)
	go func() {
		_, _, err := s.Join(channel, uuid.New(), func(webrtc.SessionDescription) { otherOffer <- struct{}{} }, nil)
		joined <- err
	}()
	select {
	case err := <-joined:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("stalled offer blocks another join")
	}
	select {
	case <-otherOffer:
	case <-time.After(5 * time.Second):
		t.Fatal("stalled offer blocks another peer's offer")
	}
	for range 50 {
		r.SignalPeerConnections()
	}
	if offers.Load() != 1 || !peer.negotiationPending.Load() {
		t.Fatal("changes were not coalesced behind the stalled offer")
	}
}

// The answer can arrive while the offer callback is still returning. A
// membership change during that interval must still produce the next offer.
func TestChangeWhileOfferCallbackAndAnswerOverlap(t *testing.T) {
	s, err := NewSFU(0, 0, []string{"127.0.0.1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	client, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	offers := make(chan webrtc.SessionDescription, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	var count atomic.Int32
	r, peer, err := s.Join(uuid.New(), uuid.New(), func(offer webrtc.SessionDescription) {
		offers <- offer
		if count.Add(1) == 1 {
			<-release
		}
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var initial webrtc.SessionDescription
	select {
	case initial = <-offers:
	case <-time.After(5 * time.Second):
		t.Fatal("initial offer missing")
	}
	track, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus}, "late-audio", "late-publisher")
	if err != nil {
		t.Fatal(err)
	}
	r.addTrack(track.ID(), &TrackInfo{Track: track, SenderID: uuid.New(), Kind: webrtc.RTPCodecTypeAudio, Source: SourceAudio})
	// Signal synchronously so the pending request is definitely present before
	// SetAnswer resumes the stable connection.
	r.SignalPeerConnections()
	if err := client.SetRemoteDescription(initial); err != nil {
		t.Fatal(err)
	}
	answer, err := client.CreateAnswer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SetLocalDescription(answer); err != nil {
		t.Fatal(err)
	}
	if err := peer.SetAnswer(answer); err != nil {
		t.Fatal(err)
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case offer := <-offers:
		if !strings.Contains(offer.SDP, track.ID()) {
			t.Fatal("pending change missing from follow-up offer")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pending change lost after answer/offer callback overlap")
	}
}

// A pending retry on one stable connection must settle without creating a
// pending request on every other connection in the room. Whole-room retries
// could keep a burst alive by repeatedly marking unrelated workers pending.
func TestPeerRetryDoesNotWakeUnrelatedPeer(t *testing.T) {
	s, err := NewSFU(0, 0, []string{"127.0.0.1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	offers := make(chan webrtc.SessionDescription, 1)
	r, peer, err := s.Join(uuid.New(), uuid.New(), func(offer webrtc.SessionDescription) {
		offers <- offer
		<-release
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var offer webrtc.SessionDescription
	select {
	case offer = <-offers:
	case <-time.After(5 * time.Second):
		t.Fatal("initial offer missing")
	}
	otherOffers := make(chan struct{}, 1)
	_, other, err := s.Join(r.ID, uuid.New(), func(webrtc.SessionDescription) { otherOffers <- struct{}{} }, nil)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-otherOffers:
	case <-time.After(5 * time.Second):
		t.Fatal("other peer offer missing")
	}
	waitSignalingIdle(t, other)
	other.negotiationPending.Store(false)
	// Coalesce a burst specifically behind the first peer's blocked callback.
	for range 100 {
		r.requestPeerSignal(peer)
	}
	client, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.SetRemoteDescription(offer); err != nil {
		t.Fatal(err)
	}
	answer, err := client.CreateAnswer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SetLocalDescription(answer); err != nil {
		t.Fatal(err)
	}
	if err := peer.SetAnswer(answer); err != nil {
		t.Fatal(err)
	}
	releaseOnce.Do(func() { close(release) })
	waitSignalingIdle(t, peer)
	// Completion queues its retry asynchronously. Observe a quiet interval so
	// a room-wide retry queued just after the lock was released is detected.
	quietUntil := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(quietUntil) {
		if other.negotiationPending.Load() {
			t.Fatal("retry woke an unrelated peer after the signaling burst")
		}
		time.Sleep(time.Millisecond)
	}
}

func waitSignalingIdle(t *testing.T, peer *Peer) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if peer.signalingMu.TryLock() {
			idle := !peer.negotiationPending.Load()
			peer.signalingMu.Unlock()
			// A request can arrive while this observation holds the worker's
			// admission mutex. Its TryLock then fails behind the test rather
			// than a signaling task. Release that artificial obstruction with
			// the same peer-only completion handoff; never wake the room.
			if peer.PC.SignalingState() == webrtc.SignalingStateStable && peer.negotiationPending.Swap(false) {
				peer.room.requestPeerSignal(peer)
				idle = false
			}
			if idle {
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("peer signaling did not settle after burst")
}
