package sfu

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

// lostSignal registers OnLost on peer and returns a channel that is closed
// when the SFU reports the peer lost.
func lostSignal(peer *Peer) chan struct{} {
	lost := make(chan struct{})
	peer.OnLost(func() { close(lost) })
	return lost
}

func waitLost(t *testing.T, lost chan struct{}, what string) {
	t.Helper()
	select {
	case <-lost:
	case <-time.After(10 * time.Second):
		t.Fatalf("%s: the owner was not told", what)
	}
}

// A client that never answers must not stay a silent member: after the last
// resend the peer is removed and its owner told.
func TestUnansweredOfferDropsThePeerAndTellsTheOwner(t *testing.T) {
	peer, _ := joinWithOffers(t, 20*time.Millisecond)
	lost := lostSignal(peer)
	waitLost(t, lost, "unanswered offer")
	if peer.room.GetPeer(peer.ID) != nil {
		t.Fatal("the lost peer is still in the room")
	}
}

// OnLost registered after the peer was lost still fires.
func TestOnLostAfterTheFactFires(t *testing.T) {
	peer, _ := joinWithOffers(t, 20*time.Millisecond)
	waitSFUCondition(t, "peer removed", func() bool { return peer.room.GetPeer(peer.ID) == nil })
	waitLost(t, lostSignal(peer), "late registration")
}

// joinViewerAndAnswer joins a viewer, answers its first offer and returns the
// viewer's peer and its offers.
func joinViewerAndAnswer(t *testing.T, s *SFU, room uuid.UUID) (*Peer, chan webrtc.SessionDescription) {
	t.Helper()
	offers := make(chan webrtc.SessionDescription, 8)
	_, peer, err := s.Join(room, uuid.New(), func(o webrtc.SessionDescription) { offers <- o }, nil)
	if err != nil {
		t.Fatal(err)
	}
	client, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := peer.SetAnswer(answerOffer(t, client, nextOffer(t, offers, "first offer"))); err != nil {
		t.Fatal(err)
	}
	// Let the answered peer settle before the test injects a fault.
	waitSFUCondition(t, "viewer stable", func() bool { return peer.PC.SignalingState() == webrtc.SignalingStateStable })
	return peer, offers
}

// One failed offer must not leave the viewer's new sender un-negotiated: the
// offer is built again without another track change.
func TestFailedOfferIsRetried(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()
	viewer, offers := joinViewerAndAnswer(t, s, room)

	var calls atomic.Int32
	viewer.signalingMu.Lock()
	viewer.offerFault = func() error {
		if calls.Add(1) == 1 {
			return errors.New("injected")
		}
		return nil
	}
	viewer.signalingMu.Unlock()

	pub := newClient(t)
	pub.publishAudio("mic")
	pub.join(s, room)

	nextOffer(t, offers, "offer with the new track after a failed one")
	if calls.Load() < 2 {
		t.Fatalf("offer built %d times, want a retry", calls.Load())
	}
}

// A viewer whose offers can never be built is dropped, not left half-served.
func TestOfferThatCannotBeBuiltDropsThePeer(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()
	viewer, _ := joinViewerAndAnswer(t, s, room)
	lost := lostSignal(viewer)

	viewer.signalingMu.Lock()
	viewer.offerFault = func() error { return errors.New("injected") }
	viewer.signalingMu.Unlock()

	pub := newClient(t)
	pub.publishAudio("mic")
	pub.join(s, room)

	waitLost(t, lost, "offers that cannot be built")
	if s.Room(room).GetPeer(viewer.ID) != nil {
		t.Fatal("the peer is still in the room")
	}
}

// A signaling round that is still running when its peer is released must not
// register a layered viewer nobody would release.
func TestReleasedPeerRefusesViewerTracks(t *testing.T) {
	p := &Peer{}
	if !p.putViewerTrack("a", &viewerTrack{}) {
		t.Fatal("a live peer refused a viewer track")
	}
	p.takeViewerTrack("a")
	p.releaseViewerTracks()
	if p.putViewerTrack("b", &viewerTrack{}) {
		t.Fatal("a released peer accepted a viewer track")
	}
	if p.takeViewerTrack("b") != nil {
		t.Fatal("the refused viewer track was recorded")
	}
}
