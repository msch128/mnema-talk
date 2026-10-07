package sfu

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

// joinWithOffers joins a fresh room with a shortened offer timeout and
// returns the peer and its offers.
func joinWithOffers(t *testing.T, timeout time.Duration) (*Peer, chan webrtc.SessionDescription) {
	t.Helper()
	s, err := NewSFU(0, 0, []string{"127.0.0.1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.offerTimeout = timeout
	t.Cleanup(func() { _ = s.Close() })
	offers := make(chan webrtc.SessionDescription, 8)
	_, peer, err := s.Join(uuid.New(), uuid.New(), func(o webrtc.SessionDescription) { offers <- o }, nil)
	if err != nil {
		t.Fatal(err)
	}
	return peer, offers
}

func nextOffer(t *testing.T, offers chan webrtc.SessionDescription, what string) webrtc.SessionDescription {
	t.Helper()
	select {
	case o := <-offers:
		return o
	case <-time.After(5 * time.Second):
		t.Fatalf("%s missing", what)
	}
	return webrtc.SessionDescription{}
}

// answerOffer plays the browser: it applies offer and returns its answer.
func answerOffer(t *testing.T, client *webrtc.PeerConnection, offer webrtc.SessionDescription) webrtc.SessionDescription {
	t.Helper()
	if err := client.SetRemoteDescription(offer); err != nil {
		t.Fatal(err)
	}
	answer, err := client.CreateAnswer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SetLocalDescription(answer); err != nil {
		t.Fatal(err)
	}
	return answer
}

// A lost answer must not leave the peer in have-local-offer for good: the SFU
// sends the offer again, and the browser, which already applied it, answers
// once more.
func TestUnansweredOfferIsRetried(t *testing.T) {
	peer, offers := joinWithOffers(t, 100*time.Millisecond)
	client, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	first := nextOffer(t, offers, "initial offer")
	// The browser applies it, but its answer never reaches the server.
	_ = answerOffer(t, client, first)

	second := nextOffer(t, offers, "retried offer")
	// Pion adds gathered candidates to the pending offer; the session line
	// (ID and version) shows it is the same offer.
	if originLine(second.SDP) != originLine(first.SDP) {
		t.Fatal("retry is not the outstanding offer")
	}
	if err := peer.SetAnswer(answerOffer(t, client, second)); err != nil {
		t.Fatalf("answer to retried offer: %v", err)
	}
	if st := peer.PC.SignalingState(); st != webrtc.SignalingStateStable {
		t.Fatalf("signaling state %s after answer, want stable", st)
	}
	select {
	case <-offers:
		t.Fatal("answered offer was retried")
	case <-time.After(300 * time.Millisecond):
	}
}

// An answer the SFU cannot apply leaves the offer outstanding, and the
// watchdog sends it again.
func TestFailedAnswerRestartsNegotiation(t *testing.T) {
	peer, offers := joinWithOffers(t, 200*time.Millisecond)
	nextOffer(t, offers, "initial offer")
	waitSignalingIdle(t, peer)

	bad := webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: "v=0\r\nnot an answer\r\n"}
	if err := peer.SetAnswer(bad); err == nil {
		t.Fatal("malformed answer was accepted")
	}
	nextOffer(t, offers, "offer after failed answer")
}

// A stale answer arriving once the peer is stable does not trigger a new
// round.
func TestStaleAnswerInStableStateIsIgnored(t *testing.T) {
	peer, offers := joinWithOffers(t, time.Hour)
	client, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	answer := answerOffer(t, client, nextOffer(t, offers, "initial offer"))
	waitSignalingIdle(t, peer)
	if err := peer.SetAnswer(answer); err != nil {
		t.Fatal(err)
	}
	if err := peer.SetAnswer(answer); err == nil {
		t.Fatal("duplicate answer in stable state was accepted")
	}
	select {
	case <-offers:
		t.Fatal("stale answer triggered a new offer")
	case <-time.After(300 * time.Millisecond):
	}
}

func originLine(sdp string) string {
	for line := range strings.SplitSeq(sdp, "\r\n") {
		if strings.HasPrefix(line, "o=") {
			return line
		}
	}
	return ""
}
