package sfu

import (
	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

// Test-only views into a room.

// GetPeer returns the user's current connection in the room.
func (r *Room) GetPeer(userID uuid.UUID) *Peer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.peers[userID]
}

// Receives reports whether viewer currently receives publisher's video of the given kind.
func (r *Room) Receives(viewer, publisher uuid.UUID, kind Source) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return viewer != publisher && r.subs[viewer].Wants(publisher, webrtc.RTPCodecTypeVideo, kind)
}
