package sfu

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

// A peer whose client never completes the connection is reported once, with
// every layer's state, instead of staying silent in the log.
func TestStuckConnectingPeerIsReported(t *testing.T) {
	s := newTestSFU(t)
	s.offerTimeout = 200 * time.Millisecond
	logs := captureSFULog(t)
	_, peer, err := s.Join(uuid.New(), uuid.New(), func(webrtc.SessionDescription) {}, func(*webrtc.ICECandidateInit) {})
	if err != nil {
		t.Fatal(err)
	}
	waitSFUCondition(t, "the report", func() bool { return strings.Contains(logs.String(), "sfu peer still connecting") })
	out := logs.String()
	for _, want := range []string{"user=" + peer.ID.String(), "answered=false", "client_candidates_unapplied=0", "gathering=", "signaling=have-local-offer"} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q: %s", want, out)
		}
	}
}

// A peer that is gone or already connected needs no report.
func TestClosedPeerIsNotReportedAsConnecting(t *testing.T) {
	s := newTestSFU(t)
	s.offerTimeout = 200 * time.Millisecond
	logs := captureSFULog(t)
	room, peer, err := s.Join(uuid.New(), uuid.New(), func(webrtc.SessionDescription) {}, func(*webrtc.ICECandidateInit) {})
	if err != nil {
		t.Fatal(err)
	}
	s.RemovePeer(room.ID, peer)
	time.Sleep(600 * time.Millisecond)
	if strings.Contains(logs.String(), "sfu peer still connecting") {
		t.Fatalf("reported a removed peer: %s", logs.String())
	}
}
