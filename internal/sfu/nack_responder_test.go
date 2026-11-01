package sfu

import (
	"encoding/binary"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/metrics"
	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
)

func TestNackWindowAndBudgetShrinkWithLoad(t *testing.T) {
	for _, tc := range []struct {
		peers  int64
		window time.Duration
		conn   int
	}{
		{0, time.Second, 4 << 20}, {16, time.Second, 4 << 20}, {17, 750 * time.Millisecond, 2 << 20},
		{32, 750 * time.Millisecond, 2 << 20}, {33, 500 * time.Millisecond, 1 << 20},
		{64, 500 * time.Millisecond, 1 << 20}, {65, 250 * time.Millisecond, 512 << 10}, {1000, 250 * time.Millisecond, 512 << 10},
	} {
		if got := nackWindow(tc.peers); got != tc.window {
			t.Errorf("nackWindow(%d) = %v, want %v", tc.peers, got, tc.window)
		}
		if got := nackConnBytes(tc.peers); got != tc.conn {
			t.Errorf("nackConnBytes(%d) = %d, want %d", tc.peers, got, tc.conn)
		}
	}
}

// clock is a settable time for the responder.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// wire collects what a responder writes below itself.
type wire struct {
	mu   sync.Mutex
	sent []rtp.Packet
	fail bool
}

func (w *wire) Write(h *rtp.Header, payload []byte, _ interceptor.Attributes) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fail {
		return 0, errTelemetryTest
	}
	w.sent = append(w.sent, rtp.Packet{Header: h.Clone(), Payload: append([]byte(nil), payload...)})
	return len(payload), nil
}

func (w *wire) take() []rtp.Packet {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := w.sent
	w.sent = nil
	return out
}

func videoInfo(ssrc uint32, rtx bool) *interceptor.StreamInfo {
	info := &interceptor.StreamInfo{SSRC: ssrc, RTCPFeedback: []interceptor.RTCPFeedback{{Type: "nack"}, {Type: "nack", Parameter: "pli"}}}
	if rtx {
		info.SSRCRetransmission, info.PayloadTypeRetransmission = ssrc+1, 97
	}
	return info
}

func newResponder(t *testing.T, load *peerLoad) (*nackResponder, *clock) {
	t.Helper()
	clk := &clock{t: time.Unix(1_000_000, 0)}
	icp, err := nackResponderFactory{load: load, now: clk.now}.NewInterceptor("")
	if err != nil {
		t.Fatal(err)
	}
	return icp.(*nackResponder), clk
}

func send(w interceptor.RTPWriter, ssrc uint32, seq uint16, size int) {
	payload := make([]byte, size)
	binary.BigEndian.PutUint16(payload, seq)
	_, _ = w.Write(&rtp.Header{Version: 2, SSRC: ssrc, SequenceNumber: seq, Timestamp: uint32(seq) * 3000}, payload, nil)
}

// nackFor feeds a NACK for seqs to the responder as the connection would read it.
func nackFor(t *testing.T, n *nackResponder, ssrc uint32, seqs ...uint16) {
	t.Helper()
	var pairs []rtcp.NackPair
	for _, s := range seqs {
		pairs = append(pairs, rtcp.NackPair{PacketID: s})
	}
	raw, err := (&rtcp.TransportLayerNack{SenderSSRC: 1, MediaSSRC: ssrc, Nacks: pairs}).Marshal()
	if err != nil {
		t.Fatal(err)
	}
	nackRaw(n, raw)
}

// nackRaw feeds an encoded NACK to the responder and waits until it is answered.
func nackRaw(n *nackResponder, raw []byte) {
	var wg sync.WaitGroup
	n.afterResend = wg.Done
	wg.Add(1)
	reader := n.BindRTCPReader(interceptor.RTCPReaderFunc(func(b []byte, a interceptor.Attributes) (int, interceptor.Attributes, error) {
		return copy(b, raw), a, nil
	}))
	if _, _, err := reader.Read(make([]byte, 4096), nil); err != nil {
		panic(err)
	}
	wg.Wait()
}

