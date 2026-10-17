package sfu

import (
	"errors"
	"log/slog"
	"net/netip"
	"strings"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"
)

// Trickle ICE crosses two independent orderings, and both used to lose
// candidates silently:
//
//   - The server's own candidates are gathered as soon as it sets its offer,
//     concurrently with handing that offer to the signalling channel, so a
//     candidate could reach the client before the offer. A browser rejects
//     addIceCandidate without a remote description. They are now held back
//     until the offer has been handed over.
//   - A client's candidates can reach the server before its answer is applied
//     (they are produced right after the client sets its local description,
//     while the answer travels the same channel but is applied on the server
//     only after it returns from SetRemoteDescription). Pion refuses them
//     without a remote description. They are now kept and applied once the
//     answer is.

const (
	// maxHeldCandidates bounds the candidates kept in either direction. A
	// peer gathers a handful per interface; more than this is not a client
	// worth keeping them for.
	maxHeldCandidates = 64
	// maxRemoteCandidates bounds the candidates one peer may have applied
	// over its lifetime. Every one costs pairs and STUN probes toward an
	// address the member chose, so a flood is a way to point the server at
	// others. A browser gathers a handful per interface and per STUN server.
	maxRemoteCandidates = 64
	// maxCandidateLen bounds one candidate line; real ones are under 200.
	maxCandidateLen = 512
	// candidateWarnWindow and candidateWarnBurst rate-limit the log of
	// dropped candidates per peer.
	candidateWarnWindow = 30 * time.Second
	candidateWarnBurst  = 3
)

var (
	errTooManyCandidates = errors.New("too many candidates before the remote description")
	errCandidateBudget   = errors.New("candidate budget exhausted")
	errCandidateAddress  = errors.New("candidate address refused")
	errCandidateInvalid  = errors.New("candidate invalid")
)

// candidateState is the per-peer bookkeeping of the two queues above.
type candidateState struct {
	// offered turns true once an offer was handed to SendOffer; the server's
	// candidates flow after that.
	offered bool
	// out are the server's candidates gathered before that.
	out []webrtc.ICECandidateInit
	// early are the client's candidates received before its answer was applied.
	early []webrtc.ICECandidateInit
	// accepted counts the client's candidates taken so far (see
	// maxRemoteCandidates).
	accepted int

	warnWindow     time.Time
	warnCount      int
	warnSuppressed int
}

// emitCandidate hands one of the server's candidates to the client, or holds
// it until the offer has been handed over.
func (p *Peer) emitCandidate(c webrtc.ICECandidateInit) {
	p.candMu.Lock()
	defer p.candMu.Unlock()
	if !p.cands.offered {
		if len(p.cands.out) < maxHeldCandidates {
			p.cands.out = append(p.cands.out, c)
		}
		return
	}
	if p.SendICE != nil {
		// Under the lock so a candidate gathered now cannot overtake the
		// flush of the earlier ones.
		p.SendICE(&c)
	}
}

// offerDelivered releases the candidates held back while the first offer was
// being handed over. Later offers change nothing.
func (p *Peer) offerDelivered() {
	p.candMu.Lock()
	defer p.candMu.Unlock()
	if p.cands.offered {
		return
	}
	p.cands.offered = true
	held := p.cands.out
	p.cands.out = nil
	if p.SendICE == nil {
		return
	}
	for i := range held {
		p.SendICE(&held[i])
	}
}

// AddRemoteCandidate applies a candidate of the client, or keeps it until
// the client's answer is applied. A candidate Pion rejects is logged with its
// reason (rate-limited, without the address) and returned.
func (p *Peer) AddRemoteCandidate(c webrtc.ICECandidateInit) error {
	p.candMu.Lock()
	if err := vetRemoteCandidate(c.Candidate, p.allowLoopbackCands); err != nil {
		p.candidateDropped(err)
		p.candMu.Unlock()
		return err
	}
	if p.PC.RemoteDescription() == nil {
		defer p.candMu.Unlock()
		if len(p.cands.early) >= maxHeldCandidates {
			p.candidateDropped(errTooManyCandidates)
			return errTooManyCandidates
		}
		// The held ones are counted and the bound above is not above the
		// budget, so there is always room.
		p.cands.accepted++
		p.cands.early = append(p.cands.early, c)
		return nil
	}
	if err := p.takeCandidateBudget(); err != nil {
		p.candMu.Unlock()
		return err
	}
	p.candMu.Unlock()
	err := p.PC.AddICECandidate(c)
	if err != nil {
		p.candMu.Lock()
		p.candidateDropped(err)
		p.candMu.Unlock()
	}
	return err
}

