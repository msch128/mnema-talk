package sfu

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
)

// What the hub does to the media when a right is taken away, measured over
// real loopback connections. Losing Connect or View, a kick from the Talk and
// a Talk made private all end in SFU.RemovePeer (internal/ws/voice.go:121-128,
// reached from enforceVoiceAccess and kickUserFromChannel); losing Speak or
// Video ends in SetDeniedSources plus RevokeUserSource (internal/ws/perms.go:
// 99-123). Video covers the camera and the screen: there is no separate screen
// share permission (internal/ws/perms.go:89-91).

// mediaStopBound is how long packets already in flight may still arrive after
// the change: the SFU drops the forwarding at once, the network does not.
const mediaStopBound = 300 * time.Millisecond

// packetsAfter drains c's received packets, waits settle, then counts what
// arrives during the next window.
func packetsAfter(c *client, settle, window time.Duration) int {
	deadline := time.After(settle)
drain:
	for {
		select {
		case <-c.received:
		case <-deadline:
			break drain
		}
	}
	n := 0
	end := time.After(window)
	for {
		select {
		case <-c.received:
			n++
		case <-end:
			return n
		}
	}
}

// talkWithTwo has a speaker and a listener, each publishing a microphone, both
// hearing the other.
func talkWithTwo(t *testing.T) (*SFU, uuid.UUID, *client, *client) {
	t.Helper()
	s := newTestSFU(t)
	room := uuid.New()
	speaker, listener := newClient(t), newClient(t)
	speaker.publishAudio("mic")
	listener.publishAudio("mic")
	speaker.join(s, room)
	listener.join(s, room)
	waitTrack(t, listener)
	waitTrack(t, speaker)
	if packetsAfter(listener, 0, 300*time.Millisecond) == 0 || packetsAfter(speaker, 0, 300*time.Millisecond) == 0 {
		t.Fatal("the two do not hear each other before the change")
	}
	return s, room, speaker, listener
}

// A member removed from the Talk (Connect or View lost, kicked, Talk made
// private) stops receiving and stops being heard, and the RTCP its client
// still sends reaches nobody.
func TestARemovedMemberNeitherHearsNorIsHeard(t *testing.T) {
	s, room, speaker, listener := talkWithTwo(t)
	ssrc := uint32(0)
	for _, r := range listener.pc.GetReceivers() {
		if tr := r.Track(); tr != nil {
			ssrc = uint32(tr.SSRC())
		}
	}

	start := time.Now()
	s.RemovePeer(room, listener.peer)
	took := time.Since(start)
	if n := packetsAfter(listener, mediaStopBound, time.Second); n != 0 {
		t.Fatalf("the removed member still received %d packets %v after the removal", n, mediaStopBound)
	}
	if n := packetsAfter(speaker, mediaStopBound, time.Second); n != 0 {
		t.Fatalf("the others still heard the removed member: %d packets", n)
	}
	if st := listener.peer.PC.ConnectionState(); st != webrtc.PeerConnectionStateClosed {
		t.Fatalf("the removed member's server connection is %v, not closed", st)
	}
	// Feedback from the removed client (a keyframe request, a receiver
	// report) reaches no publisher.
	drainPLIs(speaker)
	_ = listener.pc.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: ssrc}, &rtcp.ReceiverReport{}})
	select {
	case <-speaker.plis:
		t.Fatal("RTCP of the removed member reached the speaker")
	case <-time.After(500 * time.Millisecond):
	}
	// A renegotiation cannot bring the connection back: the server side is
	// closed, so an answer to it is refused.
	if err := listener.peer.SetAnswer(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: listener.pc.LocalDescription().SDP}); err == nil {
		t.Fatal("the closed connection took an answer")
	}
	t.Logf("RemovePeer returned after %v", took.Round(time.Millisecond))
}

// A member who loses Speak is no longer heard; they still hear the others,
// which is the right they kept.
func TestAMemberWhoLosesSpeakIsNoLongerHeardButStillHears(t *testing.T) {
	s, room, speaker, listener := talkWithTwo(t)

	speaker.peer.SetDeniedSources(SourceSet(0).With(SourceAudio).With(SourceScreenAudio))
	s.Room(room).RevokeUserSource(speaker.id, SourceAudio)

	if n := packetsAfter(listener, mediaStopBound, time.Second); n != 0 {
		t.Fatalf("the member who lost Speak was still heard: %d packets", n)
	}
	if n := packetsAfter(speaker, 0, 500*time.Millisecond); n == 0 {
		t.Fatal("the member who lost Speak stopped hearing the others")
	}
	// The packets the client keeps sending do not bring the voice back.
	time.Sleep(resumeHoldoff + 300*time.Millisecond)
	if n := packetsAfter(listener, 0, 500*time.Millisecond); n != 0 {
		t.Fatalf("the revoked voice came back by itself: %d packets", n)
	}
}

// A member who loses Video stops showing the screen share they had published.
func TestAMemberWhoLosesVideoStopsSharingTheScreen(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()
	sharer, viewer := newClient(t), newClient(t)
	sharer.publishVideo("screen")
	sharer.join(s, room)
	viewer.join(s, room)
	r := s.Room(room)
	waitSources(t, r, sharer.id, SourceScreen, 1)

	sharer.peer.SetDeniedSources(SourceSet(0).With(SourceScreen).With(SourceScreenAudio).With(SourceCamera))
	r.RevokeUserSource(sharer.id, SourceScreen)
	if n := r.sourceCount(sharer.id, SourceScreen); n != 0 {
		t.Fatalf("the screen share is still published (%d tracks)", n)
	}
	if r.Receives(viewer.id, sharer.id, SourceScreen) {
		t.Fatal("the viewer still receives the revoked screen share")
	}
	time.Sleep(resumeHoldoff + 300*time.Millisecond)
	if n := r.sourceCount(sharer.id, SourceScreen); n != 0 {
		t.Fatalf("the revoked screen share came back by itself (%d)", n)
	}
}