func TestResponderResendsWhatIsRemembered(t *testing.T) {
	n, clk := newResponder(t, nil)
	w := &wire{}
	out := n.BindLocalStream(videoInfo(10, false), w)
	for seq := uint16(100); seq < 140; seq++ {
		send(out, 10, seq, 900)
	}
	w.take()
	clk.advance(time.Second)
	nackFor(t, n, 10, 105, 139, 5000)
	got := w.take()
	if len(got) != 2 || got[0].SequenceNumber != 105 || got[1].SequenceNumber != 139 || got[0].SSRC != 10 {
		t.Fatalf("resent %+v", got)
	}
	if binary.BigEndian.Uint16(got[0].Payload) != 105 || len(got[0].Payload) != 900 {
		t.Fatal("resent payload differs")
	}
	// Asked again at once: a packet is not resent within a round trip.
	nackFor(t, n, 10, 105)
	if len(w.take()) != 0 {
		t.Fatal("resent twice within the interval")
	}
	clk.advance(time.Second)
	nackFor(t, n, 10, 105)
	if len(w.take()) != 1 {
		t.Fatal("not resent after the interval")
	}
}

func TestResponderResendsOnTheRetransmissionStream(t *testing.T) {
	n, clk := newResponder(t, nil)
	w := &wire{}
	out := n.BindLocalStream(videoInfo(20, true), w)
	send(out, 20, 7, 300)
	w.take()
	clk.advance(time.Second)
	nackFor(t, n, 20, 7)
	got := w.take()
	if len(got) != 1 || got[0].SSRC != 21 || got[0].PayloadType != 97 {
		t.Fatalf("resent %+v", got)
	}
	if binary.BigEndian.Uint16(got[0].Payload) != 7 || len(got[0].Payload) != 302 {
		t.Fatal("the original sequence number is not in front of the payload")
	}
}

func TestResponderIgnoresStreamsWithoutNackAndForeignSSRCs(t *testing.T) {
	n, _ := newResponder(t, nil)
	w := &wire{}
	audio := n.BindLocalStream(&interceptor.StreamInfo{SSRC: 5}, w)
	send(audio, 5, 1, 100)
	if len(w.take()) != 1 || len(n.streams) != 0 {
		t.Fatal("an audio stream was remembered")
	}
	out := n.BindLocalStream(videoInfo(30, false), w)
	send(out, 31, 1, 100) // not the stream's own SSRC
	if n.streams[30].history.retained() != 0 {
		t.Fatal("a foreign SSRC was remembered")
	}
	nackFor(t, n, 99, 1) // a stream it does not know
	if len(w.take()) != 1 {
		t.Fatal("unexpected resend")
	}
}

func TestAFailedResendMayBeAskedForAgain(t *testing.T) {
	n, clk := newResponder(t, nil)
	w := &wire{}
	out := n.BindLocalStream(videoInfo(40, false), w)
	send(out, 40, 1, 100)
	w.take()
	clk.advance(time.Second)
	w.fail = true
	nackFor(t, n, 40, 1)
	w.fail = false
	nackFor(t, n, 40, 1)
	if len(w.take()) != 1 {
		t.Fatal("the failed resend blocked the next one")
	}
}

func TestResponderLearnsTheRoundTripFromReceiverReports(t *testing.T) {
	n, clk := newResponder(t, nil)
	n.BindLocalStream(videoInfo(50, false), &wire{})
	now := clk.now()
	rr := &rtcp.ReceiverReport{SSRC: 1, Reports: []rtcp.ReceptionReport{
		{SSRC: 50, LastSenderReport: ntpMiddle32(now.Add(-300 * time.Millisecond)), Delay: 0},
		{SSRC: 77}, // a stream it does not know
	}}
	raw, _ := rr.Marshal()
	reader := n.BindRTCPReader(interceptor.RTCPReaderFunc(func(b []byte, a interceptor.Attributes) (int, interceptor.Attributes, error) {
		return copy(b, raw), a, nil
	}))
	if _, _, err := reader.Read(make([]byte, 1500), nil); err != nil {
		t.Fatal(err)
	}
	s := n.streams[50]
	s.mu.Lock()
	rtt := s.rtt
	s.mu.Unlock()
	if rtt < 250*time.Millisecond || rtt > 350*time.Millisecond {
		t.Fatalf("rtt %v, want about 300 ms", rtt)
	}
	// Garbage and short reads pass through.
	short := n.BindRTCPReader(interceptor.RTCPReaderFunc(func(b []byte, a interceptor.Attributes) (int, interceptor.Attributes, error) {
		return 0, a, nil
	}))
	if _, _, err := short.Read(make([]byte, 8), nil); err != nil {
		t.Fatal(err)
	}
	n.noteRTT([]byte{1, 2, 3})
}

