package sfu

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/pion/webrtc/v4"
)

// A call that stays "connecting" leaves no trace in the log by itself: ICE
// never reaches "connected" or "failed" within the time anyone looks, so
// neither of the transitions logICEState records happens. watchConnecting
// fires once, offerTimeout after the peer was attached, and when the peer is
// still current and not connected it logs what each layer of the connection
// looks like (see connectingReport), so the next such call is explained by the
// server log alone. Addresses are left out, as in every other warning about a
// member's candidates.
func (r *Room) watchConnecting(peer *Peer) {
	if r.offerTimeout <= 0 {
		return
	}
	time.AfterFunc(r.offerTimeout, func() {
		r.mu.RLock()
		current := r.peers[peer.ID] == peer
		r.mu.RUnlock()
		if !current {
			return
		}
		switch peer.PC.ConnectionState() {
		case webrtc.PeerConnectionStateConnected, webrtc.PeerConnectionStateClosed:
			return
		}
		slog.Warn("sfu peer still connecting", peer.connectingReport()...)
	})
}

// connectingReport describes the peer's connection layer by layer: the states
// Pion reports, how many candidates the client gave and how many still wait
// for its answer, and for each candidate pair how far its checks got
// (requests sent/received, responses received/sent).
func (p *Peer) connectingReport() []any {
	pc := p.PC
	p.candMu.Lock()
	early, seen := len(p.cands.early), len(p.cands.seen)
	p.candMu.Unlock()
	attrs := []any{
		"user", p.ID,
		"connection", pc.ConnectionState().String(),
		"ice", pc.ICEConnectionState().String(),
		"gathering", pc.ICEGatheringState().String(),
		"signaling", pc.SignalingState().String(),
		"answered", pc.RemoteDescription() != nil,
		"client_candidates", seen,
		"client_candidates_unapplied", early,
	}
	report := pc.GetStats()
	kinds := map[string]string{}
	for _, s := range report {
		if c, ok := s.(webrtc.ICECandidateStats); ok {
			kinds[c.ID] = c.Protocol + "/" + c.CandidateType.String()
		}
	}
	var pairs, transports []string
	for _, s := range report {
		switch v := s.(type) {
		case webrtc.ICECandidatePairStats:
			pairs = append(pairs, fmt.Sprintf("%s>%s %s req=%d/%d resp=%d/%d nominated=%t",
				kinds[v.LocalCandidateID], kinds[v.RemoteCandidateID], v.State,
				v.RequestsSent, v.RequestsReceived, v.ResponsesReceived, v.ResponsesSent, v.Nominated))
		case webrtc.TransportStats:
			transports = append(transports, fmt.Sprintf("ice=%s role=%s dtls=%s", v.ICEState, v.ICERole, v.DTLSState))
		}
	}
	return append(attrs, "pairs", strings.Join(pairs, "; "), "transport", strings.Join(transports, "; "))
}
