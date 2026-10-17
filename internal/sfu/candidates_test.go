package sfu

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

func captureLogs(t *testing.T) *synchronizedLogCapture {
	t.Helper()
	var buf synchronizedLogCapture
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func candidate(i int) webrtc.ICECandidateInit {
	return webrtc.ICECandidateInit{Candidate: fmt.Sprintf("candidate:%d 1 udp 2130706431 192.0.2.%d 50000 typ host", i, i)}
}

// Candidates gathered while the offer is still being handed over wait for it:
// a browser cannot add a candidate before it has the offer.
func TestServerCandidatesWaitForTheOffer(t *testing.T) {
	var sent []string
	p := &Peer{SendICE: func(c *webrtc.ICECandidateInit) { sent = append(sent, c.Candidate) }}

	p.emitCandidate(candidate(1))
	p.emitCandidate(candidate(2))
	if len(sent) != 0 {
		t.Fatalf("candidates sent before the offer: %v", sent)
	}
	p.offerDelivered()
	if len(sent) != 2 || sent[0] != candidate(1).Candidate || sent[1] != candidate(2).Candidate {
		t.Fatalf("held candidates not released in order: %v", sent)
	}
	p.emitCandidate(candidate(3))
	if len(sent) != 3 {
		t.Fatalf("candidate after the offer was held: %v", sent)
	}
	p.offerDelivered() // a later offer changes nothing
	if len(sent) != 3 {
		t.Fatalf("candidates sent again: %v", sent)
	}
}

func TestServerCandidatesHeldAreBounded(t *testing.T) {
	var sent int
	p := &Peer{SendICE: func(*webrtc.ICECandidateInit) { sent++ }}
	for i := range 3 * maxHeldCandidates {
		p.emitCandidate(candidate(i % 200))
	}
	p.offerDelivered()
	if sent != maxHeldCandidates {
		t.Fatalf("released %d, want %d", sent, maxHeldCandidates)
	}
}

func TestServerCandidatesWithoutSender(t *testing.T) {
	p := &Peer{}
	p.emitCandidate(candidate(1))
	p.offerDelivered()
	p.emitCandidate(candidate(2)) // nobody listens: neither panics nor queues
	if len(p.cands.out) != 0 {
		t.Fatalf("queue = %v", p.cands.out)
	}
}

// A real join: with a slow offer callback the gathered candidates would
// overtake the offer if they were sent as they appear.
func TestJoinSendsOfferBeforeCandidates(t *testing.T) {
	s := newTestSFU(t)
	var mu sync.Mutex
	var order []string
	candidates := make(chan struct{}, 64)
	_, peer, err := s.Join(uuid.New(), uuid.New(),
		func(webrtc.SessionDescription) {
			time.Sleep(300 * time.Millisecond) // gathering is well under way
			mu.Lock()
			order = append(order, "offer")
			mu.Unlock()
		},
		func(*webrtc.ICECandidateInit) {
			mu.Lock()
			order = append(order, "candidate")
			mu.Unlock()
			select {
			case candidates <- struct{}{}:
			default:
			}
		})
	if err != nil {
		t.Fatal(err)
	}
	_ = peer
	select {
	case <-candidates:
	case <-time.After(10 * time.Second):
		t.Fatal("no candidate was sent")
	}
	mu.Lock()
	defer mu.Unlock()
	if order[0] != "offer" {
		t.Fatalf("order = %v, want the offer first", order)
	}
}

// A client's candidates can arrive before its answer is applied. They are
// kept and then reach the ICE agent, which the statistics show.
func TestEarlyClientCandidatesAreAppliedWithTheAnswer(t *testing.T) {
	s := newTestSFU(t)
	c := newClient(t)
	offers := make(chan webrtc.SessionDescription, 4)
	_, peer, err := s.Join(uuid.New(), c.id, func(o webrtc.SessionDescription) { offers <- o }, nil)
	if err != nil {
		t.Fatal(err)
	}
	var offer webrtc.SessionDescription
	select {
	case offer = <-offers:
	case <-time.After(10 * time.Second):
		t.Fatal("no offer")
	}
	if err := c.pc.SetRemoteDescription(offer); err != nil {
		t.Fatal(err)
	}
	answer, err := c.pc.CreateAnswer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gathered := make(chan webrtc.ICECandidateInit, 16)
	c.pc.OnICECandidate(func(ic *webrtc.ICECandidate) {
		if ic != nil {
			gathered <- ic.ToJSON()
		}
	})
	if err := c.pc.SetLocalDescription(answer); err != nil {
		t.Fatal(err)
	}
	var cand webrtc.ICECandidateInit
	select {
	case cand = <-gathered:
	case <-time.After(10 * time.Second):
		t.Fatal("client gathered no candidate")
	}

	// The candidate wins the race against the answer.
	if err := peer.AddRemoteCandidate(cand); err != nil {
		t.Fatalf("early candidate rejected: %v", err)
	}
	peer.candMu.Lock()
	held := len(peer.cands.early)
	peer.candMu.Unlock()
	if held != 1 {
		t.Fatalf("held %d early candidates, want 1", held)
	}

	if err := peer.SetAnswer(answer); err != nil {
		t.Fatal(err)
	}
	peer.candMu.Lock()
	held = len(peer.cands.early)
	peer.candMu.Unlock()
	if held != 0 {
		t.Fatalf("%d early candidates still held after the answer", held)
	}
	remote := 0
	for _, st := range peer.PC.GetStats() {
		if cs, ok := st.(webrtc.ICECandidateStats); ok && cs.Type == webrtc.StatsTypeRemoteCandidate {
			remote++
		}
	}
	if remote == 0 {
		t.Fatal("the ICE agent never received the early candidate")
	}
}

func TestEarlyClientCandidatesAreBounded(t *testing.T) {
	logs := captureLogs(t)
	s := newTestSFU(t)
	_, peer, err := s.Join(uuid.New(), uuid.New(), func(webrtc.SessionDescription) {}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := range maxHeldCandidates {
		if err := peer.AddRemoteCandidate(candidate(i % 200)); err != nil {
			t.Fatalf("candidate %d: %v", i, err)
		}
	}
	if err := peer.AddRemoteCandidate(candidate(1)); err == nil {
		t.Fatal("candidate over the bound accepted")
	}
	if got := logs.String(); !strings.Contains(got, "reason=\"too many candidates before the answer\"") {
		t.Fatalf("overflow not logged with its reason: %s", got)
	}
}

// A rejected candidate is logged at WARN with a reason, without its address,
// and a flood of them does not flood the log.
func TestRejectedRemoteCandidatesAreLoggedRateLimited(t *testing.T) {
	logs := captureLogs(t)
	s := newTestSFU(t)
	c := newClient(t)
	offers := make(chan webrtc.SessionDescription, 4)
	_, peer, err := s.Join(uuid.New(), c.id, func(o webrtc.SessionDescription) { offers <- o }, nil)
	if err != nil {
		t.Fatal(err)
	}
	offer := <-offers
	if err := c.pc.SetRemoteDescription(offer); err != nil {
		t.Fatal(err)
	}
	answer, err := c.pc.CreateAnswer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.pc.SetLocalDescription(answer); err != nil {
		t.Fatal(err)
	}
	if err := peer.SetAnswer(answer); err != nil {
		t.Fatal(err)
	}

	bad := webrtc.ICECandidateInit{Candidate: "candidate:1 1 udp notanumber 198.51.100.7 50000 typ host"}
	for range 10 {
		if err := peer.AddRemoteCandidate(bad); err == nil {
			t.Fatal("garbage candidate accepted")
		}
	}
	out := logs.String()
	if n := strings.Count(out, "level=WARN msg=\"sfu remote candidate dropped\""); n != candidateWarnBurst {
		t.Fatalf("logged %d times, want %d: %s", n, candidateWarnBurst, out)
	}
	if !strings.Contains(out, "reason=\"invalid candidate\"") {
		t.Fatalf("reason missing: %s", out)
	}
	if strings.Contains(out, "198.51.100.7") {
		t.Fatalf("candidate address in the log: %s", out)
	}

	// A new window reports what was suppressed.
	peer.candMu.Lock()
	peer.cands.warnWindow = time.Now().Add(-2 * candidateWarnWindow)
	peer.candMu.Unlock()
	_ = peer.AddRemoteCandidate(bad)
	if !strings.Contains(logs.String(), "sfu more remote candidates dropped") {
		t.Fatalf("suppressed count not reported: %s", logs.String())
	}
}

func TestEarlyCandidateRejectedWhenApplied(t *testing.T) {
	logs := captureLogs(t)
	s := newTestSFU(t)
	c := newClient(t)
	offers := make(chan webrtc.SessionDescription, 4)
	_, peer, err := s.Join(uuid.New(), c.id, func(o webrtc.SessionDescription) { offers <- o }, nil)
	if err != nil {
		t.Fatal(err)
	}
	offer := <-offers
	if err := c.pc.SetRemoteDescription(offer); err != nil {
		t.Fatal(err)
	}
	answer, err := c.pc.CreateAnswer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.pc.SetLocalDescription(answer); err != nil {
		t.Fatal(err)
	}
	// Garbage is only found out when the candidate is applied.
	if err := peer.AddRemoteCandidate(webrtc.ICECandidateInit{Candidate: "candidate:1 1 udp notanumber 198.51.100.7 50000 typ host"}); err != nil {
		t.Fatalf("held candidate returned %v", err)
	}
	if err := peer.SetAnswer(answer); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "level=WARN msg=\"sfu remote candidate dropped\" user="+peer.ID.String()+" reason=\"invalid candidate\"") {
		t.Fatalf("dropped early candidate not logged: %s", logs.String())
	}
}

func TestCandidateDropReasons(t *testing.T) {
	for err, want := range map[error]string{
		errTooManyCandidates:                          "too many candidates before the answer",
		webrtc.ErrNoRemoteDescription:                 "no remote description",
		webrtc.ErrConnectionClosed:                    "connection closed",
		fmt.Errorf("parse: %w", errTooManyCandidates): "too many candidates before the answer",
		fmt.Errorf("boom"):                            "invalid candidate",
	} {
		if got := candidateDropReason(err); got != want {
			t.Errorf("%v: %q, want %q", err, got, want)
		}
	}
}