func TestUnbindAndCloseForgetTheHistory(t *testing.T) {
	n, _ := newResponder(t, nil)
	a := videoInfo(60, false)
	b := videoInfo(61, false)
	n.BindLocalStream(a, &wire{})
	out := n.BindLocalStream(b, &wire{})
	send(out, 61, 1, 500)
	n.UnbindLocalStream(a)
	n.UnbindLocalStream(a) // twice is fine
	if len(n.streams) != 1 {
		t.Fatalf("%d streams after unbinding one", len(n.streams))
	}
	s := n.streams[61]
	if err := n.Close(); err != nil || len(n.streams) != 0 || s.history.retained() != 0 {
		t.Fatalf("close left %d streams and %d bytes", len(n.streams), s.history.retained())
	}
}

func TestEarlyJoinersDoNotKeepTheLargeHistory(t *testing.T) {
	load := &peerLoad{}
	load.peers.Store(1)
	n, clk := newResponder(t, load)
	out := n.BindLocalStream(videoInfo(70, false), &wire{})
	feed := func(from uint16, packets int) {
		for i := 0; i < packets; i++ {
			send(out, 70, from+uint16(i), 1100)
			clk.advance(7 * time.Millisecond) // about 1.2 MB/s
		}
	}
	feed(0, 1500)
	alone := n.streams[70].history.retained()
	load.peers.Store(100)
	feed(1500, 1500)
	crowded := n.streams[70].history.retained()
	if alone <= 2*crowded {
		t.Fatalf("history was %d bytes with one peer and %d with a hundred: it did not shrink", alone, crowded)
	}
	if crowded > 600<<10 {
		t.Fatalf("%d bytes kept for one track with a hundred peers", crowded)
	}
}

type discard struct{}

func (discard) Write(_ *rtp.Header, payload []byte, _ interceptor.Attributes) (int, error) {
	return len(payload), nil
}

func heapInUse() uint64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

// A hundred members each watching three cameras of 1.5 Mbit/s: the heap the
// histories take, measured on synthetic connections, against the 1024 buffers
// of 1.5 KB the fixed history kept for every one of them.
func TestHistoryMemoryOfAFullRoom(t *testing.T) {
	const members, cameras, pps = 100, 3, 150
	load := &peerLoad{}
	load.peers.Store(members)
	clk := &clock{t: time.Unix(1_000_000, 0)}
	before := heapInUse()
	var writers [members][cameras]interceptor.RTPWriter
	var responders [members]*nackResponder
	for m := range responders {
		icp, _ := nackResponderFactory{load: load, now: clk.now}.NewInterceptor("")
		responders[m] = icp.(*nackResponder)
		for c := 0; c < cameras; c++ {
			writers[m][c] = responders[m].BindLocalStream(videoInfo(uint32(c+1), true), discard{})
		}
	}
	for step := 0; step < 10*pps; step++ {
		for m := range responders {
			for c := 0; c < cameras; c++ {
				send(writers[m][c], uint32(c+1), uint16(step), 1000)
			}
		}
		clk.advance(time.Second / pps)
	}
	heap := int64(heapInUse()) - int64(before)
	counted := 0
	for _, r := range responders {
		for _, s := range r.streams {
			counted += s.history.retained()
		}
	}
	runtime.KeepAlive(responders)
	runtime.KeepAlive(writers)
	fixed := members * cameras * 1024 * 1500
	t.Logf("histories take %.1f MB of heap (%.1f MB counted); the fixed history holds %.1f MB", float64(heap)/1e6, float64(counted)/1e6, float64(fixed)/1e6)
	if heap*5 > int64(fixed) {
		t.Fatalf("histories take %d bytes of heap, more than a fifth of the fixed %d", heap, fixed)
	}
	// The accounting is exact: the slots' sizes plus the rings, summed here
	// independently of the history's own counter.
	summed := 0
	for _, r := range responders {
		for _, st := range r.streams {
			h := &st.history
			h.mu.Lock()
			for i := range h.ring {
				if h.ring[i].buf != nil {
					summed += int(h.ring[i].size)
				}
			}
			summed += len(h.ring) * nackSlotBytes
			h.mu.Unlock()
		}
	}
	if summed != counted {
		t.Fatalf("the history counts %d bytes but its slots and rings sum to %d", counted, summed)
	}
	// What the heap shows depends on the runtime (pooled buffers may sit in a
	// collected pool): it only has to stay near what is counted, never above.
	if heap > int64(counted)*4 {
		t.Fatalf("the heap grew by %d bytes for %d counted", heap, counted)
	}
}

