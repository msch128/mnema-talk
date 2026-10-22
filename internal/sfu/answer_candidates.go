package sfu

import "github.com/pion/sdp/v3"

// vetAnswerCandidates removes the ICE candidates an answer SDP carries that the
// trickle path would have refused (unspecified, multicast, link-local, loopback
// and name addresses, malformed or oversized lines) and every candidate beyond
// the peer's budget. Pion adds the candidates of a remote description to its
// ICE agent as they are, so without this an answer would sidestep the vetting
// and the cap that AddRemoteCandidate applies, and make the server probe
// addresses the member chose.
//
// The SDP is parsed with the parser Pion itself uses and written back from the
// parsed form, so what is vetted is exactly what Pion would read, whatever line
// endings or stray characters the sender chose. An SDP that does not parse is
// refused. Dropped candidates are logged like trickled ones.
func (p *Peer) vetAnswerCandidates(raw string) (string, error) {
	var desc sdp.SessionDescription
	if err := desc.Unmarshal([]byte(raw)); err != nil {
		return "", err
	}
	p.candMu.Lock()
	defer p.candMu.Unlock()
	desc.Attributes = p.keepVettedCandidates(desc.Attributes)
	for _, media := range desc.MediaDescriptions {
		media.Attributes = p.keepVettedCandidates(media.Attributes)
	}
	out, err := desc.Marshal()
	return string(out), err
}

// keepVettedCandidates filters candidate attributes. The caller holds candMu.
func (p *Peer) keepVettedCandidates(attrs []sdp.Attribute) []sdp.Attribute {
	kept := attrs[:0]
	for _, a := range attrs {
		if a.Key != "candidate" {
			kept = append(kept, a)
			continue
		}
		if err := vetRemoteCandidate(a.Value, p.allowLoopbackCands); err != nil {
			p.candidateDropped(err)
			continue
		}
		if p.takeCandidateBudget() != nil {
			continue
		}
		kept = append(kept, a)
	}
	return kept
}
