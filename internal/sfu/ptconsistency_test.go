package sfu

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

// assertBundlePTConsistent fails when one payload type means different things
// (codec, clock rate, fmtp) on different m-lines of the same BUNDLE group.
// Chromium logs "Bundled payload type collision" for that.
func assertBundlePTConsistent(t *testing.T, sdp string) {
	t.Helper()
	type def struct{ rtpmap, fmtp string }
	seen := map[string]def{}
	var cur map[string]def
	flush := func() {
		for pt, d := range cur {
			if prev, ok := seen[pt]; ok && prev != d {
				t.Errorf("payload type %s collides in the bundle: %+v vs %+v", pt, prev, d)
			}
			seen[pt] = d
		}
	}
	for _, l := range strings.Split(strings.ReplaceAll(sdp, "\r\n", "\n"), "\n") {
		switch {
		case strings.HasPrefix(l, "m="):
			flush()
			cur = map[string]def{}
		case cur != nil && strings.HasPrefix(l, "a=rtpmap:"):
			pt, rest, _ := strings.Cut(strings.TrimPrefix(l, "a=rtpmap:"), " ")
			d := cur[pt]
			d.rtpmap = rest
			cur[pt] = d
		case cur != nil && strings.HasPrefix(l, "a=fmtp:"):
			pt, rest, _ := strings.Cut(strings.TrimPrefix(l, "a=fmtp:"), " ")
			d := cur[pt]
			d.fmtp = rest
			cur[pt] = d
		}
	}
	flush()
}

func TestOffersKeepPayloadTypesConsistentAcrossBundle(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()
	offers := make(chan webrtc.SessionDescription, 16)
	_, peer, err := s.Join(room, uuid.New(), func(o webrtc.SessionDescription) { offers <- o }, nil)
	if err != nil {
		t.Fatal(err)
	}
	client, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	round := func(what string) {
		t.Helper()
		var o webrtc.SessionDescription
		select {
		case o = <-offers:
		case <-time.After(1500 * time.Millisecond):
			t.Logf("%s: no new offer (existing lines reused)", what)
			return
		}
		assertBundlePTConsistent(t, o.SDP)
		if err := peer.SetAnswer(answerOffer(t, client, o)); err != nil {
			t.Fatal(err)
		}
	}
	round("initial offer")
	for i := 0; i < 5; i++ {
		other := uuid.New()
		if _, _, err := s.Join(room, other, func(webrtc.SessionDescription) {}, nil); err != nil {
			t.Fatal(err)
		}
		for _, src := range []Source{SourceCamera, SourceScreen} {
			if err := s.Room(room).PublishTestVideo(other, src); err != nil {
				t.Fatal(err)
			}
			round("offer for " + string(src))
		}
	}
}
