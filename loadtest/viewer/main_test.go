package main

import (
	"strings"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

func TestRunBoundedReturnsDespiteStuckFunction(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	fns := []func(){func() {}, func() { <-release }, func() {}}
	start := time.Now()
	done := runBounded(fns, 200*time.Millisecond)
	if done != 2 {
		t.Fatalf("finished = %d, want 2", done)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("took %s, want about the limit", elapsed)
	}
}

func TestRunBoundedReturnsEarlyWhenAllFinish(t *testing.T) {
	start := time.Now()
	if done := runBounded([]func(){func() {}, func() {}}, 10*time.Second); done != 2 {
		t.Fatalf("finished = %d, want 2", done)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("waited for the limit although everything finished")
	}
}

func newTestMember() *member {
	return &member{id: "00000000-0000-0000-0000-000000000001", stop: make(chan struct{}), startedAt: time.Now()}
}

func TestWatchConnectMarksSilentMemberFailed(t *testing.T) {
	m := newTestMember()
	m.watchConnect(20 * time.Millisecond)
	if !m.timedOut.Load() {
		t.Fatal("member not marked timed out")
	}
	msg, _ := m.failed.Load().(string)
	if !strings.Contains(msg, "ice connect timeout") {
		t.Fatalf("failed = %q", msg)
	}
	res := summarize([]*member{m}, 0, 0)
	if res.ICETimedOut != 1 || res.NeverConnected != 1 || res.Failed != 1 {
		t.Fatalf("summary = %+v", res)
	}
	if len(res.PerMember[0].Timeline) == 0 {
		t.Fatal("no timeline for a member that never connected")
	}
}

func TestWatchConnectIgnoresConnectedAndStoppedMembers(t *testing.T) {
	m := newTestMember()
	m.everConn.Store(true)
	m.watchConnect(10 * time.Millisecond)
	if m.timedOut.Load() {
		t.Fatal("connected member timed out")
	}
	s := newTestMember()
	close(s.stop)
	s.watchConnect(time.Hour)
	if s.timedOut.Load() {
		t.Fatal("stopped member timed out")
	}
}

func TestIceTimeoutDefaultAndOverride(t *testing.T) {
	if got := iceTimeout(); got != 25*time.Second {
		t.Fatalf("default = %s", got)
	}
	t.Setenv("ICE_TIMEOUT_S", "3")
	if got := iceTimeout(); got != 3*time.Second {
		t.Fatalf("override = %s", got)
	}
}

func TestTrimUfragAndRemoteNotes(t *testing.T) {
	line := "candidate:1 1 udp 2130706431 192.0.2.1 50000 typ host ufrag abcd generation 0"
	if got := trimUfrag(line); strings.Contains(got, "abcd") || !strings.HasSuffix(got, "typ host") {
		t.Fatalf("trimUfrag = %q", got)
	}
	m := newTestMember()
	m.noteRemote(webrtc.ICECandidateInit{Candidate: line}, "added")
	m.event("ice %s", "checking")
	lines := m.diagLines()
	if len(lines) != 2 || !strings.Contains(lines[0], "ice checking") || !strings.Contains(lines[1], "added candidate:1") {
		t.Fatalf("diagLines = %q", lines)
	}
}

func TestQueuedRemoteCandidatesAreAppliedOnceTheOfferIs(t *testing.T) {
	api, err := newAPI(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pc.Close() }()
	m := newTestMember()
	m.pc = pc
	m.addRemote(webrtc.ICECandidateInit{Candidate: "candidate:1 1 udp 2130706431 192.0.2.1 50000 typ host"})
	if m.candRecv.Load() != 0 || len(m.pendingIn) != 1 {
		t.Fatalf("candidate not queued: recv=%d pending=%d", m.candRecv.Load(), len(m.pendingIn))
	}
	// A real remote description is needed to apply it; here the failure is
	// recorded instead, which must not lose the count.
	m.remoteApplied()
	if m.candRecv.Load() != 1 || len(m.pendingIn) != 0 {
		t.Fatalf("after remoteApplied: recv=%d pending=%d", m.candRecv.Load(), len(m.pendingIn))
	}
	if m.candErr.Load() != 1 {
		t.Fatalf("candidate errors = %d, want 1 (no remote description in this test)", m.candErr.Load())
	}
}

func TestSnapshotOfFreshConnection(t *testing.T) {
	api, err := newAPI(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pc.Close() }()
	m := newTestMember()
	m.pc = pc
	m.diagnose()
	if m.snapshot == nil || m.snapshot.Unresponsive || m.snapshot.IceState == "" {
		t.Fatalf("snapshot = %+v", m.snapshot)
	}
}

func TestBlockedUDPWritesCountsNothingInAHealthyProcess(t *testing.T) {
	if n := blockedUDPWrites(); n != 0 {
		t.Fatalf("blocked writes = %d", n)
	}
}
