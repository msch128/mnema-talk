package sfu

import (
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/metrics"
	"github.com/pion/logging"
	"github.com/pion/rtcp"
	"github.com/pion/transport/v5/packetio"
	"github.com/pion/webrtc/v4"
)

// Lane 8 (shape 7), H-L8-07: Pion keeps one SRTCP read stream, with a buffer,
// for every SSRC an authenticated RTCP packet of the member names that nothing
// on the connection reads (PLI, NACK, FIR, REMB: the media SSRC; a receiver
// report: each of its up to 31 blocks), until the connection closes. Before
// the fix a member grew the SFU's heap by about 2.7 KB per such SSRC without
// a cap, and by about 320 KB per SSRC it kept sending to (the 100 KB buffer
// nobody reads, plus a queue slot per packet). The SFU now caps what one stream
// holds, gives the streams of SSRCs it does not read no buffer, and closes a
// connection that names too many unknown SSRCs (rtcpStreamCap).

// The heap the flood tests allow: what one stream may hold when it is filled
// (measured about 20 KB), and what a flooded connection may cost (65 streams
// of one report each, about 2.7 KB apiece, and those Pion makes while the
// connection closes, which get no buffer). Without the fix the smoke batch
// alone costs about 4 MB, the deep one about 130 MB per vector.
const (
	maxBytesPerFilledStream = 32 << 10
	maxBytesPerFloodedConn  = 2 << 20
)

// ssrcFloodBatch is the number of distinct SSRCs each of the two batches
// sends. SFU_SSRC_FLOOD_N overrides it; the deep gate raises it to 50 000.
func ssrcFloodBatch(smoke int) int {
	if v := os.Getenv("SFU_SSRC_FLOOD_N"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	if os.Getenv("SFU_SSRC_FLOOD_DEEP") == "1" || os.Getenv("SFU_CYCLE_DEEP") == "1" {
		return 50000
	}
	return smoke
}

// floodPLIs makes c name n distinct SSRCs from first on in picture-loss
// reports, 50 to a packet, and returns how many it sent before its own
// connection stopped writing.
func floodPLIs(c *client, first uint32, n int) int {
	pkts := make([]rtcp.Packet, 0, 50)
	sent := 0
	for i := 0; i < n; i += 50 {
		pkts = pkts[:0]
		for j := i; j < min(i+50, n); j++ {
			pkts = append(pkts, &rtcp.PictureLossIndication{SenderSSRC: 0x10000000, MediaSSRC: first + uint32(j)})
		}
		if c.pc.WriteRTCP(pkts) != nil {
			return sent
		}
		sent += len(pkts)
	}
	return sent
}

// floodReports is floodPLIs with receiver reports of 31 blocks each, the
// cheapest vector, in a compound packet with a picture-loss report.
func floodReports(c *client, first uint32, n int) int {
	sent := 0
	for i := 0; i < n; i += 32 {
		rr := &rtcp.ReceiverReport{SSRC: 0x10000000}
		for j := i; j < min(i+31, n); j++ {
			rr.Reports = append(rr.Reports, rtcp.ReceptionReport{SSRC: first + uint32(j)})
		}
		pkts := []rtcp.Packet{rr}
		if i+31 < n {
			pkts = append(pkts, &rtcp.PictureLossIndication{SenderSSRC: 0x10000000, MediaSSRC: first + uint32(i+31)})
		}
		if c.pc.WriteRTCP(pkts) != nil {
			return sent
		}
		sent += min(32, n-i)
	}
	return sent
}

// joinLoneMember connects one member who publishes a microphone and nothing
// else: no resident, no victim.
func joinLoneMember(t *testing.T, s *SFU, room uuid.UUID) *client {
	t.Helper()
	member := newClient(t)
	member.publishAudio("mic")
	member.join(s, room)
	waitSFUCondition(t, "member connected", func() bool {
		r := s.Room(room)
		return r != nil && r.GetPeer(member.id) != nil &&
			member.peer.PC.ConnectionState() == webrtc.PeerConnectionStateConnected
	})
	time.Sleep(300 * time.Millisecond)
	return member
}

// TestUnhandledSRTCPStreamsAreCapped floods two member connections with
// batch distinct unknown SSRCs each, one by picture-loss reports and one by
// 31-block receiver reports in compound packets: each connection is closed
// past the cap and costs the heap a bounded amount, however long the flood.
func TestUnhandledSRTCPStreamsAreCapped(t *testing.T) {
	if os.Getenv("SFU_CYCLE_HEAPPROFILE") != "" {
		runtime.MemProfileRate = 1024
	}
	batch := ssrcFloodBatch(1500)
	s := newTestSFU(t)
	closedBefore := metrics.SFUIngressDropped.With("rtcp_ssrcs").Load()
	floods := []struct {
		name  string
		flood func(*client, uint32, int) int
	}{{"picture-loss reports", floodPLIs}, {"31-block receiver reports", floodReports}}
	for _, f := range floods {
		room := uuid.New()
		member := joinLoneMember(t, s, room)
		peer := member.peer
		heapBase := heapAfterGC()
		sent := f.flood(member, 0x40000000, batch)
		time.Sleep(time.Second)
		grew := int64(heapAfterGC()) - int64(heapBase)
		unknown, buffered, _ := peer.rtcp.counts()
		t.Logf("%s: named %d of %d unknown SSRCs before the connection stopped; Pion kept %d unknown "+
			"streams, %d with a buffer, heap +%d bytes", f.name, sent, batch, unknown, buffered, grew)
		writeFloodProfile()
		if grew > maxBytesPerFloodedConn {
			t.Fatalf("%s: one flooding connection grew the heap by %d bytes, over the %d it may cost; "+
				"unhandled SRTCP streams are not capped (H-L8-07)", f.name, grew, maxBytesPerFloodedConn)
		}
		if unknown <= maxUnknownRTCPStreams {
			t.Fatalf("%s: only %d unknown SSRCs reached the SFU; the flood did not get past the cap, the test is void", f.name, unknown)
		}
		waitSFUCondition(t, "flooding connection closed", func() bool {
			r := s.Room(room)
			return peer.PC.ConnectionState() == webrtc.PeerConnectionStateClosed && (r == nil || r.GetPeer(member.id) == nil)
		})
	}
	if got := metrics.SFUIngressDropped.With("rtcp_ssrcs").Load() - closedBefore; got != 2 {
		t.Errorf("rtcp_ssrcs closures counted %d times, want once per connection (2)", got)
	}
}

func writeFloodProfile() {
	if path := os.Getenv("SFU_CYCLE_HEAPPROFILE"); path != "" {
		if f, err := os.Create(path); err == nil {
			runtime.GC()
			_ = pprof.WriteHeapProfile(f)
			_ = f.Close()
		}
	}
}

// TestFilledUnhandledSRTCPStreamsStaySmall fills as many unread streams as the
// cap lets one connection keep, each with far more than it may hold.
func TestFilledUnhandledSRTCPStreamsStaySmall(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()
	member := joinLoneMember(t, s, room)
	heapBase := heapAfterGC()

	const first, streams = 0x40000000, maxUnknownRTCPStreams - 2
	// 200 packets to each SSRC: more than the 128 a stream may queue, and
	// the receiver reports (31 blocks, 252 bytes) more than its 16 KB.
	for range 100 {
		floodPLIs(member, first, streams)
		floodReports(member, first, streams)
	}
	time.Sleep(time.Second)
	grew := int64(heapAfterGC()) - int64(heapBase)
	t.Logf("%d unread streams filled: heap +%d bytes, %d per stream", streams, grew, grew/streams)
	if member.peer.PC.ConnectionState() != webrtc.PeerConnectionStateConnected {
		t.Fatalf("a connection at %d unknown SSRCs was closed; the cap is %d", streams, maxUnknownRTCPStreams)
	}
	if grew > streams*maxBytesPerFilledStream {
		t.Fatalf("%d filled streams grew the heap by %d bytes (%d per stream), over %d per stream",
			streams, grew, grew/streams, maxBytesPerFilledStream)
	}
}

// counts reads the cap's counters.
func (c *rtcpStreamCap) counts() (unknown, buffered, quiet int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.unknown), c.buffered, c.quiet
}

