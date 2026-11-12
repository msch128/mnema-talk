package sfu

import (
	"bytes"
	"os"
	"runtime"
	"runtime/pprof"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Cycle tests for the SFU (lane 8): rounds of a member joining a Talk over a
// real loopback connection, publishing a microphone that the resident member
// receives, and leaving. After the rounds and a settle wait the SFU's
// remote-keyed structures (inventory I6 rows 19-22, 28), the goroutine count
// and the heap in use must be back at the baseline. Every round is counted.
//
// A round costs a full ICE and DTLS handshake, so smoke runs a few dozen
// rounds; SFU_CYCLE_ROUNDS=n (or SFU_CYCLE_DEEP=1 for 2000) runs more with
// the same assertions.

func sfuCycleRounds(smoke int) int {
	if v := os.Getenv("SFU_CYCLE_ROUNDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	if os.Getenv("SFU_CYCLE_DEEP") == "1" {
		return 2000
	}
	return smoke
}

// sfuSizes reports the size of every per-room and per-SFU map.
func sfuSizes(s *SFU) map[string]int {
	out := map[string]int{}
	s.roomsMu.Lock()
	out["rooms"] = len(s.rooms)
	rooms := make([]*Room, 0, len(s.rooms))
	for _, r := range s.rooms {
		rooms = append(rooms, r)
	}
	s.roomsMu.Unlock()
	for _, r := range rooms {
		r.mu.RLock()
		out["peers"] += len(r.peers)
		out["trackLocals"] += len(r.trackLocals)
		out["latestSources"] += len(r.latestSources)
		out["groups"] += len(r.groups) + len(r.cameraGroups)
		out["cameraLayerMeta"] += len(r.cameraLayerMeta)
		out["subs"] += len(r.subs)
		out["lastMedia"] += len(r.lastMedia)
		out["lastViewers"] += len(r.lastViewers)
		out["layerMeta"] += len(r.layerMeta) + len(r.lastLayers)
		out["resumeBudgets"] += len(r.resumeBudgets)
		r.mu.RUnlock()
	}
	conns := 0
	s.conns.byPionID.Range(func(any, any) bool { conns++; return true })
	out["conns"] = conns
	s.probeMu.Lock()
	out["probes"], out["probesBy"] = s.probes, len(s.probesBy)
	s.probeMu.Unlock()
	return out
}

func sameSizes(a, b map[string]int) bool {
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func heapAfterGC() uint64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapInuse
}

// cycleOfferTimeout replaces the 10 s offer timeout in the cycle tests. The
// offer and connecting watchdogs (watchOffer, watchConnecting) are not stopped
// when a peer is removed, so each removed peer, with its connection, stays
// reachable until they fire: bounded retention, measured at about 1 MB per
// peer for offerTimeout. The settle wait below must outlast it.
const cycleOfferTimeout = time.Second

// assertSFUBaseline applies the three oracles after a settle wait and returns
// how much the heap in use grew. heapBound 0 skips the heap oracle (the caller
// applies its own).
func assertSFUBaseline(t *testing.T, s *SFU, base map[string]int, goBase int, heapBase, heapBound uint64, goSlack int) int64 {
	t.Helper()
	if heapBound == 0 {
		heapBound = 1 << 40
	}
	var sizes map[string]int
	gor := runtime.NumGoroutine()
	heap := heapAfterGC()
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		sizes, gor, heap = sfuSizes(s), runtime.NumGoroutine(), heapAfterGC()
		if sameSizes(sizes, base) && gor <= goBase+goSlack && heap <= heapBase+heapBound {
			break
		}
	}
	for k, v := range sizes {
		if v != base[k] {
			t.Errorf("sfu %s = %d after the cycle, baseline %d", k, v, base[k])
		}
	}
	if gor > goBase+goSlack {
		t.Errorf("goroutines = %d after the cycle, baseline %d (slack %d)", gor, goBase, goSlack)
		var buf bytes.Buffer
		_ = pprof.Lookup("goroutine").WriteTo(&buf, 1)
		t.Logf("goroutines by stack:\n%s", buf.String())
	}
	if path := os.Getenv("SFU_CYCLE_HEAPPROFILE"); path != "" {
		if f, err := os.Create(path); err == nil {
			runtime.GC()
			_ = pprof.WriteHeapProfile(f)
			_ = f.Close()
		}
	}
	if heap > heapBase+heapBound {
		t.Errorf("heap in use grew by %d bytes (bound %d)", heap-heapBase, heapBound)
	}
	t.Logf("goroutines %d (baseline %d), heap +%d bytes, sizes %v", gor, goBase, int64(heap)-int64(heapBase), sizes)
	return int64(heap) - int64(heapBase)
}

// sfuHeapBound is the heap growth a cycle may leave: Pion's and the NACK
// history's pools warm up by a fixed 3 to 6 MB whatever the number of rounds
// (the 2000-round room cycle stays inside it), so a leak shows only as growth
// past this, in the deep mode.
const sfuHeapBound = 16 << 20

