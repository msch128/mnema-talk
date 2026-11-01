package sfu

import "sync/atomic"

// Pion keeps the last packets of every forwarded video track to answer NACKs,
// and its history is a fixed number of 1.5 KB buffers per subscriber and
// track, sized when the connection joins: 1024 packets for an early joiner
// stay 1024 however full the server gets, which for 100 members watching 10
// cameras is about a gigabyte. The responder here (nack_responder.go) keeps
// the history by bytes instead and re-sizes it from the load as it is now
// (nack_history.go). A short history means a late retransmission request goes
// unanswered and the receiver shows an artifact until the next keyframe.

// peerLoad counts the media connections of one SFU across all rooms.
type peerLoad struct {
	peers atomic.Int64
}

func (l *peerLoad) add(delta int64) {
	if l != nil {
		l.peers.Add(delta)
	}
}

// count is the number of connections now; an SFU without a counter has none.
func (l *peerLoad) count() int64 {
	if l == nil {
		return 0
	}
	return l.peers.Load()
}
