package sfu

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
)

// What honest clients make of the cap on unread SRTCP streams (H-L8-07).

// firefoxLikeAnswer declares an SSRC on every m-line of the answer that has
// none, as Firefox does for the lines it only receives on (Mozilla bug
// 1397756), and returns the SSRCs it declared.
func firefoxLikeAnswer(first uint32, out *[]uint32) func(string) string {
	return func(sdp string) string {
		sections := strings.Split(sdp, "\r\nm=")
		*out = (*out)[:0]
		for i := 1; i < len(sections); i++ {
			if strings.Contains(sections[i], "\r\na=ssrc:") {
				continue
			}
			ssrc := first + uint32(i)
			*out = append(*out, ssrc)
			sections[i] = strings.TrimSuffix(sections[i], "\r\n") + fmt.Sprintf("\r\na=ssrc:%d cname:{firefox}\r\n", ssrc)
		}
		return strings.Join(sections, "\r\nm=")
	}
}

// reportFromEachLine sends, for every SSRC, what Firefox sends from a line it
// only receives on: a receiver report from that SSRC with its SDES chunk.
func reportFromEachLine(c *client, ssrcs []uint32) {
	for _, ssrc := range ssrcs {
		_ = c.pc.WriteRTCP([]rtcp.Packet{
			&rtcp.ReceiverReport{SSRC: ssrc},
			&rtcp.SourceDescription{Chunks: []rtcp.SourceDescriptionChunk{{
				Source: ssrc,
				Items:  []rtcp.SourceDescriptionItem{{Type: rtcp.SDESCNAME, Text: "{firefox}"}},
			}}},
		})
	}
}

// TestFirefoxLikeViewerStaysConnected: a viewer whose answer declares an SSRC
// on each of 70 and of 200 m-lines and reports from each of them is honest,
// and its streams cost no buffer.
func TestFirefoxLikeViewerStaysConnected(t *testing.T) {
	for _, lines := range []int{70, 200} {
		t.Run(fmt.Sprint(lines), func(t *testing.T) { firefoxLikeViewer(t, lines) })
	}
}

