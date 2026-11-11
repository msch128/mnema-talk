package sfu

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/msch128/mnema-talk/internal/safego"
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
	// maxRemoteCandidates bounds the distinct candidates one peer may have
	// applied over its lifetime. Every one costs pairs and STUN probes toward an
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
	// seen holds the distinct client candidates taken so far, by
	// candidateKey. Its size is what maxRemoteCandidates bounds; a candidate
	// listed again (every renegotiation answer re-lists the ones the browser
	// gathered before) costs nothing.
	seen map[string]struct{}

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
	apply, err := p.admitRemoteCandidate(c)
	if !apply {
		return err
	}
	err = p.PC.AddICECandidate(c)
	if err != nil {
		safego.Locked(&p.candMu, func() { p.candidateDropped(err) })
	}
	return err
}

// admitRemoteCandidate vets c and charges it to the budget. apply says that
// Pion gets it now; otherwise it was refused (err), is known already, or is
// held until the answer.
func (p *Peer) admitRemoteCandidate(c webrtc.ICECandidateInit) (apply bool, err error) {
	p.candMu.Lock()
	defer p.candMu.Unlock()
	key, err := vetCandidateKey(c.Candidate, p.allowLoopbackCands)
	if err != nil {
		p.candidateDropped(err)
		return false, err
	}
	if p.candidateKnown(key) {
		// Already handed to Pion (or queued for it): forwarding it again
		// would be a second probe target the budget never counted.
		return false, nil
	}
	if p.PC.RemoteDescription() == nil {
		if len(p.cands.early) >= maxHeldCandidates {
			p.candidateDropped(errTooManyCandidates)
			return false, errTooManyCandidates
		}
		if err := p.takeCandidateBudget(key); err != nil {
			return false, err
		}
		p.cands.early = append(p.cands.early, c)
		return false, nil
	}
	if err := p.takeCandidateBudget(key); err != nil {
		return false, err
	}
	return true, nil
}

// applyEarlyCandidates adds what arrived before the answer was applied.
func (p *Peer) applyEarlyCandidates() {
	p.candMu.Lock()
	early := p.cands.early
	p.cands.early = nil
	p.candMu.Unlock()
	var dropped []error
	for _, c := range early {
		if err := p.PC.AddICECandidate(c); err != nil {
			dropped = append(dropped, err)
		}
	}
	if len(dropped) > 0 {
		safego.Locked(&p.candMu, func() {
			for _, err := range dropped {
				p.candidateDropped(err)
			}
		})
	}
}

