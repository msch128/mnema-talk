package sfu

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/metrics"
	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/gcc"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
)

func twccFeedback(t *testing.T) []byte {
	t.Helper()
	fb := &rtcp.TransportLayerCC{
		Header:     rtcp.Header{Count: rtcp.FormatTCC, Type: rtcp.TypeTransportSpecificFeedback, Length: 5},
		SenderSSRC: 1, MediaSSRC: 2, PacketStatusCount: 1, FbPktCount: 0,
		PacketChunks: []rtcp.PacketStatusChunk{&rtcp.RunLengthChunk{
			PacketStatusSymbol: rtcp.TypeTCCPacketReceivedSmallDelta, RunLength: 1,
		}},
		RecvDeltas: []*rtcp.RecvDelta{{Type: rtcp.TypeTCCPacketReceivedSmallDelta, Delta: 250}},
	}
	raw, err := fb.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func receiverReportBytes(t *testing.T) []byte {
	t.Helper()
	raw, err := (&rtcp.ReceiverReport{SSRC: 1}).Marshal()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestTransportFeedbackOfFindsOnlyTransportWideFeedback(t *testing.T) {
	twcc, rr := twccFeedback(t), receiverReportBytes(t)
	nack, err := (&rtcp.TransportLayerNack{SenderSSRC: 1, MediaSSRC: 2, Nacks: []rtcp.NackPair{{PacketID: 1}}}).Marshal()
	if err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		in   []byte
		want bool
	}{
		"empty":             {nil, false},
		"short":             {[]byte{0x8f, 205}, false},
		"receiver report":   {rr, false},
		"nack":              {nack, false},
		"twcc":              {twcc, true},
		"twcc after report": {append(append([]byte{}, rr...), twcc...), true},
		"truncated":         {append(append([]byte{}, rr...), 0x8f, 205, 0xff, 0xff, 0), false},
	} {
		if got := len(transportFeedbackOf(tc.in)) > 0; got != tc.want {
			t.Errorf("%s: %v, want %v", name, got, tc.want)
		}
	}
}

func TestPassPacerForwardsToTheStreamsWriter(t *testing.T) {
	p := &passPacer{writers: map[uint32]interceptor.RTPWriter{}}
	var got []uint16
	p.AddStream(7, interceptor.RTPWriterFunc(func(h *rtp.Header, payload []byte, _ interceptor.Attributes) (int, error) {
		got = append(got, h.SequenceNumber)
		return len(payload), nil
	}))
	if n, err := p.Write(&rtp.Header{SSRC: 7, SequenceNumber: 3}, []byte{1, 2}, nil); err != nil || n != 2 {
		t.Fatalf("known stream: %d %v", n, err)
	}
	if _, err := p.Write(&rtp.Header{SSRC: 8}, nil, nil); err == nil {
		t.Fatal("a packet of an unknown stream was accepted")
	}
	p.SetTargetBitrate(1)
	if err := p.Close(); err != nil || len(got) != 1 || got[0] != 3 || p.target.Load() != 1 {
		t.Fatalf("close %v, written %v", err, got)
	}
}

func TestBandwidthInterceptor(t *testing.T) {
	icpt, err := bweFactory{}.NewInterceptor("pc")
	if err != nil {
		t.Fatal(err)
	}
	b := icpt.(*bweInterceptor)
	defer func() {
		if err := icpt.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	if b.bandwidth() != 0 {
		t.Fatal("an estimate before any feedback")
	}

	// Packets of a stream reach the wire writer through the estimator.
	var written int
	w := icpt.BindLocalStream(&interceptor.StreamInfo{SSRC: 5, SSRCRetransmission: 6}, interceptor.RTPWriterFunc(func(*rtp.Header, []byte, interceptor.Attributes) (int, error) {
		written++
		return 1, nil
	}))
	if _, err := w.Write(&rtp.Header{SSRC: 5}, []byte{1}, nil); err != nil || written != 1 {
		t.Fatalf("write: %v, %d written", err, written)
	}
	// A retransmission has an SSRC of its own and takes the same way.
	if _, err := w.Write(&rtp.Header{SSRC: 6}, []byte{1}, nil); err != nil || written != 2 {
		t.Fatalf("retransmission: %v, %d written", err, written)
	}
	// A stream without retransmission registers only its own SSRC.
	icpt.BindLocalStream(&interceptor.StreamInfo{SSRC: 9}, interceptor.RTPWriterFunc(func(*rtp.Header, []byte, interceptor.Attributes) (int, error) { return 0, nil }))
	if _, ok := b.pacer.writers[0]; ok {
		t.Fatal("a stream without retransmission registered SSRC 0")
	}

	// Reading hands the estimator to whoever reads, counts transport-wide
	// feedback only, and trusts the estimate after enough of it.
	feedback := twccFeedback(t)
	var next []byte
	var innerErr error
	r := icpt.BindRTCPReader(interceptor.RTCPReaderFunc(func(buf []byte, a interceptor.Attributes) (int, interceptor.Attributes, error) {
		return copy(buf, next), a, innerErr
	}))
	buf := make([]byte, 1500)
	next = receiverReportBytes(t)
	_, attr, err := r.Read(buf, nil)
	if err != nil {
		t.Fatal(err)
	}
	if src, ok := attr.Get(bweKey{}).(bandwidthSource); !ok || src.bandwidth() != 0 {
		t.Fatalf("attribute %v", attr.Get(bweKey{}))
	}
	if b.feedback.Load() != 0 {
		t.Fatal("a receiver report counted as transport feedback")
	}
	next = feedback
	for range bweMinFeedback {
		if _, _, err := r.Read(buf, interceptor.Attributes{}); err != nil {
			t.Fatal(err)
		}
	}
	if got := b.bandwidth(); got <= 0 {
		t.Fatalf("estimate %d after %d feedback packets", got, b.feedback.Load())
	}
	// Garbage that looks like feedback and an empty read change nothing.
	next = []byte{0x8f, 205, 0, 1, 0, 0, 0, 0}
	if _, _, err := r.Read(buf, nil); err != nil {
		t.Fatal(err)
	}
	next = nil
	if n, _, err := r.Read(buf, nil); err != nil || n != 0 {
		t.Fatalf("empty read: %d %v", n, err)
	}
	// A failing reader's error passes through.
	innerErr = errors.New("closed")
	if _, _, err := r.Read(buf, nil); !errors.Is(err, innerErr) {
		t.Fatalf("error %v", err)
	}
	// An estimator that is closed refuses feedback; the reader goes on.
	innerErr = nil
	next = feedback
	if err := icpt.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Read(buf, nil); err != nil {
		t.Fatalf("read after close: %v", err)
	}
}

func TestBandwidthInterceptorNeedsAnEstimator(t *testing.T) {
	prev := newEstimator
	newEstimator = func(...gcc.Option) (*gcc.SendSideBWE, error) { return nil, errors.New("no") }
	defer func() { newEstimator = prev }()
	if _, err := (bweFactory{}).NewInterceptor("pc"); err == nil {
		t.Fatal("no error from a failing estimator")
	}
}

// A real connection: the browser-like client reports transport-wide feedback
// for what the SFU sends, and the viewer's estimate reaches the adapter.
func TestViewerEstimateReachesTheAdapterOverARealConnection(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()
	pub := newClient(t)
	pub.publishLayers(25)
	pub.join(s, room)
	viewer := newClient(t)
	viewer.join(s, room)
	r := s.Room(room)
	waitSources(t, r, pub.id, SourceScreen, 3)
	if err := r.SetScreenLayers(pub.id, []Layer{{3840, 2160, 60}, {1920, 1080, 30}, {1280, 720, 30}}); err != nil {
		t.Fatal(err)
	}
	before := metrics.SFUBandwidth.Total()
	if err := r.Subscribe(viewer.id, pub.id, SourceScreen, false, true); err != nil {
		t.Fatal(err)
	}
	waitTrack(t, viewer)
	deadline := time.Now().Add(30 * time.Second)
	for metrics.SFUBandwidth.Total() == before {
		if time.Now().After(deadline) {
			t.Fatal("no bandwidth estimate reached the adapter")
		}
		gather(viewer, 200*time.Millisecond)
	}
}
