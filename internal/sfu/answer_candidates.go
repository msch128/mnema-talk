package sfu

import (
	"errors"
	"strconv"
	"strings"

	"github.com/pion/sdp/v3"
)

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
// The SSRC lines are vetted on the same parsed form (vetAnswerSSRCs), before
// any candidate is counted.
//
// It also returns the keys it newly counted, for forgetAnswerCandidates when
// Pion then refuses the answer.
func (p *Peer) vetAnswerCandidates(raw string) (string, []string, error) {
	var desc sdp.SessionDescription
	if err := desc.Unmarshal([]byte(raw)); err != nil {
		return "", nil, err
	}
	if err := vetAnswerSSRCs(&desc); err != nil {
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

// errAnswerSSRCs refuses an answer whose SSRC lines do not describe one stream
// per media section.
var errAnswerSSRCs = errors.New("sfu: the answer's SSRC lines do not describe one stream per media section")

// vetAnswerSSRCs checks the SSRC lines of an answer before Pion reads them.
// Every media section the SFU offers carries one stream: a microphone, one
// screen layer, one camera layer (the layers are sections of their own). So a
// section of the answer may name one SSRC, and next to it only the SSRCs an
// a=ssrc-group line ties to it as its retransmission (FID) or its error
// correction (FEC-FR). That is what browsers write: one SSRC, or one pair per
// group, also on a section they only receive on (Firefox).
//
// A section that names two streams, a group that is not one of those pairs,
// or an SSRC that appears in two sections is refused whole: Pion would start
// a receiver per stream it reads there, and the SFU has one per section. A
// refused answer leaves the offer outstanding like any other answer that
// cannot be applied.
func vetAnswerSSRCs(desc *sdp.SessionDescription) error {
	seen := map[uint32]struct{}{}
	for _, media := range desc.MediaDescriptions {
		ids, ok := sectionSSRCs(media.Attributes)
		if !ok {
			return errAnswerSSRCs
		}
		for id := range ids {
			if _, dup := seen[id]; dup {
				return errAnswerSSRCs
			}
			seen[id] = struct{}{}
		}
	}
	return nil
}

// sectionSSRCs returns the SSRCs one media section names, and whether they
// are one stream: at most one of them is not the second member of a group.
func sectionSSRCs(attrs []sdp.Attribute) (map[uint32]struct{}, bool) {
	ids := map[uint32]struct{}{}
	repairs := map[uint32]struct{}{}
	for _, a := range attrs {
		switch a.Key {
		case "ssrc":
			id, _, _ := strings.Cut(a.Value, " ")
			ssrc, ok := parseSSRC(id)
			if !ok {
				return nil, false
			}
			ids[ssrc] = struct{}{}
		case "ssrc-group":
			primary, repair, ok := ssrcPair(a.Value)
			if !ok {
				return nil, false
			}
			ids[primary] = struct{}{}
			ids[repair] = struct{}{}
			repairs[repair] = struct{}{}
		}
	}
	primaries := 0
	for id := range ids {
		if _, repair := repairs[id]; !repair {
			primaries++
		}
	}
	// No primary at all means the groups name each other in a circle.
	return ids, primaries == 1 || len(ids) == 0
}

// ssrcPair reads "FID a b" or "FEC-FR a b": a stream and the SSRC that
// repairs it.
func ssrcPair(value string) (primary, repair uint32, ok bool) {
	f := strings.Fields(value)
	if len(f) != 3 || (f[0] != "FID" && f[0] != "FEC-FR") {
		return 0, 0, false
	}
	primary, okA := parseSSRC(f[1])
	repair, okB := parseSSRC(f[2])
	return primary, repair, okA && okB && primary != repair
}

func parseSSRC(s string) (uint32, bool) {
	n, err := strconv.ParseUint(s, 10, 32)
	return uint32(n), err == nil
}