// residentLeakRounds is from how many joiners on the resident's connection
// the per-joiner retention of H-L8-05 is asserted instead of the plain bound.
const residentLeakRounds = 1000

// joinAndLeave runs one round: a fresh member joins room publishing a
// microphone, the resident receives it, then the member is removed and its
// connection closed.
func joinAndLeave(t *testing.T, s *SFU, room uuid.UUID, resident *client) {
	t.Helper()
	c := newClient(t)
	c.publishAudio("mic")
	c.join(s, room)
	waitTrack(t, resident)
	s.RemovePeer(room, c.peer)
	c.release()
	// The resident's test client keeps what it receives in a buffered
	// channel; empty it so the harness itself does not hold packets.
	for {
		select {
		case <-resident.received:
			continue
		default:
		}
		break
	}
}

// TestSFUJoinLeaveCycleReturnsToBaseline keeps one resident in the Talk while
// members come and go.
func TestSFUJoinLeaveCycleReturnsToBaseline(t *testing.T) {
	if os.Getenv("SFU_CYCLE_HEAPPROFILE") != "" {
		runtime.MemProfileRate = 1024
	}
	rounds := sfuCycleRounds(40)
	s := newTestSFU(t)
	s.offerTimeout = cycleOfferTimeout
	room := uuid.New()
	resident := newClient(t)
	resident.publishAudio("mic")
	resident.join(s, room)
	joinAndLeave(t, s, room, resident) // warm-up: Pion's lazy goroutines
	time.Sleep(500 * time.Millisecond)
	base := sfuSizes(s)
	goBase, heapBase := runtime.NumGoroutine(), heapAfterGC()
	if base["peers"] != 1 {
		t.Fatalf("baseline has %d peers, want the resident only", base["peers"])
	}

	trBase, sdpBase := residentShape(resident)
	if path := os.Getenv("SFU_CYCLE_HEAPPROFILE"); path != "" {
		if f, err := os.Create(path + ".base"); err == nil {
			runtime.GC()
			_ = pprof.WriteHeapProfile(f)
			_ = f.Close()
		}
	}
	done := 0
	for range rounds {
		joinAndLeave(t, s, room, resident)
		done++
	}
	if done != rounds {
		t.Fatalf("only %d of %d rounds ran", done, rounds)
	}
	tr, sdp := residentShape(resident)
	t.Logf("resident connection: transceivers %d -> %d, local SDP %d -> %d bytes after %d joiners came and went", trBase, tr, sdpBase, sdp, rounds)
	if tr != trBase {
		t.Errorf("the resident's connection has %d transceivers, %d before", tr, trBase)
	}
	if rounds < residentLeakRounds {
		assertSFUBaseline(t, s, base, goBase, heapBase, sfuHeapBound, 8)
		t.Logf("rounds=%d joins=%d", rounds, done)
		return
	}
	// H-L8-05, inverse assertion (known failure): the resident's long-lived
	// connection keeps about 11 KB for every member who joined and left (the
	// sender-side state of each departed stream). It turns red when that
	// retention is gone; then drop this branch and keep the plain bound.
	grew := assertSFUBaseline(t, s, base, goBase, heapBase, 0, 8)
	perJoiner := grew / int64(rounds)
	t.Logf("rounds=%d joins=%d, heap retained per joiner %d bytes", rounds, done, perJoiner)
	if perJoiner < 4<<10 {
		t.Fatalf("H-L8-05 is fixed: %d bytes per joiner retained; drop the known-failure branch", perJoiner)
	}
}

// residentShape is the number of transceivers and the size of the local
// description of the resident's server-side connection.
func residentShape(c *client) (transceivers, sdpBytes int) {
	pc := c.peer.PC
	transceivers = len(pc.GetTransceivers())
	if d := pc.CurrentLocalDescription(); d != nil {
		sdpBytes = len(d.SDP)
	}
	return transceivers, sdpBytes
}

// TestSFURoomOpenCloseCycleReturnsToBaseline: each round two members open a
// Talk, hear each other and both leave, so the room itself must go.
func TestSFURoomOpenCloseCycleReturnsToBaseline(t *testing.T) {
	rounds := sfuCycleRounds(25)
	s := newTestSFU(t)
	s.offerTimeout = cycleOfferTimeout
	round := func() {
		room := uuid.New()
		a, b := newClient(t), newClient(t)
		a.publishAudio("mic")
		b.publishAudio("mic")
		a.join(s, room)
		b.join(s, room)
		waitTrack(t, a)
		waitTrack(t, b)
		s.RemovePeer(room, a.peer)
		s.RemovePeer(room, b.peer)
		a.release()
		b.release()
	}
	round()
	time.Sleep(500 * time.Millisecond)
	base := sfuSizes(s)
	goBase, heapBase := runtime.NumGoroutine(), heapAfterGC()

	done := 0
	for range rounds {
		round()
		done++
	}
	if done != rounds {
		t.Fatalf("only %d of %d rounds ran", done, rounds)
	}
	_ = assertSFUBaseline(t, s, base, goBase, heapBase, sfuHeapBound, 8)
	t.Logf("rounds=%d rooms opened and closed=%d", rounds, done)
}
