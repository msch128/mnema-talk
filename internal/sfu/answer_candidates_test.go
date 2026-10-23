package sfu

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

func candidateLine(addr string) string {
	return fmt.Sprintf("a=candidate:1 1 udp 2130706431 %s 50000 typ host", addr)
}

const sdpHead = "v=0\r\no=- 1 1 IN IP4 0.0.0.0\r\ns=-\r\nt=0 0\r\nm=audio 9 UDP/TLS/RTP/SAVPF 111\r\nc=IN IP4 0.0.0.0\r\n"

func mustVet(t *testing.T, p *Peer, sdp string) string {
	t.Helper()
	got, err := p.vetAnswerCandidates(sdp)
	if err != nil {
		t.Fatalf("vet: %v", err)
	}
	return got
}

func TestVetAnswerCandidatesDropsWhatTrickleWouldRefuse(t *testing.T) {
	p := &Peer{}
	sdp := sdpHead + strings.Join([]string{
		candidateLine("127.0.0.1"),
		candidateLine("169.254.10.1"),
		candidateLine("0.0.0.0"),
		candidateLine("224.0.0.251"),
		"a=candidate:1 1 udp 1 printer.local 5 typ host",
		candidateLine("192.0.2.7"),
		"a=end-of-candidates", "",
	}, "\r\n")
	got := mustVet(t, p, sdp)
	for _, bad := range []string{"127.0.0.1", "169.254.10.1", "0.0.0.0 50000", "224.0.0.251", "printer.local"} {
		if strings.Contains(got, bad) {
			t.Fatalf("answer still carries %s: %q", bad, got)
		}
	}
	if !strings.Contains(got, "192.0.2.7") || !strings.Contains(got, "a=end-of-candidates") || !strings.Contains(got, "m=audio") {
		t.Fatalf("answer lost lines it should keep: %q", got)
	}
	if len(p.cands.seen) != 1 {
		t.Fatalf("budget used = %d, want 1", len(p.cands.seen))
	}
}

func TestVetAnswerCandidatesKeepsLoopbackOnLoopbackServers(t *testing.T) {
	p := &Peer{allowLoopbackCands: true}
	if got := mustVet(t, p, sdpHead+candidateLine("127.0.0.1")+"\r\n"); !strings.Contains(got, "127.0.0.1") {
		t.Fatalf("loopback candidate dropped on a loopback server: %q", got)
	}
}

func TestVetAnswerCandidatesSharesTheBudgetWithTrickle(t *testing.T) {
	p := &Peer{}
	var lines []string
	for i := 0; i < maxRemoteCandidates+20; i++ {
		lines = append(lines, candidateLine(fmt.Sprintf("192.0.2.%d", i%250+1)))
	}
	got := mustVet(t, p, sdpHead+strings.Join(lines, "\r\n")+"\r\n")
	if n := strings.Count(got, "a=candidate:"); n != maxRemoteCandidates {
		t.Fatalf("%d candidates passed, want the budget of %d", n, maxRemoteCandidates)
	}
	p.candMu.Lock()
	err := p.takeCandidateBudget("new")
	p.candMu.Unlock()
	if err == nil {
		t.Fatal("trickle still had budget after the answer used it all")
	}
}

func TestVetAnswerCandidatesFiltersSessionLevelCandidates(t *testing.T) {
	sdp := "v=0\r\no=- 1 1 IN IP4 0.0.0.0\r\ns=-\r\nt=0 0\r\n" + candidateLine("169.254.1.1") + "\r\nm=audio 9 UDP/TLS/RTP/SAVPF 111\r\n"
	if got := mustVet(t, &Peer{}, sdp); strings.Contains(got, "169.254.1.1") {
		t.Fatalf("session-level candidate kept: %q", got)
	}
}

func TestVetAnswerCandidatesRefusesUnparsableSDP(t *testing.T) {
	if _, err := (&Peer{}).vetAnswerCandidates("not an sdp"); err == nil {
		t.Fatal("garbage accepted")
	}
}

// Line terminators a splitter on LF would misread must not hide a candidate:
// whatever the parser makes of the text, no refused address survives. A
// parse error is as good as a filtered result.
func TestVetAnswerCandidatesSurviveOddLineEndings(t *testing.T) {
	bad := candidateLine("169.254.9.9")
	for name, sdp := range map[string]string{
		"leading CR":   sdpHead + "\r" + bad + "\r\n",
		"CR only":      strings.ReplaceAll(sdpHead+bad+"\r\n", "\r\n", "\r"),
		"mixed":        "v=0\no=- 1 1 IN IP4 0.0.0.0\r\ns=-\rt=0 0\r\nm=audio 9 UDP/TLS/RTP/SAVPF 111\n" + bad + "\r\n",
		"LF only":      strings.ReplaceAll(sdpHead+bad+"\r\n", "\r\n", "\n"),
		"CR before LF": sdpHead + "\r\r\n" + bad + "\r\n",
	} {
		got, err := (&Peer{}).vetAnswerCandidates(sdp)
		if err == nil && strings.Contains(got, "169.254.9.9") {
			t.Errorf("%s: refused candidate survived: %q", name, got)
		}
	}
}

// A hostile answer reaches Pion (which adds every candidate of a remote
// description to its ICE agent) only with the candidates the vetting lets
// through.
func TestSetAnswerKeepsRefusedCandidatesOutOfTheICEAgent(t *testing.T) {
	s := newTestSFU(t)
	c := newClient(t)
	offers := make(chan webrtc.SessionDescription, 4)
	_, peer, err := s.Join(uuid.New(), c.id, func(o webrtc.SessionDescription) { offers <- o }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.pc.SetRemoteDescription(<-offers); err != nil {
		t.Fatal(err)
	}
	answer, err := c.pc.CreateAnswer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.pc.SetLocalDescription(answer); err != nil {
		t.Fatal(err)
	}
	answer.SDP = strings.TrimRight(answer.SDP, "\r\n") + "\r\n" +
		candidateLine("224.0.0.251") + "\r\n" + candidateLine("169.254.7.7") + "\r\n" + candidateLine("192.0.2.9") + "\r\n"
	if err := peer.SetAnswer(answer); err != nil {
		t.Fatal(err)
	}
	applied := peer.PC.RemoteDescription().SDP
	for _, refused := range []string{"224.0.0.251", "169.254.7.7"} {
		if strings.Contains(applied, refused) {
			t.Fatalf("refused candidate %s was handed to the ICE agent", refused)
		}
	}
	if !strings.Contains(applied, "192.0.2.9") {
		t.Fatal("the acceptable candidate was dropped as well")
	}
}
