package sfu

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/safego"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
)

// linesWith returns the logged lines that contain every one of parts.
func linesWith(logged *synchronizedLogCapture, parts ...string) []string {
	var out []string
	for _, line := range strings.Split(logged.String(), "\n") {
		all := line != ""
		for _, part := range parts {
			all = all && strings.Contains(line, part)
		}
		if all {
			out = append(out, line)
		}
	}
	return out
}

// A member who joins again from a second session gets a new peer, and the
// log tells the two apart: what the replaced peer still logs carries the
// first session's connection id, and the new peer's lines and its recovered
// panic carry the second's.
func TestPeersOfTwoSessionsOfOneMemberAreToldApartInTheLog(t *testing.T) {
	// The panic's line is read below: an earlier test or run may have used
	// up what the site may log in a minute.
	safego.RefillLogBudget()
	logged := captureSFULog(t)
	s := newTestSFU(t)
	room, member := uuid.New(), uuid.New()

	if _, _, err := s.Join(room, member, "tab-one", func(webrtc.SessionDescription) {}, nil); err != nil {
		t.Fatal(err)
	}
	_, second, err := s.Join(room, member, "tab-two", func(webrtc.SessionDescription) { panic("offer bug") }, nil)
	if err != nil {
		t.Fatal(err)
	}

	// The join replaced the first session's peer, whose connection closes.
	waitSFUCondition(t, "the replaced peer's state line", func() bool {
		return len(linesWith(logged, "sfu peer state", "user="+member.String(), "state=closed", "conn_id=tab-one")) == 1
	})
	// The second session's first offer panics on its way out: its peer is
	// dropped, and the panic names that session.
	lost := make(chan struct{})
	second.OnLost(func() { close(lost) })
	<-lost
	waitSFUCondition(t, "the panic of the second session", func() bool {
		return len(linesWith(logged, "goroutine panicked", "name=sfu_signal", "conn_id=tab-two")) == 1
	})
	if wrong := linesWith(logged, "goroutine panicked", "conn_id=tab-one"); len(wrong) != 0 {
		t.Fatalf("the panic is logged under the other session: %v", wrong)
	}
	for _, line := range linesWith(logged, "user="+member.String()) {
		if !strings.Contains(line, "conn_id=tab-") {
			t.Errorf("a line about the member names no connection: %s", line)
		}
	}
}

// The outermost guard of a connection knows only Pion's ID of it. The SFU
// keeps the hub connection for that ID while the connection lives, so the
// guard's panic line can name it, and forgets it when the connection closes.
func TestViewerGuardKnowsItsHubConnectionWhileTheConnectionLives(t *testing.T) {
	s := newTestSFU(t)
	_, peer, err := s.Join(uuid.New(), uuid.New(), "tab-one", func(webrtc.SessionDescription) {}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.conns.of(peer.connID); got != "tab-one" {
		t.Fatalf("the guard would log conn_id %q, want tab-one", got)
	}
	if err := peer.PC.Close(); err != nil {
		t.Fatal(err)
	}
	waitSFUCondition(t, "the closed connection is forgotten", func() bool { return s.conns.of(peer.connID) == "" })
}

// A peer joined without a hub connection (a test, the load generator) logs
// as before, without the attribute.
func TestPeerWithoutAHubConnectionLogsNoConnectionID(t *testing.T) {
	logged := captureSFULog(t)
	s := newTestSFU(t)
	room, member := uuid.New(), uuid.New()
	_, peer, err := s.Join(room, member, "", func(webrtc.SessionDescription) {}, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.RemovePeer(room, peer)
	waitSFUCondition(t, "the peer's state line", func() bool {
		return len(linesWith(logged, "sfu peer state", "user="+member.String())) > 0
	})
	// Only this member's lines: a peer of an earlier test may still be closing
	// and logs its own connection id into the shared capture.
	if got := linesWith(logged, "conn_id=", "user="+member.String()); len(got) != 0 {
		t.Fatalf("lines carry a connection id nobody gave: %v", got)
	}
}

// What goes wrong for one peer outside its forwarding loops names the peer's
// session too: an offer that cannot be built, and the panic of the worker
// that answers the session's retransmission requests.
func TestAPeersFailedOfferAndItsNackWorkersPanicNameItsSession(t *testing.T) {
	safego.RefillLogBudget()
	logged := captureSFULog(t)
	s := newTestSFU(t)
	member := uuid.New()
	_, peer, err := s.Join(uuid.New(), member, "tab-one", func(webrtc.SessionDescription) {}, nil)
	if err != nil {
		t.Fatal(err)
	}

	i, err := nackResponderFactory{names: &s.conns}.NewInterceptor(peer.connID)
	if err != nil {
		t.Fatal(err)
	}
	n := i.(*nackResponder)
	t.Cleanup(func() { _ = n.Close() })
	n.afterResend = func() { panic("resend bug") }
	n.enqueue(&rtcp.TransportLayerNack{})
	waitSFUCondition(t, "the worker's panic line", func() bool {
		return len(linesWith(logged, "goroutine panicked", "name=sfu_nack", "conn_id=tab-one")) == 1
	})

	if err := peer.PC.Close(); err != nil {
		t.Fatal(err)
	}
	// The closed connection's own line, so it does not reach the next test.
	waitSFUCondition(t, "the peer's state line", func() bool {
		return len(linesWith(logged, "sfu peer state", "state=closed", "conn_id=tab-one")) == 1
	})
	if err := negotiateOffer(peer); err == nil {
		t.Fatal("an offer was built on a closed connection")
	}
	if got := linesWith(logged, "sfu create offer", "conn_id=tab-one", "user="+member.String()); len(got) != 1 {
		t.Fatalf("the failed offer does not name its session and member:\n%s", logged.String())
	}
}
