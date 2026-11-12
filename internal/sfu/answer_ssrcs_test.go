package sfu

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/pion/sdp/v3"
	"github.com/pion/webrtc/v4"
)

func section(attrs ...string) *sdp.MediaDescription {
	m := &sdp.MediaDescription{}
	for _, a := range attrs {
		key, value, _ := strings.Cut(a, ":")
		m.Attributes = append(m.Attributes, sdp.Attribute{Key: key, Value: value})
	}
	return m
}

func TestAnAnswerNamesOneStreamPerMediaSection(t *testing.T) {
	for _, c := range []struct {
		name     string
		sections []*sdp.MediaDescription
		ok       bool
	}{
		{"no SSRC lines at all", []*sdp.MediaDescription{section("mid:0", "recvonly")}, true},
		{"one SSRC with its lines", []*sdp.MediaDescription{section("ssrc:11 cname:a", "ssrc:11 msid:s t")}, true},
		{"a stream and its retransmission", []*sdp.MediaDescription{section("ssrc-group:FID 11 12", "ssrc:11 cname:a", "ssrc:12 cname:a")}, true},
		{"retransmission and error correction", []*sdp.MediaDescription{section("ssrc-group:FID 11 12", "ssrc-group:FEC-FR 11 13", "ssrc:11 cname:a", "ssrc:12 cname:a", "ssrc:13 cname:a")}, true},
		{"a group whose members have no line of their own", []*sdp.MediaDescription{section("ssrc-group:FID 11 12")}, true},
		{"one stream in each of two sections", []*sdp.MediaDescription{section("ssrc:11 cname:a"), section("ssrc:21 cname:a")}, true},
		{"a second stream in one section", []*sdp.MediaDescription{section("ssrc:11 cname:a", "ssrc:99 cname:x")}, false},
		{"a second stream next to a pair", []*sdp.MediaDescription{section("ssrc-group:FID 11 12", "ssrc:11 cname:a", "ssrc:12 cname:a", "ssrc:99 cname:x")}, false},
		{"layers as a group", []*sdp.MediaDescription{section("ssrc-group:SIM 11 12 13", "ssrc:11 cname:a", "ssrc:12 cname:a", "ssrc:13 cname:a")}, false},
		{"a group of three", []*sdp.MediaDescription{section("ssrc-group:FID 11 12 13")}, false},
		{"a stream that repairs itself", []*sdp.MediaDescription{section("ssrc-group:FID 11 11")}, false},
		{"two groups in a circle", []*sdp.MediaDescription{section("ssrc-group:FID 11 12", "ssrc-group:FID 12 11")}, false},
		{"two pairs", []*sdp.MediaDescription{section("ssrc-group:FID 11 12", "ssrc-group:FID 21 22")}, false},
		{"a pair written with two spaces", []*sdp.MediaDescription{section("ssrc-group:FID 11  12", "ssrc:11 cname:a", "ssrc:12 cname:a")}, false},
		{"a pair written with a space at the end", []*sdp.MediaDescription{section("ssrc-group:FID 11 12 ", "ssrc:11 cname:a", "ssrc:12 cname:a")}, false},
		{"a pair written with a tab", []*sdp.MediaDescription{section("ssrc-group:FID 11\t12", "ssrc:11 cname:a", "ssrc:12 cname:a")}, false},
		{"an SSRC with a leading zero", []*sdp.MediaDescription{section("ssrc:011 cname:a")}, false},
		{"an SSRC with a sign", []*sdp.MediaDescription{section("ssrc:+11 cname:a")}, false},
		{"an SSRC line that starts with a space", []*sdp.MediaDescription{section("ssrc: 11 cname:a")}, false},
		{"an SSRC line split by a tab", []*sdp.MediaDescription{section("ssrc:11\tcname:a")}, false},
		{"an SSRC that is no number", []*sdp.MediaDescription{section("ssrc:eleven cname:a")}, false},
		{"an SSRC past 32 bits", []*sdp.MediaDescription{section("ssrc:4294967296 cname:a")}, false},
		{"the same SSRC in two sections", []*sdp.MediaDescription{section("ssrc:11 cname:a"), section("ssrc:11 cname:a")}, false},
		{"a section's repair SSRC is another section's stream", []*sdp.MediaDescription{section("ssrc-group:FID 11 12"), section("ssrc:12 cname:a")}, false},
	} {
		err := vetAnswerSSRCs(&sdp.SessionDescription{MediaDescriptions: c.sections})
		if (err == nil) != c.ok {
			t.Errorf("%s: %v, want accepted %v", c.name, err, c.ok)
		}
	}
}

