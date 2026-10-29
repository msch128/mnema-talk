package sfu

import (
	"net/netip"
	"sync/atomic"

	"github.com/pion/webrtc/v4"
)

// ICEConnected is what the server learned when a media connection finished
// ICE: the remote end of the selected candidate pair.
type ICEConnected struct {
	// Remote is the address the other end was reached at; invalid when the
	// selected pair could not be read.
	Remote netip.Addr
	// RemoteType is the candidate type of the remote end ("host", "srflx",
	// "prflx" or "relay").
	RemoteType string
}

// iceObserver hands every successful ICE connection (voice peers and the
// connection test) to one registered callback. It is shared by the SFU and
// its rooms; a nil observer or one without a callback ignores them.
type iceObserver struct {
	fn atomic.Pointer[func(ICEConnected)]
}

// SetICEConnectedHandler registers the callback that hears of every media
// connection that completed ICE, from any member. It runs on the ICE state
// callback's goroutine and must not block.
func (s *SFU) SetICEConnectedHandler(fn func(ICEConnected)) {
	s.iceSeen.fn.Store(&fn)
}

func (o *iceObserver) connected(pair *webrtc.ICECandidatePair) {
	if o == nil {
		return
	}
	fn := o.fn.Load()
	if fn == nil || *fn == nil {
		return
	}
	var ev ICEConnected
	if pair != nil && pair.Remote != nil {
		if a, err := netip.ParseAddr(pair.Remote.Address); err == nil {
			ev.Remote = a.Unmap()
		}
		ev.RemoteType = pair.Remote.Typ.String()
	}
	(*fn)(ev)
}

// connectionPair is the selected candidate pair of a connection that carries
// tracks, or else of one that carries only a data channel.
func connectionPair(pc *webrtc.PeerConnection) (*webrtc.ICECandidatePair, error) {
	if pair, err := selectedPair(pc); err != nil || pair != nil {
		return pair, err
	}
	if sctp := pc.SCTP(); sctp != nil && sctp.Transport() != nil {
		return sctp.Transport().ICETransport().GetSelectedCandidatePair()
	}
	return nil, nil
}
