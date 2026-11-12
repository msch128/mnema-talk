package sfu

import (
	"os"
	"runtime"
	"runtime/pprof"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pion/rtcp"
)

// Lane 8 (shape 7: bounded in the test, unbounded outside): a member's
// server-side SFU connection keeps one Pion SRTCP read stream for every
// distinct media SSRC it names in authenticated SRTCP that the SFU never
// opened, with no cap on the number of streams.
//
// When a peer's PeerConnection receives SRTCP whose destination SSRC was not
// declared in the SDP, Pion's PeerConnection.undeclaredRTCPMediaProcessor
// (webrtc/v4@v4.2.26 peerconnection.go:2327) accepts the stream, logs
// "Incoming unhandled RTCP ssrc(...)" and appends it to a slice closed only
// when the PeerConnection closes; the srtp session also keeps it in its
// readStreams map (srtp/v3@v3.1.3 session.go:72). Each stream is a
// *srtp.ReadStreamSRTCP with a packetio.Buffer (capped at 100 KB each,
// srtcpBufferSize) and a deadline. The per-buffer size is bounded; the number
// of buffers is not. The SFU builds its API with the default SettingEngine
// (internal/sfu/sfu_setup.go:buildAPI): no BufferFactory, no per-connection
// SSRC cap, so nothing on our side bounds the count either.
//
// RTCP is authenticated with the member's own negotiated SRTCP keys, so an
// ordinary member (no special rights) can send a report that names any
// destination SSRC: Pion's srtp.SessionSRTCP.decrypt demuxes by the
// destination SSRC of each report (session_srtcp.go:216) and creates a new
// read stream per unseen one. A member who keeps one voice connection open and
// streams receiver reports for fresh SSRCs grows the shared SFU process's heap
// by about 2.5 KB per SSRC without bound, until its own connection ends.
//
// This is a known failure written as an inverse assertion: the test asserts
// that the growth is present and UNCAPPED, so it passes while the defect
// stands and turns red the day a cap or a drain bounds the streams. When it
// turns red, drop this test and keep a positive bound instead. See
// ~/mnema-hunt/findings/H-L8-07.md.

// floodUnhandledSRTCP makes c send n distinct media SSRCs the SFU never opened,
// as authenticated SRTCP picture-loss reports, and returns the count sent.
func floodUnhandledSRTCP(t *testing.T, c *client, firstSSRC uint32, n int) int {
	t.Helper()
	const perPacket = 50
	pkts := make([]rtcp.Packet, 0, perPacket)
	sent := 0
	flush := func() {
		if len(pkts) == 0 {
			return
		}
		if err := c.pc.WriteRTCP(pkts); err != nil {
			t.Fatalf("WriteRTCP: %v", err)
		}
		sent += len(pkts)
		pkts = pkts[:0]
		time.Sleep(time.Millisecond)
	}
	for i := 0; i < n; i++ {
		pkts = append(pkts, &rtcp.PictureLossIndication{
			SenderSSRC: 0x10000000,
			MediaSSRC:  firstSSRC + uint32(i),
		})
		if len(pkts) == perPacket {
			flush()
		}
	}
	flush()
	return sent
}

// ssrcFloodRounds is the number of distinct SSRCs each of the two batches
// sends. SFU_SSRC_FLOOD_N overrides it; the deep gate raises it to 50 000 so
// the nightly job demonstrates the quarter-gigabyte scale.
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

func TestUnhandledSRTCPStreamsGrowWithoutCap(t *testing.T) {
	if os.Getenv("SFU_CYCLE_HEAPPROFILE") != "" {
		runtime.MemProfileRate = 1024
	}
	batch := ssrcFloodBatch(1500)
	s := newTestSFU(t)
	room := uuid.New()
	// A lone member: no resident, no victim. It floods its own connection, so
	// the growth is on the member's own server-side peer and the only shared
	// cost is the SFU process's heap.
	member := newClient(t)
	member.publishAudio("mic")
	member.join(s, room)
	waitSFUCondition(t, "member connected", func() bool {
		r := s.Room(room)
		return r != nil && r.GetPeer(member.id) != nil &&
			member.peer.PC.ConnectionState().String() == "connected"
	})
	time.Sleep(300 * time.Millisecond)

	heapBase := heapAfterGC()
	goBase := runtime.NumGoroutine()

	// First batch.
	n1 := floodUnhandledSRTCP(t, member, 0x40000000, batch)
	time.Sleep(time.Second)
	heap1 := heapAfterGC()
	grow1 := int64(heap1) - int64(heapBase)

	// Second batch of the same size, disjoint SSRCs: if a cap or an eviction
	// bounded the streams, the Nth stream would be free and this batch would
	// cost nothing. It does not.
	n2 := floodUnhandledSRTCP(t, member, 0x80000000, batch)
	time.Sleep(time.Second)
	heap2 := heapAfterGC()
	grow2 := int64(heap2) - int64(heapBase)
	growSecond := grow2 - grow1
	goNow := runtime.NumGoroutine()

	perSSRC := grow2 / int64(n1+n2)
	t.Logf("sent %d + %d unhandled SRTCP SSRCs on one member connection: "+
		"heap +%d then +%d bytes (second batch +%d), %d B/SSRC, goroutines %d -> %d",
		n1, n2, grow1, grow2, growSecond, perSSRC, goBase, goNow)

	if path := os.Getenv("SFU_CYCLE_HEAPPROFILE"); path != "" {
		if f, err := os.Create(path); err == nil {
			runtime.GC()
			_ = pprof.WriteHeapProfile(f)
			_ = f.Close()
		}
	}

	// The perturbation must have happened, or the test could pass by doing
	// nothing: creating ~1500 SRTCP read streams must cost real heap.
	const minPerturbation = 512 << 10
	if grow1 < minPerturbation {
		t.Fatalf("first batch of %d SSRCs grew the heap by only %d bytes; "+
			"the flood did not reach the SFU, the measurement is void", n1, grow1)
	}
	// It is not a per-connection goroutine leak: Pion runs one
	// undeclaredRTCPMediaProcessor per connection, not one per stream.
	if goNow > goBase+8 {
		t.Errorf("goroutines grew from %d to %d under the SSRC flood; expected a "+
			"flat count (the growth must be heap, not goroutines)", goBase, goNow)
	}

	// Inverse assertion (known failure). The second, disjoint batch costs about
	// as much heap as the first: the streams are uncapped, so every further
	// SSRC keeps allocating. The day a per-connection cap or a drain of
	// unhandled streams lands, growSecond collapses and this fails: then delete
	// this test and assert the cap instead.
	const perSSRCFloor = 1 << 10 // 1 KB; measured ~2.5 KB
	if growSecond < int64(n2)*perSSRCFloor {
		t.Fatalf("the second batch of %d disjoint SSRCs cost only %d bytes "+
			"(%d B/SSRC); unhandled SRTCP streams now look bounded. The leak "+
			"appears fixed: drop this inverse assertion and assert the cap "+
			"(see H-L8-07).", n2, growSecond, growSecond/int64(n2))
	}
}
