package sfu

import (
	"testing"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

func TestPeerLoadFollowsJoinsRejoinsAndLeaves(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()
	alice, bob := uuid.New(), uuid.New()
	noop := func(webrtc.SessionDescription) {}
	noICE := func(*webrtc.ICECandidateInit) {}

	_, first, err := s.Join(room, alice, noop, noICE)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Join(room, bob, noop, noICE); err != nil {
		t.Fatal(err)
	}
	_, again, err := s.Join(room, alice, noop, noICE)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.load.peers.Load(); got != 2 {
		t.Fatalf("load after two members and a rejoin = %d, want 2", got)
	}
	s.RemovePeer(room, first) // the replaced connection no longer counts
	if got := s.load.peers.Load(); got != 2 {
		t.Fatalf("load after removing a replaced peer = %d, want 2", got)
	}
	s.RemovePeer(room, again)
	if got := s.load.peers.Load(); got != 1 {
		t.Fatalf("load after a leave = %d, want 1", got)
	}
	s.CloseRoom(room)
	if got := s.load.peers.Load(); got != 0 {
		t.Fatalf("load after closing the room = %d, want 0", got)
	}
}

func TestPeerLoadCountIgnoresAMissingCounter(t *testing.T) {
	var none *peerLoad
	if none.count() != 0 {
		t.Fatal("a nil load counted peers")
	}
	none.add(1)
}