// candidateDropped logs a rejected remote candidate. The caller holds candMu.
func (p *Peer) candidateDropped(err error) {
	now := time.Now()
	if now.Sub(p.cands.warnWindow) >= candidateWarnWindow {
		suppressed := p.cands.warnSuppressed
		p.cands.warnWindow, p.cands.warnCount, p.cands.warnSuppressed = now, 0, 0
		if suppressed > 0 {
			p.logger().Warn("sfu more remote candidates dropped", "user", p.ID, "count", suppressed)
		}
	}
	if p.cands.warnCount >= candidateWarnBurst {
		p.cands.warnSuppressed++
		return
	}
	p.cands.warnCount++
	p.logger().Warn("sfu remote candidate dropped", "user", p.ID, "reason", candidateDropReason(err))
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

func isPrintableASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

func vetCandidateAddresses(parsed ice.Candidate, allowLoopback bool) error {
	if !routableCandidateAddress(parsed.Address(), allowLoopback) {
		return errCandidateAddress
	}
	rel := parsed.RelatedAddress()
	if rel != nil && rel.Address != "" && !unspecifiedAddress(rel.Address) {
		if !routableCandidateAddress(rel.Address, allowLoopback) {
			return errCandidateAddress
		}
	}
	return nil
}

// vetRemoteCandidate refuses candidates that would make the server probe
// addresses a member has no business pointing it at: mDNS names (each costs
// a multicast query and a goroutine), names in general, and unspecified,
// multicast, link-local and (unless the server itself runs on loopback)
// loopback addresses. Private ranges stay: LAN deployments need them. An
// empty candidate (end of candidates) passes.
func vetRemoteCandidate(candidate string, allowLoopback bool) error {
	_, err := vetCandidateKey(candidate, allowLoopback)
	return err
}

// vetCandidateKey is vetRemoteCandidate that also returns the candidate's
// identity (see candidateKey); an empty candidate has none.
func vetCandidateKey(candidate string, allowLoopback bool) (string, error) {
	if candidate == "" {
		return "", nil
	}
	if len(candidate) > maxCandidateLen || !isPrintableASCII(candidate) {
		return "", errCandidateInvalid
	}
	// Pion's own parser, on the same text Pion will be given, so what is
	// vetted is exactly what is applied.
	parsed, err := ice.UnmarshalCandidate(strings.TrimPrefix(candidate, "candidate:"))
	if err != nil {
		return "", errCandidateInvalid
	}
	if err := vetCandidateAddresses(parsed, allowLoopback); err != nil {
		return "", err
	}
	return candidateKey(parsed), nil
}

// candidateKey names a candidate by everything Pion's candidate equality
// compares (transport address, type, related address) plus foundation,
// component and TCP type, so two candidates Pion would keep apart never share
// a key. Only the priority and extensions are left out: the same candidate
// re-listed with another priority is one candidate.
func candidateKey(c ice.Candidate) string {
	rel := ""
	if r := c.RelatedAddress(); r != nil {
		rel = fmt.Sprintf("%s/%d", strings.ToLower(r.Address), r.Port)
	}
	return fmt.Sprintf("%s/%d/%s/%s/%d/%s/%s/%s", c.Foundation(), c.Component(),
		c.NetworkType().NetworkShort(), strings.ToLower(c.Address()), c.Port(),
		c.Type(), rel, c.TCPType())
}

// candidateKnown reports whether the candidate was counted before. The
// caller holds candMu.
func (p *Peer) candidateKnown(key string) bool {
	if key == "" {
		return false
	}
	_, ok := p.cands.seen[key]
	return ok
}

// unspecifiedAddress reports 0.0.0.0 or ::. Browsers that hide the host
// address send it as the related address of a srflx candidate (raddr 0.0.0.0
// rport 0); it only says "not disclosed" and is never probed.
func unspecifiedAddress(host string) bool {
	addr, err := netip.ParseAddr(host)
	return err == nil && addr.Unmap().IsUnspecified()
}

func isDisallowedAddr(addr netip.Addr) bool {
	switch {
	case addr.IsUnspecified(), addr.IsMulticast():
		return true
	case addr.IsLinkLocalUnicast(), addr.IsLinkLocalMulticast():
		return true
	default:
		return false
	}
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
	if isDisallowedAddr(addr) {
		return false
	}
	return allowLoopback || !addr.IsLoopback()
}

// takeCandidateBudget counts one distinct accepted candidate; one already
// counted (or the empty end-of-candidates marker) is free. The caller holds
// candMu.
//
// The set is never reset, not even when the remote ufrag changes. The server
// is the only offerer and never restarts ICE, so a peer's remote ufrag cannot
// change legitimately: a client that wants a fresh connection joins again and
// gets a fresh Peer. A reset on a client-asserted ufrag would let a hostile
// client renew the budget at will and defeat the cap.
func (p *Peer) takeCandidateBudget(key string) error {
	if key == "" {
		return nil
	}
	if _, ok := p.cands.seen[key]; ok {
		return nil
	}
	if len(p.cands.seen) >= maxRemoteCandidates {
		p.candidateDropped(errCandidateBudget)
		return errCandidateBudget
	}
	if p.cands.seen == nil {
		p.cands.seen = make(map[string]struct{})
	}
	p.cands.seen[key] = struct{}{}
	return nil
}
