package sfu

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

// attachRig is a layered screen share in a room and a viewer peer that has
// not received it yet.
type attachRig struct {
	room   *Room
	viewer *Peer
	info   *TrackInfo
	group  *layerGroup
}

func newAttachRig(t *testing.T) *attachRig {
	t.Helper()
	s := newTestSFU(t)
	room, _, err := s.Join(uuid.New(), uuid.New(), "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	viewer, err := s.joinPeerIn(room, uuid.New(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	// The tests call addSender themselves, which a signaling round does while
	// it owns the peer's signaling lock. Joining started a round of the room's
	// own; it must be over, and no other may start, or it would take the share
	// (which the room does not list) off the viewer again in the middle of a
	// test.
	viewer.signalingMu.Lock()
	t.Cleanup(viewer.signalingMu.Unlock)
	rig := newLayerRig(t, "video/VP8", 3)
	return &attachRig{room: room, viewer: viewer, info: rig.infos[0], group: rig.group}
}

// A rejoined viewer's frozen video: the old connection's late round must not
// take the place of the new connection's track.
func TestARejoinedViewerKeepsItsTrackWhenTheOldConnectionRegistersLate(t *testing.T) {
	rig := newLayerRig(t, "video/VP8", 3)
	user := uuid.New()
	oldVT, err := rig.group.newViewer(user, -1, 0, "t", "s")
	if err != nil {
		t.Fatal(err)
	}
	newVT, err := rig.group.newViewer(user, -1, 0, "t", "s")
	if err != nil {
		t.Fatal(err)
	}
	oldVT.gen, newVT.gen = 1, 2

	if !rig.group.addViewer(newVT) {
		t.Fatal("the newer connection's track was refused")
	}
	if rig.group.addViewer(oldVT) {
		t.Fatal("the replaced connection's track took the viewer's place")
	}
	if got := rig.group.viewerFor(user); got != newVT {
		t.Fatal("the rejoined viewer lost its track")
	}
	newVT.mu.Lock()
	closed := newVT.closed
	newVT.mu.Unlock()
	if closed {
		t.Fatal("the rejoined viewer's track was closed by the old connection")
	}
}

func TestAClosedTrackIsNotRegisteredAsViewer(t *testing.T) {
	rig := newLayerRig(t, "video/VP8", 3)
	vt, err := rig.group.newViewer(uuid.New(), -1, 0, "t", "s")
	if err != nil {
		t.Fatal(err)
	}
	vt.close()
	if rig.group.addViewer(vt) || rig.group.viewerFor(vt.viewer) != nil {
		t.Fatal("a closed track was registered")
	}
}

// Leaving while a round adds the track must leave no viewer behind that keeps
// its layer wanted.
func TestLeavingBetweenTheAttachStepsLeavesNoViewerBehind(t *testing.T) {
	rig := newAttachRig(t)
	before := rig.group.need()
	peer := rig.viewer
	peer.attachStep = func(step string) {
		if step == "viewer-added" {
			peer.releaseViewerTracks()
		}
	}
	if rig.room.addSender(peer, "screen", rig.info) {
		t.Fatal("a track was added to a released peer")
	}
	if rig.group.viewerFor(peer.ID) != nil {
		t.Fatal("a released peer's track stayed registered")
	}
	if got := rig.group.need(); got != before {
		t.Fatalf("layers needed = %b, want %b", got, before)
	}
}

// A quality or fit choice made while the track is being attached must not be
// lost because no viewer existed to apply it to.
func TestAChoiceMadeWhileTheTrackIsAttachedIsKept(t *testing.T) {
	rig := newAttachRig(t)
	peer, pub := rig.viewer, rig.info.SenderID
	peer.attachStep = func(step string) {
		if step != "viewer-made" {
			return
		}
		if err := rig.room.SetScreenQuality(peer.ID, pub, 2); err != nil {
			t.Error(err)
		}
		if err := rig.room.SetScreenFit(peer.ID, pub, 1); err != nil {
			t.Error(err)
		}
	}
	if !rig.room.addSender(peer, "screen", rig.info) {
		t.Fatal("track not added")
	}
	vt := rig.group.viewerFor(peer.ID)
	vt.mu.Lock()
	pref, fit := vt.pref, vt.fit
	vt.mu.Unlock()
	if pref != 2 || fit != 1 {
		t.Fatalf("viewer has quality %d fit %d, want 2 and 1", pref, fit)
	}
}

// A choice made just after the attach round read the viewer's choice, but
// before it applied it, is not reverted to the older one.
func TestANewerChoiceIsNotRevertedByTheAttachRound(t *testing.T) {
	rig := newAttachRig(t)
	peer, pub := rig.viewer, rig.info.SenderID
	rig.room.mu.Lock()
	if rig.room.groups == nil {
		rig.room.groups = map[uuid.UUID]*layerGroup{}
	}
	rig.room.groups[pub] = rig.group
	rig.room.mu.Unlock()
	if err := rig.room.SetScreenQuality(peer.ID, pub, 1); err != nil {
		t.Fatal(err)
	}
	once := false
	peer.attachStep = func(step string) {
		if step != "viewer-choice-read" || once {
			return
		}
		once = true
		if err := rig.room.SetScreenQuality(peer.ID, pub, 2); err != nil {
			t.Error(err)
		}
	}
	if !rig.room.addSender(peer, "screen", rig.info) {
		t.Fatal("track not added")
	}
	vt := rig.group.viewerFor(peer.ID)
	vt.mu.Lock()
	pref := vt.pref
	vt.mu.Unlock()
	if pref != 2 {
		t.Fatalf("viewer has quality %d, want the newer choice 2", pref)
	}
}

// quietPeer is a peer whose connection nobody else signals: unlike a peer that
// joined a room, no offer of the server is on its way.
func quietPeer(t *testing.T) *Peer {
	t.Helper()
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	return &Peer{ID: uuid.New(), PC: pc}
}

func candidatesCounted(p *Peer) int {
	p.candMu.Lock()
	defer p.candMu.Unlock()
	return len(p.cands.seen)
}

// A rejected answer must not use up the candidates it carried.
func TestARefusedAnswerLeavesItsCandidatesUnused(t *testing.T) {
	p := quietPeer(t)
	sdp := sdpHead + candidateLine("192.0.2.7") + "\r\n"
	err := p.SetAnswer(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: sdp})
	if err == nil {
		t.Fatal("an answer without an offer was accepted")
	}
	if used := candidatesCounted(p); used != 0 || strings.Contains(err.Error(), "budget") {
		t.Fatalf("a refused answer used %d candidates of the budget", used)
	}
}