// The camera's and the screen's NACKs in one batch are both answered.
func TestTwoNacksInOneBatchAreBothAnswered(t *testing.T) {
	n, clk := newResponder(t, nil)
	w := &wire{}
	cam := n.BindLocalStream(videoInfo(200, false), w)
	screen := n.BindLocalStream(videoInfo(201, false), w)
	send(cam, 200, 1, 100)
	send(screen, 201, 1, 100)
	w.take()
	clk.advance(time.Second)
	raw, err := rtcp.Marshal([]rtcp.Packet{
		&rtcp.TransportLayerNack{MediaSSRC: 200, Nacks: []rtcp.NackPair{{PacketID: 1}}},
		&rtcp.TransportLayerNack{MediaSSRC: 201, Nacks: []rtcp.NackPair{{PacketID: 1}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	n.afterResend = wg.Done
	wg.Add(2)
	reader := n.BindRTCPReader(interceptor.RTCPReaderFunc(func(b []byte, a interceptor.Attributes) (int, interceptor.Attributes, error) {
		return copy(b, raw), a, nil
	}))
	if _, _, err := reader.Read(make([]byte, 1500), nil); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	got := w.take()
	if len(got) != 2 || got[0].SSRC == got[1].SSRC {
		t.Fatalf("resent %+v, want one packet of each stream", got)
	}
}

// Asking faster than the queue drains loses the excess, and it is counted.
func TestANackFloodBeyondTheQueueIsCounted(t *testing.T) {
	n, _ := newResponder(t, nil)
	n.queue = make(chan *rtcp.TransportLayerNack) // nobody reads: every enqueue after start finds it full
	n.startOnce.Do(func() {})
	dropped := metrics.SFURetransmitDropped.Load()
	n.enqueue(&rtcp.TransportLayerNack{Nacks: []rtcp.NackPair{{PacketID: 1, LostPackets: 0b11}}})
	if got := metrics.SFURetransmitDropped.Load() - dropped; got != 3 {
		t.Fatalf("counted %d dropped packets, want 3", got)
	}
}

func TestClosingStopsTheWorker(t *testing.T) {
	n, _ := newResponder(t, nil)
	n.enqueue(&rtcp.TransportLayerNack{MediaSSRC: 9, Nacks: []rtcp.NackPair{{PacketID: 1}}})
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	n.Close() // twice is fine
	select {
	case <-n.done:
	default:
		t.Fatal("done not closed")
	}
}

// Through a real connection: a viewer that asks for a packet it received gets
// it again.
func TestViewerNackIsAnsweredOverARealConnection(t *testing.T) {
	s := newTestSFU(t)
	roomID := uuid.New()
	publisher := newClient(t)
	publisher.publishVideo("nack-camera")
	publisher.join(s, roomID)
	viewer := newClient(t)
	viewer.join(s, roomID)
	if err := s.Room(roomID).Subscribe(viewer.id, publisher.id, SourceScreen, false, true); err != nil {
		t.Fatal(err)
	}
	remote := waitTrack(t, viewer)
	var seq uint16
	select {
	case p := <-viewer.received:
		seq = p.SequenceNumber
	case <-time.After(10 * time.Second):
		t.Fatal("the viewer received no video")
	}
	before := metrics.SFURetransmitted.Load()
	deadline := time.Now().Add(10 * time.Second)
	for metrics.SFURetransmitted.Load() == before && time.Now().Before(deadline) {
		if err := viewer.pc.WriteRTCP([]rtcp.Packet{&rtcp.TransportLayerNack{
			MediaSSRC: uint32(remote.SSRC()), Nacks: []rtcp.NackPair{{PacketID: seq}},
		}}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if metrics.SFURetransmitted.Load() == before {
		t.Fatal("the NACK was not answered")
	}
}