// TestRTCPStreamCapHoldsWithoutPionsLogLine stands for a Pion whose log line
// changed: the buffer factory alone still closes a flooding connection.
func TestRTCPStreamCapHoldsWithoutPionsLogLine(t *testing.T) {
	pionUnhandledRTCPChanged.Store(true)
	t.Cleanup(func() { pionUnhandledRTCPChanged.Store(false) })
	s := newTestSFU(t)
	room := uuid.New()
	member := joinLoneMember(t, s, room)
	peer := member.peer
	heapBase := heapAfterGC()
	sent := floodPLIs(member, 0x40000000, 4*maxBufferedRTCPStreams)
	time.Sleep(time.Second)
	grew := int64(heapAfterGC()) - int64(heapBase)
	unknown, buffered, _ := peer.rtcp.counts()
	t.Logf("named %d unknown SSRCs: %d counted by the log, %d buffered streams, heap +%d bytes", sent, unknown, buffered, grew)
	if unknown != 0 {
		t.Fatalf("the log hook counted %d streams; the test must run without it", unknown)
	}
	waitSFUCondition(t, "flooding connection closed", func() bool {
		r := s.Room(room)
		return peer.PC.ConnectionState() == webrtc.PeerConnectionStateClosed && (r == nil || r.GetPeer(member.id) == nil)
	})
	if limit := int64(maxBufferedRTCPStreams) * 4 << 10; grew > limit {
		t.Fatalf("heap grew by %d bytes, over %d for %d buffered streams", grew, limit, maxBufferedRTCPStreams)
	}
}

