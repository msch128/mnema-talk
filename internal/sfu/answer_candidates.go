package sfu

import "github.com/pion/sdp/v3"

// vetAnswerCandidates removes the ICE candidates an answer SDP carries that the
// trickle path would have refused (unspecified, multicast, link-local, loopback
// and name addresses, malformed or oversized lines) and every new candidate
// beyond the peer's budget (one already counted is removed, as Pion has it already). Pion adds the candidates of a remote description to its
// ICE agent as they are, so without this an answer would sidestep the vetting
// and the cap that AddRemoteCandidate applies, and make the server probe
// addresses the member chose.
//
// The SDP is parsed with the parser Pion itself uses and written back from the
// parsed form, so what is vetted is exactly what Pion would read, whatever line
// endings or stray characters the sender chose. An SDP that does not parse is
// refused. Dropped candidates are logged like trickled ones.
//
// It also returns the keys it newly counted, for forgetAnswerCandidates when
// Pion then refuses the answer.
func (p *Peer) vetAnswerCandidates(raw string) (string, []string, error) {
	var desc sdp.SessionDescription
	if err := desc.Unmarshal([]byte(raw)); err != nil {
		return "", nil, err
	}
	p.candMu.Lock()
	defer p.candMu.Unlock()
	var added []string
	desc.Attributes = p.keepVettedCandidates(desc.Attributes, &added)
	for _, media := range desc.MediaDescriptions {
		media.Attributes = p.keepVettedCandidates(media.Attributes, &added)
	}
	out, err := desc.Marshal()
	return string(out), added, err
}

// forgetAnswerCandidates takes back the candidates of an answer Pion did not
// accept: they never reached it, so a later answer or a trickled copy must
// still be able to bring them.
func (p *Peer) forgetAnswerCandidates(keys []string) {
	p.candMu.Lock()
	defer p.candMu.Unlock()
	for _, key := range keys {
		delete(p.cands.seen, key)
	}
}

// keepVettedCandidates filters candidate attributes. The caller holds candMu.
func (p *Peer) keepVettedCandidates(attrs []sdp.Attribute, added *[]string) []sdp.Attribute {
	kept := attrs[:0]
	for _, a := range attrs {
		if a.Key != "candidate" {
			kept = append(kept, a)
			continue
		}
		key, err := vetCandidateKey(a.Value, p.allowLoopbackCands)
		if err != nil {
			p.candidateDropped(err)
			continue
		}
		if p.candidateKnown(key) {
			// Pion has it from an earlier description; it is not handed over
			// again.
			continue
		}
		if p.takeCandidateBudget(key) != nil {
			continue
		}
		if key != "" {
			*added = append(*added, key)
		}
		kept = append(kept, a)
	}
	return kept
}
