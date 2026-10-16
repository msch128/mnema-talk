package sfu

import (
	"sync/atomic"

	"github.com/pion/interceptor/pkg/nack"
)

// Pion keeps the last packets of every forwarded track to answer NACKs, in
// buffers of about 1.5 KB per packet. Memory therefore grows with members x
// forwarded tracks: 1024 packets per video track for 100 members watching 10
// cameras would be about 1.5 GB. The history shrinks as the server fills up.
// A short history means a late retransmission request goes unanswered and
// the receiver shows an artifact until the next keyframe.
const (
	nackHistoryMax = 1024
	nackHistoryMin = 128
)

// peerLoad counts the media connections of one SFU across all rooms.
type peerLoad struct {
	peers atomic.Int64
}

func (l *peerLoad) add(delta int64) {
	if l != nil {
		l.peers.Add(delta)
	}
}

// nackHistory is the retransmission history, in packets, for a connection
// that becomes the peers-th one: 1024 up to 16 connections, halving each
// time the count doubles, never below 128.
func nackHistory(peers int64) uint16 {
	size := uint16(nackHistoryMax)
	for limit := int64(16); peers > limit && size > nackHistoryMin; limit *= 2 {
		size /= 2
	}
	return size
}

// responderOption sizes each new connection's history from the current load.
// A connection keeps its size; later ones adapt.
func (l *peerLoad) responderOption() nack.ResponderOption {
	return func(r *nack.ResponderInterceptor) error {
		return nack.ResponderSize(nackHistory(l.peers.Load() + 1))(r)
	}
}