// The server's next offer can be set while an answer is being refused. That
// changes the signaling state, but the answer still reached nothing: its
// candidates stay unused.
func TestAnOfferSetWhileAnAnswerIsRefusedDoesNotCountItsCandidates(t *testing.T) {
	p := quietPeer(t)
	if _, err := p.PC.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio); err != nil {
		t.Fatal(err)
	}
	before := remoteStateOf(p.PC)
	offer, err := p.PC.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.PC.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	if !before.unchanged(p.PC) {
		t.Fatal("an offer of the server was taken for an applied answer")
	}
}

// An answer Pion applies and only then refuses has handed its candidates
// over: they stay counted.
func TestAnAnswerRefusedAfterItWasAppliedKeepsItsCandidatesCounted(t *testing.T) {
	p := quietPeer(t)
	if _, err := p.PC.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo); err != nil {
		t.Fatal(err)
	}
	offer, err := p.PC.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.PC.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	sdp := sdpHead + candidateLine("192.0.2.7") + "\r\n"
	if err := p.SetAnswer(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: sdp}); err == nil {
		t.Fatal("an answer that does not match the offer was accepted")
	}
	if p.PC.CurrentRemoteDescription() == nil {
		t.Fatal("the fixture must be an answer Pion applies before refusing it")
	}
	if used := candidatesCounted(p); used != 1 {
		t.Fatalf("%d candidates counted, want the one Pion took", used)
	}
}