func firefoxLikeViewer(t *testing.T, lines int) {
	s := newTestSFU(t)
	room := uuid.New()
	viewer := newClient(t)
	var declared []uint32
	viewer.answerTweak = firefoxLikeAnswer(0x60000000, &declared)
	viewer = joinMember(t, s, room, viewer)
	peer := viewer.peer
	// Lines the SFU sends on, offered in one more round. The signaling task
	// is held for the rest of the test, so no room round removes them.
	peer.signalingMu.Lock()
	defer peer.signalingMu.Unlock()
	for range lines - len(peer.PC.GetTransceivers()) {
		tr, err := peer.PC.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio,
			webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly})
		if err != nil {
			t.Fatal(err)
		}
		peer.rtcp.sentTrack(tr.Sender()) // as addSender does
	}
	if err := negotiateOffer(peer); err != nil {
		t.Fatal(err)
	}
	waitSFUCondition(t, "answer with every line", func() bool {
		sdp, _ := viewer.answerSDP.Load().(string)
		return strings.Count(sdp, "\r\nm=") == lines
	})
	time.Sleep(200 * time.Millisecond)
	ssrcs := append([]uint32(nil), declared...)
	for range 5 {
		reportFromEachLine(viewer, ssrcs)
		time.Sleep(100 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	unknown, buffered, quiet := peer.rtcp.counts()
	t.Logf("%d lines, %d declared SSRCs reporting: %d unknown, %d buffered, %d quiet streams",
		lines, len(ssrcs), unknown, buffered, quiet)
	if peer.PC.ConnectionState() != webrtc.PeerConnectionStateConnected {
		t.Fatalf("the Firefox-like viewer of %d lines was disconnected", lines)
	}
	if unknown != 0 || quiet < len(ssrcs) {
		t.Fatalf("%d unknown and %d quiet streams for %d declared SSRCs; want 0 and all of them quiet",
			unknown, quiet, len(ssrcs))
	}
}

// joinMember joins c and waits until its connection is up.
func joinMember(t *testing.T, s *SFU, room uuid.UUID, c *client) *client {
	t.Helper()
	c.join(s, room)
	waitSFUCondition(t, "member connected", func() bool {
		r := s.Room(room)
		return r != nil && r.GetPeer(c.id) != nil &&
			c.peer.PC.ConnectionState() == webrtc.PeerConnectionStateConnected
	})
	return c
}

// TestSubscriptionTogglesMakeQuietStreams: a viewer that watches and stops
// watching a screen share again and again, and keeps reporting on every
// stopped track, makes the SFU keep only quiet streams for them.
func TestSubscriptionTogglesMakeQuietStreams(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()
	publisher := newClient(t)
	publisher.publishVideo("toggle-screen")
	publisher = joinMember(t, s, room, publisher)
	viewer := joinMember(t, s, room, newClient(t))
	r := s.Room(room)
	peer := viewer.peer
	var retired []uint32
	const toggles = 150
	for range toggles {
		retired = append(retired, toggleScreen(t, r, viewer, publisher)...)
	}
	// What the toggles cost without reports is the SFU's and Pion's per
	// sender state (H-L8-05); what the reports add is the streams'.
	time.Sleep(300 * time.Millisecond)
	heapBase := heapAfterGC()
	for range 100 { // 200 reports on each stopped SSRC, more than a buffer holds
		for i := 0; i < len(retired); i += 31 {
			fillReports(viewer, retired[i:min(i+31, len(retired))], 2)
		}
	}
	time.Sleep(500 * time.Millisecond)
	grew := int64(heapAfterGC()) - int64(heapBase)
	unknown, buffered, quiet := peer.rtcp.counts()
	t.Logf("%d toggles, %d stopped SSRCs reported on: %d unknown, %d buffered, %d quiet streams, heap +%d bytes (%d per SSRC)",
		toggles, len(retired), unknown, buffered, quiet, grew, grew/int64(max(1, len(retired))))
	if peer.PC.ConnectionState() != webrtc.PeerConnectionStateConnected {
		t.Fatal("the toggling viewer was disconnected")
	}
	if unknown != 0 || quiet == 0 {
		t.Fatalf("%d unknown and %d quiet streams; want 0 and the stopped tracks' streams quiet", unknown, quiet)
	}
	// About 0.5 KB measured (2.3 KB under -race); a buffered one is 19 KB.
	if perSSRC := grew / int64(len(retired)); perSSRC > 4<<10 {
		t.Fatalf("each stopped SSRC reported on keeps %d bytes; a quiet stream keeps about 0.5 KB", perSSRC)
	}
}

// toggleScreen subscribes viewer to publisher's screen and off again and
// returns the SSRCs of the track the SFU stopped sending.
func toggleScreen(t *testing.T, r *Room, viewer, publisher *client) []uint32 {
	t.Helper()
	if err := r.Subscribe(viewer.id, publisher.id, SourceScreen, false, true); err != nil {
		t.Fatal(err)
	}
	var ssrcs []uint32
	waitSFUCondition(t, "screen sent", func() bool {
		ssrcs = sentSSRCs(viewer.peer.PC)
		return len(ssrcs) > 0
	})
	if err := r.Subscribe(viewer.id, publisher.id, SourceScreen, false, false); err != nil {
		t.Fatal(err)
	}
	waitSFUCondition(t, "screen stopped", func() bool { return len(sentSSRCs(viewer.peer.PC)) == 0 })
	return ssrcs
}

// sentSSRCs are the SSRCs (RTX included) of the tracks pc sends.
func sentSSRCs(pc *webrtc.PeerConnection) []uint32 {
	var out []uint32
	for _, sender := range pc.GetSenders() {
		if sender.Track() == nil {
			continue
		}
		for _, e := range sender.GetParameters().Encodings {
			out = append(out, uint32(e.SSRC))
			if e.RTX.SSRC != 0 {
				out = append(out, uint32(e.RTX.SSRC))
			}
		}
	}
	return out
}

// fillReports sends rounds receiver reports with a block for each SSRC.
func fillReports(c *client, ssrcs []uint32, rounds int) {
	rr := &rtcp.ReceiverReport{SSRC: 0x10000000}
	for _, ssrc := range ssrcs {
		rr.Reports = append(rr.Reports, rtcp.ReceptionReport{SSRC: ssrc})
	}
	for range rounds {
		_ = c.pc.WriteRTCP([]rtcp.Packet{rr})
	}
}
