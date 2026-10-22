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
		want := 0
		if round == 0 {
			want = 10
		}
		if n := strings.Count(got, "a=candidate:"); n != want {
			t.Fatalf("round %d forwarded %d candidates, want %d", round, n, want)
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
	// Known ones are not handed over again, a new one is refused.
	again := mustVet(t, p, sdpHead+hostLines(1, 4)+hostLines(100, 101))
	if n := strings.Count(again, "a=candidate:"); n != 0 {
		t.Fatalf("second answer forwarded %d candidates, want none", n)
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

// Candidates sharing address and port but differing in type or related address
// are distinct to Pion, so they are distinct here: 100 of them reach Pion as
// at most 64, and one already known is never forwarded twice.
func TestCandidatesDifferingOnlyInTypeOrRelatedAddressAreCapped(t *testing.T) {
	p := &Peer{}
	var b strings.Builder
	for i := 0; i < 100; i++ {
		typ, rel := "srflx", fmt.Sprintf(" raddr 198.51.100.%d rport 9", i+1)
		if i%2 == 1 {
			typ, rel = "relay", fmt.Sprintf(" raddr 203.0.113.%d rport 9", i+1)
		}
		fmt.Fprintf(&b, "a=candidate:1 1 udp 100 192.0.2.5 50000 typ %s%s\r\n", typ, rel)
	}
	sdp := sdpHead + b.String()
	if n := strings.Count(mustVet(t, p, sdp), "a=candidate:"); n != maxRemoteCandidates {
		t.Fatalf("%d candidates reached Pion, want %d", n, maxRemoteCandidates)
	}
	if n := strings.Count(mustVet(t, p, sdp), "a=candidate:"); n != 0 {
		t.Fatalf("re-listed answer forwarded %d candidates again", n)
	}
	same := "a=candidate:1 1 udp 100 192.0.2.5 50000 typ host\r\n"
	if n := strings.Count(mustVet(t, &Peer{}, sdpHead+same+same), "a=candidate:"); n != 1 {
		t.Fatalf("a candidate listed twice in one answer reached Pion %d times", n)
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
	if n := strings.Count(got, "a=candidate:"); n != 0 {
		t.Fatalf("after a ufrag change %d candidates forwarded, want none", n)
	}
}