// TestRTCPStreamCapSortsStreams checks, on a bare connection that sends one
// track, which streams get a buffer and which count against which cap.
func TestRTCPStreamCapSortsStreams(t *testing.T) {
	c, sentSSRC := newTestRTCPCap(t)
	c.declare("v=0\r\nm=audio 9 UDP/TLS/RTP/SAVPF 111\r\na=ssrc:7 cname:x\r\na=ssrc:7 msid:a b\r\n" +
		"a=ssrc:8 cname:x\r\na=ssrc:9 cname:x\r\na=ssrc:10 cname:x\r\na=ssrc:11 cname:x\r\n")
	if _, ok := c.buffer(packetio.RTCPBufferPacket, 7).(*quietBuffer); !ok {
		t.Error("a declared SSRC got a buffer")
	}
	if _, ok := c.buffer(packetio.RTCPBufferPacket, 11).(*rtcpBuffer); !ok {
		t.Error("the fifth SSRC of one m-line was taken as declared")
	}
	sent := c.buffer(packetio.RTCPBufferPacket, sentSSRC)
	if _, ok := sent.(*rtcpBuffer); !ok {
		t.Fatal("the SSRC of a track the SFU sends got no buffer")
	}
	_ = sent.Close() // the SFU stopped sending it
	if _, ok := c.buffer(packetio.RTCPBufferPacket, sentSSRC).(*quietBuffer); !ok {
		t.Error("the late reports of a stopped track got a buffer")
	}
	for _, ssrc := range []uint32{7, 8, sentSSRC, 12} {
		c.unhandled(ssrc)
	}
	// SSRC 12 reported before the answer that adds its line.
	c.declare("v=0\r\nm=audio 9 UDP/TLS/RTP/SAVPF 111\r\na=ssrc:7 cname:x\r\n" +
		"m=audio 9 UDP/TLS/RTP/SAVPF 111\r\na=ssrc:12 cname:x\r\n")
	if unknown, buffered, quiet := c.counts(); unknown != 0 || buffered != 1 || quiet != 2 {
		t.Fatalf("unknown %d, buffered %d, quiet %d; want 0, 1 (SSRC 11), 2", unknown, buffered, quiet)
	}
	for i := range maxUnknownRTCPStreams {
		c.unhandled(uint32(1000 + i))
	}
	if c.closing.Load() {
		t.Fatalf("closed at %d unknown streams", maxUnknownRTCPStreams)
	}
	c.unhandled(5000)
	if !c.closing.Load() {
		t.Fatalf("still open past %d unknown streams", maxUnknownRTCPStreams)
	}
	if _, ok := c.buffer(packetio.RTCPBufferPacket, 6000).(closedBuffer); !ok {
		t.Error("a stream made while the connection closes got a buffer")
	}
	pc := c.pc.Load()
	waitSFUCondition(t, "connection closed", func() bool {
		return pc.ConnectionState() == webrtc.PeerConnectionStateClosed
	})
}

// TestDeclaringReportedSSRCsDoesNotLaunderThem is the reviewer's probe: a
// client that reports from 32 unknown SSRCs and then sends an answer that
// declares them, round after round, gets at most one forgiven per new
// m-line, so it is closed near the unknown cap and not at the buffered one.
func TestDeclaringReportedSSRCsDoesNotLaunderThem(t *testing.T) {
	c, _ := newTestRTCPCap(t)
	const lines, perRound = 8, 32
	ssrc := uint32(0x40000000)
	rounds := 0
	for ; rounds < 40 && !c.closing.Load(); rounds++ {
		var sdp strings.Builder
		sdp.WriteString("v=0\r\n")
		for i := range perRound {
			_ = c.buffer(packetio.RTCPBufferPacket, ssrc)
			c.unhandled(ssrc)
			if i%(perRound/lines) == 0 {
				sdp.WriteString("m=audio 9 UDP/TLS/RTP/SAVPF 111\r\n")
			}
			fmt.Fprintf(&sdp, "a=ssrc:%d cname:x\r\n", ssrc)
			ssrc++
		}
		c.declare(sdp.String())
	}
	unknown, buffered, _ := c.counts()
	t.Logf("closed after %d rounds: %d unknown, %d buffered streams", rounds, unknown, buffered)
	if !c.closing.Load() {
		t.Fatalf("still open after %d rounds, %d buffered streams", rounds, buffered)
	}
	if limit := maxUnknownRTCPStreams + lines + perRound; buffered > limit {
		t.Fatalf("%d buffered streams before the close, over %d", buffered, limit)
	}
}

// newTestRTCPCap is the cap of a connection that sends one track, with the
// SSRC of that track.
func newTestRTCPCap(t *testing.T) (*rtcpStreamCap, uint32) {
	t.Helper()
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	track, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus}, "a", "s")
	if err != nil {
		t.Fatal(err)
	}
	sender, err := pc.AddTrack(track)
	if err != nil {
		t.Fatal(err)
	}
	c := newRTCPStreamCap(logging.NewDefaultLoggerFactory(), joinLogger(uuid.New(), ""))
	c.bind(pc)
	c.sentTrack(sender)
	return c, uint32(sender.GetParameters().Encodings[0].SSRC)
}