// applyEarlyCandidates adds what arrived before the answer was applied.
func (p *Peer) applyEarlyCandidates() {
	p.candMu.Lock()
	early := p.cands.early
	p.cands.early = nil
	p.candMu.Unlock()
	for _, c := range early {
		if err := p.PC.AddICECandidate(c); err != nil {
			p.candMu.Lock()
			p.candidateDropped(err)
			p.candMu.Unlock()
		}
	}
}

// candidateDropped logs a rejected remote candidate. The caller holds candMu.
func (p *Peer) candidateDropped(err error) {
	now := time.Now()
	if now.Sub(p.cands.warnWindow) >= candidateWarnWindow {
		suppressed := p.cands.warnSuppressed
		p.cands.warnWindow, p.cands.warnCount, p.cands.warnSuppressed = now, 0, 0
		if suppressed > 0 {
			slog.Warn("sfu more remote candidates dropped", "user", p.ID, "count", suppressed)
		}
	}
	if p.cands.warnCount >= candidateWarnBurst {
		p.cands.warnSuppressed++
		return
	}
	p.cands.warnCount++
	slog.Warn("sfu remote candidate dropped", "user", p.ID, "reason", candidateDropReason(err))
}

// candidateDropReason names why Pion refused a candidate without echoing the
// candidate (it carries the client's address).
func candidateDropReason(err error) string {
	switch {
	case errors.Is(err, errTooManyCandidates):
		return "too many candidates before the answer"
	case errors.Is(err, errCandidateBudget):
		return "candidate budget exhausted"
	case errors.Is(err, errCandidateAddress):
		return "address refused"
	case errors.Is(err, webrtc.ErrNoRemoteDescription):
		return "no remote description"
	case errors.Is(err, webrtc.ErrConnectionClosed):
		return "connection closed"
	default:
		return "invalid candidate"
	}
}

// vetRemoteCandidate refuses candidates that would make the server probe
// addresses a member has no business pointing it at: mDNS names (each costs
// a multicast query and a goroutine), names in general, and unspecified,
// multicast, link-local and (unless the server itself runs on loopback)
// loopback addresses. Private ranges stay: LAN deployments need them. An
// empty candidate (end of candidates) passes.
func vetRemoteCandidate(candidate string, allowLoopback bool) error {
	if candidate == "" {
		return nil
	}
	if len(candidate) > maxCandidateLen {
		return errCandidateInvalid
	}
	// Plain printable ASCII and single spaces only: Pion splits on its own
	// idea of a separator, and anything else (tabs, NBSP, NEL, controls)
	// could make the two readings differ.
	for i := 0; i < len(candidate); i++ {
		if b := candidate[i]; b < 0x20 || b > 0x7e {
			return errCandidateInvalid
		}
	}
	// Pion's own parser, on the same text Pion will be given, so what is
	// vetted is exactly what is applied.
	parsed, err := ice.UnmarshalCandidate(strings.TrimPrefix(candidate, "candidate:"))
	if err != nil {
		return errCandidateInvalid
	}
	if !routableCandidateAddress(parsed.Address(), allowLoopback) {
		return errCandidateAddress
	}
	if rel := parsed.RelatedAddress(); rel != nil && rel.Address != "" && !routableCandidateAddress(rel.Address, allowLoopback) {
		return errCandidateAddress
	}
	return nil
}

// routableCandidateAddress accepts an IP literal that is not unspecified,
// multicast, link-local or (unless allowLoopback) loopback. A name,
// including *.local, is refused.
func routableCandidateAddress(host string, allowLoopback bool) bool {
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	if addr.IsUnspecified() || addr.IsMulticast() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() {
		return false
	}
	return allowLoopback || !addr.IsLoopback()
}

// takeCandidateBudget counts one accepted candidate. The caller holds candMu.
func (p *Peer) takeCandidateBudget() error {
	if p.cands.accepted >= maxRemoteCandidates {
		p.candidateDropped(errCandidateBudget)
		return errCandidateBudget
	}
	p.cands.accepted++
	return nil
}
