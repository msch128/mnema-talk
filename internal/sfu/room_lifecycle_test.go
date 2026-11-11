package sfu

import (
	"testing"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

func TestJoinFailureDropsEmptyRoom(t *testing.T) {
	// TURN URLs without a credential are rejected by Pion's public
	// configuration validation when a connection is created.
	s, err := NewSFU(0, 0, nil, []string{"turn:127.0.0.1:3478"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	roomID := uuid.New()
	if _, _, err := s.Join(roomID, uuid.New(), "", nil, nil); err == nil {
		t.Fatal("TURN configuration without credentials accepted")
	}
	if s.Room(roomID) != nil {
		t.Fatal("failed first join left an empty room")
	}
}

func TestRemovePeerWithoutRoomStillClosesTransport(t *testing.T) {
	s := newTestSFU(t)
	roomID := uuid.New()
	_, peer, err := s.Join(roomID, uuid.New(), "", nil, nil)
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
