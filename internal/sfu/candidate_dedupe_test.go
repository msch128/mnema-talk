package sfu

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

func hostLines(from, to int) string {
	var b strings.Builder
	for i := from; i < to; i++ {
		fmt.Fprintf(&b, "a=candidate:%d 1 udp 2130706431 198.51.100.%d 50000 typ host\r\n", i, i)
	}
	return b.String()
}

// fillBudget marks n distinct candidates as taken.
func fillBudget(p *Peer, n int) {
	p.candMu.Lock()
	defer p.candMu.Unlock()
	for i := range n {
		_ = p.takeCandidateBudget(fmt.Sprintf("filler-%d", i))
	}
}

// Browsers re-list the candidates they gathered earlier in every
// renegotiation answer. Those must not eat the budget meant for new ones.
func TestRenegotiationAnswersDoNotExhaustTheCandidateBudget(t *testing.T) {
	p := &Peer{}
	sdp := sdpHead + hostLines(1, 11)
	for round := 0; round < 20; round++ {
		got := mustVet(t, p, sdp)
		if n := strings.Count(got, "a=candidate:"); n != 10 {
			t.Fatalf("round %d kept %d of 10 candidates", round, n)
		}
	}
	if len(p.cands.seen) != 10 {
		t.Fatalf("distinct candidates = %d, want 10", len(p.cands.seen))
	}
	for i := 11; i <= maxRemoteCandidates; i++ {
		p.candMu.Lock()
		err := p.takeCandidateBudget(fmt.Sprintf("trickled-%d", i))
		p.candMu.Unlock()
		if err != nil {
			t.Fatalf("trickled candidate %d refused after 20 answers: %v", i, err)
		}
	}
}

// A candidate re-listed with another priority is the same one; another
// transport or port is not.
func TestCandidateIdentityIsFoundationComponentTransportAddressPort(t *testing.T) {
	p := &Peer{}
	sdp := sdpHead +
		"a=candidate:7 1 udp 2130706431 198.51.100.1 50000 typ host\r\n" +
		"a=candidate:7 1 udp 100 198.51.100.1 50000 typ host\r\n" +
		"a=candidate:7 1 tcp 100 198.51.100.1 50000 typ host tcptype passive\r\n" +
		"a=candidate:7 1 udp 100 198.51.100.1 50001 typ host\r\n"
	mustVet(t, p, sdp)
	if len(p.cands.seen) != 3 {
		t.Fatalf("distinct = %d, want 3 (udp/50000, tcp/50000, udp/50001)", len(p.cands.seen))
	}
}

func TestDistinctCandidatesAreStillCapped(t *testing.T) {
	p := &Peer{}
	got := mustVet(t, p, sdpHead+hostLines(1, maxRemoteCandidates+2))
	if n := strings.Count(got, "a=candidate:"); n != maxRemoteCandidates {
		t.Fatalf("%d distinct candidates passed, want %d", n, maxRemoteCandidates)
	}
	// Already counted ones still pass, a new one does not.
	again := mustVet(t, p, sdpHead+hostLines(1, 4)+hostLines(100, 101))
	if n := strings.Count(again, "a=candidate:"); n != 3 {
		t.Fatalf("re-listed answer kept %d, want the 3 known ones", n)
	}
}

// Trickled and answer candidates share one set.
func TestTrickleAndAnswerShareTheSet(t *testing.T) {
	s := newTestSFU(t)
	_, peer, err := s.Join(uuid.New(), uuid.New(), func(webrtc.SessionDescription) {}, nil)
	if err != nil {
		t.Fatal(err)
	}
	trickled := webrtc.ICECandidateInit{Candidate: "candidate:1 1 udp 2130706431 198.51.100.1 50000 typ host"}
	if err := peer.AddRemoteCandidate(trickled); err != nil {
		t.Fatal(err)
	}
	mustVet(t, peer, sdpHead+hostLines(1, 2))
	if len(peer.cands.seen) != 1 {
		t.Fatalf("distinct = %d, want 1", len(peer.cands.seen))
	}
	if err := peer.AddRemoteCandidate(trickled); err != nil {
		t.Fatalf("repeated trickle: %v", err)
	}
}

// A new ice-ufrag in an answer does not reset the set: the server is the only
// offerer and never restarts ICE, so a changing ufrag is a client trying to
// renew its budget.
func TestChangedUfragDoesNotResetTheBudget(t *testing.T) {
	p := &Peer{}
	withUfrag := func(u, lines string) string {
		return sdpHead + "a=ice-ufrag:" + u + "\r\na=ice-pwd:aaaaaaaaaaaaaaaaaaaaaaaa\r\n" + lines
	}
	mustVet(t, p, withUfrag("abcd", hostLines(1, maxRemoteCandidates+1)))
	got := mustVet(t, p, withUfrag("wxyz", hostLines(1, 3)+hostLines(200, 201)))
	if n := strings.Count(got, "a=candidate:"); n != 2 {
		t.Fatalf("after a ufrag change %d candidates kept, want only the 2 known", n)
	}
}
