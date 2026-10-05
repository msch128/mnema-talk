package sfu

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

// videoSections returns the SDP's video m-sections in order, each as its lines.
func videoSections(sdp string) [][]string {
	var out [][]string
	var cur []string
	for _, l := range strings.Split(strings.ReplaceAll(sdp, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(l, "m=") {
			if cur != nil {
				out = append(out, cur)
				cur = nil
			}
			if strings.HasPrefix(l, "m=video") {
				cur = []string{l}
			}
			continue
		}
		if cur != nil {
			cur = append(cur, l)
		}
	}
	if cur != nil {
		out = append(out, cur)
	}
	return out
}

// firstCodec is the codec name of the first payload type on a video m-line.
func firstCodec(t *testing.T, section []string) string {
	t.Helper()
	pts := strings.Fields(section[0])[3:]
	for _, l := range section {
		if name, ok := strings.CutPrefix(l, "a=rtpmap:"+pts[0]+" "); ok {
			return strings.Split(name, "/")[0]
		}
	}
	t.Fatalf("no rtpmap for %s", pts[0])
	return ""
}

// preferH264OnFirstVideoLine moves the H.264 payload types of the first video
// m-line to the front, like the web client does for its screen line.
func preferH264OnFirstVideoLine(sdp string) string {
	lines := strings.Split(sdp, "\r\n")
	for i, l := range lines {
		if !strings.HasPrefix(l, "m=video") {
			continue
		}
		h264 := map[string]bool{}
		for _, m := range lines[i+1:] {
			if strings.HasPrefix(m, "m=") {
				break
			}
			if rest, ok := strings.CutPrefix(m, "a=rtpmap:"); ok && strings.Contains(rest, " H264/") {
				h264[strings.Fields(rest)[0]] = true
			}
		}
		fields := strings.Fields(l)
		var first, rest []string
		for _, pt := range fields[3:] {
			if h264[pt] {
				first = append(first, pt)
			} else {
				rest = append(rest, pt)
			}
		}
		lines[i] = strings.Join(append(append(fields[:3:3], first...), rest...), " ")
		break
	}
	return strings.Join(lines, "\r\n")
}

// Regression: after a client answered with H.264 first on its screen line,
// Pion re-offered every video line in that order. A browser then switched its
// running VP8 camera to H.264 while viewers kept receiving it as VP8.
func TestRenegotiationKeepsTheCameraLineCodecOrder(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()
	c := newClient(t)
	offers := make(chan webrtc.SessionDescription, 8)
	_, peer, err := s.Join(room, c.id, func(o webrtc.SessionDescription) { offers <- o }, nil)
	if err != nil {
		t.Fatal(err)
	}
	nextOffer := func() webrtc.SessionDescription {
		t.Helper()
		select {
		case o := <-offers:
			return o
		case <-time.After(5 * time.Second):
			t.Fatal("no offer")
			return webrtc.SessionDescription{}
		}
	}

	first := nextOffer()
	lines := videoSections(first.SDP)
	if len(lines) != 2 {
		t.Fatalf("want screen and camera lines, got %d video lines", len(lines))
	}
	cameraCodec := firstCodec(t, lines[1])
	if cameraCodec != "VP8" {
		t.Fatalf("camera line starts with %s, want VP8", cameraCodec)
	}

	if err := c.pc.SetRemoteDescription(first); err != nil {
		t.Fatal(err)
	}
	answer, err := c.pc.CreateAnswer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.pc.SetLocalDescription(answer); err != nil {
		t.Fatal(err)
	}
	answer.SDP = preferH264OnFirstVideoLine(answer.SDP)
	if got := firstCodec(t, videoSections(answer.SDP)[0]); got != "H264" {
		t.Fatalf("munged screen line starts with %s", got)
	}
	if err := peer.SetAnswer(answer); err != nil {
		t.Fatal(err)
	}

	// Another member's camera makes the SFU offer again.
	other := uuid.New()
	if _, _, err := s.Join(room, other, func(webrtc.SessionDescription) {}, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Room(room).PublishTestVideo(other, SourceCamera); err != nil {
		t.Fatal(err)
	}
	second := nextOffer()
	lines = videoSections(second.SDP)
	if len(lines) < 2 {
		t.Fatalf("re-offer lost the publish lines: %d video lines", len(lines))
	}
	if got := firstCodec(t, lines[1]); got != cameraCodec {
		t.Fatalf("re-offer starts the camera line with %s, the first offer with %s", got, cameraCodec)
	}
}