// The SFU reads the answer Pion would read: a second stream in a section the
// client sends on is refused before the connection sees the answer, and the
// answer without it is taken.
func TestAnAnswerWithASecondStreamInASectionIsRefused(t *testing.T) {
	s := newTestSFU(t)
	c := newClient(t)
	c.publishAudio("mic")
	offers := make(chan webrtc.SessionDescription, 4)
	_, peer, err := s.Join(uuid.New(), c.id, "", func(o webrtc.SessionDescription) { offers <- o }, nil)
	if err != nil {
		t.Fatal(err)
	}
	<-offers
	<-webrtc.GatheringCompletePromise(peer.PC)
	if err := c.pc.SetRemoteDescription(*peer.PC.LocalDescription()); err != nil {
		t.Fatal(err)
	}
	answer, err := c.pc.CreateAnswer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gathered := webrtc.GatheringCompletePromise(c.pc)
	if err := c.pc.SetLocalDescription(answer); err != nil {
		t.Fatal(err)
	}
	<-gathered
	honest := c.pc.LocalDescription().SDP
	i := strings.Index(honest, "\r\na=ssrc:")
	if i < 0 {
		t.Fatal("the client's answer names no SSRC: the test would refuse nothing")
	}
	second := honest[:i] + "\r\na=ssrc:123456 cname:x" + honest[i:]
	before := peer.PC.CurrentRemoteDescription()
	if err := peer.SetAnswer(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: second}); !errors.Is(err, errAnswerSSRCs) {
		t.Fatalf("an answer with a second stream in one section: %v, want it refused for its SSRC lines", err)
	}
	if peer.PC.CurrentRemoteDescription() != before {
		t.Fatal("the refused answer reached the connection")
	}
	if err := peer.SetAnswer(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: honest}); err != nil {
		t.Fatalf("the client's own answer after the refused one: %v", err)
	}
}

func sdpWithMids(mids ...string) string {
	var b strings.Builder
	b.WriteString("v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\n")
	for _, mid := range mids {
		b.WriteString("m=audio 9 UDP/TLS/RTP/SAVPF 111\r\nc=IN IP4 0.0.0.0\r\na=mid:" + mid + "\r\n")
	}
	return b.String()
}

func TestAnAnswerHasTheMediaSectionsOfItsOffer(t *testing.T) {
	offer := &webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: sdpWithMids("0", "1", "2")}
	for _, c := range []struct {
		name string
		mids []string
		ok   bool
	}{
		{"the offer's sections", []string{"0", "1", "2"}, true},
		{"one section more", []string{"0", "1", "2", "3"}, false},
		{"one section fewer", []string{"0", "1"}, false},
		{"another order", []string{"0", "2", "1"}, false},
		{"another mid", []string{"0", "1", "9"}, false},
		{"a mid twice", []string{"0", "1", "1"}, false},
		{"no section", nil, false},
	} {
		var answer sdp.SessionDescription
		if err := answer.Unmarshal([]byte(sdpWithMids(c.mids...))); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if err := vetAnswerSections(&answer, offer); (err == nil) != c.ok {
			t.Errorf("%s: %v, want accepted %v", c.name, err, c.ok)
		}
	}
	var answer sdp.SessionDescription
	if err := answer.Unmarshal([]byte(sdpWithMids("0", "7"))); err != nil {
		t.Fatal(err)
	}
	if err := vetAnswerSections(&answer, nil); err != nil {
		t.Errorf("without an offer outstanding the connection decides: %v", err)
	}
}

// A section the offer never had is refused before the connection reads the
// answer, so the client does not choose how many sections the connection has.
func TestAnAnswerWithASectionOfItsOwnIsRefused(t *testing.T) {
	s := newTestSFU(t)
	c := newClient(t)
	c.publishAudio("mic")
	offers := make(chan webrtc.SessionDescription, 4)
	_, peer, err := s.Join(uuid.New(), c.id, "", func(o webrtc.SessionDescription) { offers <- o }, nil)
	if err != nil {
		t.Fatal(err)
	}
	<-offers
	<-webrtc.GatheringCompletePromise(peer.PC)
	if err := c.pc.SetRemoteDescription(*peer.PC.LocalDescription()); err != nil {
		t.Fatal(err)
	}
	answer, err := c.pc.CreateAnswer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gathered := webrtc.GatheringCompletePromise(c.pc)
	if err := c.pc.SetLocalDescription(answer); err != nil {
		t.Fatal(err)
	}
	<-gathered
	honest := c.pc.LocalDescription().SDP
	more := honest + "m=audio 9 UDP/TLS/RTP/SAVPF 111\r\nc=IN IP4 0.0.0.0\r\na=mid:own\r\na=rtpmap:111 opus/48000/2\r\na=recvonly\r\n"
	before := peer.PC.CurrentRemoteDescription()
	if err := peer.SetAnswer(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: more}); !errors.Is(err, errAnswerSections) {
		t.Fatalf("an answer with a section of its own: %v, want it refused for its sections", err)
	}
	if peer.PC.CurrentRemoteDescription() != before {
		t.Fatal("the refused answer reached the connection")
	}
	if err := peer.SetAnswer(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: honest}); err != nil {
		t.Fatalf("the client's own answer after the refused one: %v", err)
	}
}
