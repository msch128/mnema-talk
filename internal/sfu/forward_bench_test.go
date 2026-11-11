package sfu

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/pion/interceptor"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// benchViewers are the audience sizes the forward benchmarks run with: one
// viewer shows the fixed cost of a packet, twenty a full Talk.
var benchViewers = []int{1, 20}

// benchBinding is one viewer's connection as a plain track sees it when the
// viewer subscribes: the guard every viewer's write goes through, over a
// connection that takes every packet.
type benchBinding struct {
	id    string
	codec webrtc.RTPCodecParameters
	out   interceptor.RTPWriter
}

func (b benchBinding) CodecParameters() []webrtc.RTPCodecParameters {
	return []webrtc.RTPCodecParameters{b.codec}
}
func (b benchBinding) HeaderExtensions() []webrtc.RTPHeaderExtensionParameter { return nil }
func (b benchBinding) SSRC() webrtc.SSRC                                      { return 1 }
func (b benchBinding) SSRCRetransmission() webrtc.SSRC                        { return 0 }
func (b benchBinding) SSRCForwardErrorCorrection() webrtc.SSRC                { return 0 }
func (b benchBinding) WriteStream() webrtc.TrackLocalWriter                   { return b }
func (b benchBinding) ID() string                                             { return b.id }
func (b benchBinding) RTCPReader() interceptor.RTCPReader                     { return nil }

func (b benchBinding) WriteRTP(h *rtp.Header, payload []byte) (int, error) {
	return b.out.Write(h, payload, nil)
}

func (b benchBinding) Write(p []byte) (int, error) { return len(p), nil }

// benchForward is one publisher's track with its viewers, and one packet as
// it arrives from the publisher.
type benchForward struct {
	room  *Room
	info  *TrackInfo
	key   string
	local rtpWriter
	wire  []byte
	pkt   rtp.Packet
}

func newBenchForward(b *testing.B, source Source, payload []byte) *benchForward {
	b.Helper()
	user := uuid.New()
	f := &benchForward{
		room: &Room{peers: map[uuid.UUID]*Peer{}, trackLocals: map[string]*TrackInfo{}, notifier: &mediaNotifier{}},
		info: &TrackInfo{SenderID: user, Kind: webrtc.RTPCodecTypeVideo, Source: source, publisher: &Peer{ID: user}},
		key:  trackKey(user, string(source)),
	}
	wire, err := (&rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: 96, SSRC: 7}, Payload: payload}).Marshal()
	if err != nil {
		b.Fatal(err)
	}
	f.wire = wire
	return f
}

// run forwards the packet b.N times: every step the forwarding loop takes
// for a packet it has read, except the publisher's rate limit, which would
// refuse a benchmark's packet rate.
func (f *benchForward) run(b *testing.B, viewers int) {
	b.Helper()
	b.ReportAllocs()
	for b.Loop() {
		if !parseForwardPacket(f.wire, len(f.wire), &f.pkt) || publisherVerdict(f.info) != forwardPacket {
			b.Fatal("the packet was not forwarded")
		}
		f.room.writeForwardedPacket(f.local, &f.pkt, f.key, f.info)
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(viewers), "ns/viewer")
}

// One packet of a plain track (microphone, or video without layers): one
// local track that every viewer's connection is bound to.
func BenchmarkForwardPlainTrack(b *testing.B) {
	codec := webrtc.RTPCodecCapability{MimeType: "video/VP8", ClockRate: videoClockRate}
	for _, viewers := range benchViewers {
		b.Run(fmt.Sprintf("viewers=%d", viewers), func(b *testing.B) {
			f := newBenchForward(b, SourceScreen, vp8Payload(true, true, 1, true, 1, 'a'))
			local, err := webrtc.NewTrackLocalStaticRTP(codec, "track", "stream")
			if err != nil {
				b.Fatal(err)
			}
			for i := range viewers {
				binding := benchBinding{id: fmt.Sprint(i), codec: webrtc.RTPCodecParameters{RTPCodecCapability: codec, PayloadType: 96}, out: guardedTestWriter()}
				if _, err := local.Bind(binding); err != nil {
					b.Fatal(err)
				}
			}
			f.local = local
			f.run(b, viewers)
		})
	}
}

// One packet of a layered track (camera or screen with simulcast): the layer
// group picks, per viewer, whether this layer's packet goes out and rewrites
// its numbering.
func BenchmarkForwardLayeredTrack(b *testing.B) {
	codec := webrtc.RTPCodecCapability{MimeType: "video/VP8", ClockRate: videoClockRate}
	for _, viewers := range benchViewers {
		b.Run(fmt.Sprintf("viewers=%d", viewers), func(b *testing.B) {
			f := newBenchForward(b, SourceScreen, vp8Payload(true, true, 1, true, 1, 'a'))
			g := newLayerGroup("group", f.info.SenderID, uuid.New(), &mediaNotifier{}, codec)
			b.Cleanup(g.close)
			pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = pc.Close() })
			f.info.publisher.PC = pc
			f.info.group, f.info.layer, f.info.ssrc = g, 0, 100
			if !g.attach(f.info, codec) {
				b.Fatal("layer refused")
			}
			delivered := 0
			for range viewers {
				vt, err := g.newViewer(uuid.New(), 0, 0, "track", "stream")
				if err != nil {
					b.Fatal(err)
				}
				vt.sink = func(*rtp.Packet) error { delivered++; return nil }
				g.addViewer(vt)
			}
			f.local = layerWriter{group: g, layer: 0}
			f.run(b, viewers)
			if delivered != b.N*viewers {
				b.Fatalf("%d packets reached the viewers, want %d", delivered, b.N*viewers)
			}
		})
	}
}
