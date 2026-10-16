package sfu

import (
	"testing"

	"github.com/google/uuid"
	"github.com/pion/interceptor/pkg/nack"
	"github.com/pion/webrtc/v4"
)

func TestNackHistoryShrinksWithLoad(t *testing.T) {
	for _, tc := range []struct {
		peers int64
		want  uint16
	}{
		{0, 1024}, {1, 1024}, {16, 1024}, {17, 512}, {32, 512}, {33, 256},
		{64, 256}, {65, 128}, {100, 128}, {1000, 128},
	} {
		if got := nackHistory(tc.peers); got != tc.want {
			t.Errorf("nackHistory(%d) = %d, want %d", tc.peers, got, tc.want)
		}
	}
}

func TestNackResponderOptionIsAValidSize(t *testing.T) {
	load := &peerLoad{}
	for _, peers := range []int64{0, 20, 40, 500} {
		load.peers.Store(peers)
		factory, err := nack.NewResponderInterceptor(load.responderOption())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := factory.NewInterceptor(""); err != nil {
			t.Fatalf("%d peers: %v", peers, err)
		}
	}
	var none *peerLoad
	none.add(1) // an SFU built without a load counter ignores it
}

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
