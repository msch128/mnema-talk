package sfu

import (
	"bytes"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

type authorizationWriter struct {
	packets int
	payload []byte
}

func (w *authorizationWriter) WriteRTP(_ *rtp.Header, payload []byte) (int, error) {
	w.packets++
	w.payload = append([]byte(nil), payload...)
	return len(payload), nil
}
func (w *authorizationWriter) Write(raw []byte) (int, error) {
	w.packets++
	w.payload = append([]byte(nil), raw...)
	return len(raw), nil
}

type authorizationContext struct {
	id     string
	writer webrtc.TrackLocalWriter
}

func (c authorizationContext) ID() string                           { return c.id }
func (c authorizationContext) WriteStream() webrtc.TrackLocalWriter { return c.writer }
func (c authorizationContext) CodecParameters() []webrtc.RTPCodecParameters {
	return []webrtc.RTPCodecParameters{{RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, PayloadType: 111}}
}

// Supply the public contextual SSRC methods used by the actual track binding.
type completeAuthorizationContext struct {
	authorizationContext
}

func (completeAuthorizationContext) SSRC() webrtc.SSRC                       { return 1 }
func (completeAuthorizationContext) SSRCRetransmission() webrtc.SSRC         { return 0 }
func (completeAuthorizationContext) SSRCForwardErrorCorrection() webrtc.SSRC { return 0 }

func TestRecipientAuthorizationUsesActualTrackLocalBinding(t *testing.T) {
	track, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, "audio", "publisher")
	if err != nil {
		t.Fatal(err)
	}
	var authorized atomic.Bool
	authorized.Store(true)
	nativeWriter, browserWriter := &authorizationWriter{}, &authorizationWriter{}
	nativeContext := completeAuthorizationContext{authorizationContext: authorizationContext{id: "native", writer: nativeWriter}}
	browserContext := completeAuthorizationContext{authorizationContext: authorizationContext{id: "browser", writer: browserWriter}}
	guarded := &authorizedTrack{TrackLocal: track, authorize: authorized.Load}
	if _, err := guarded.Bind(nativeContext); err != nil {
		t.Fatal(err)
	}
	if _, err := track.Bind(browserContext); err != nil {
		t.Fatal(err)
	}
	packet := &rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: 1}, Payload: []byte("unchanged-encrypted-payload")}
	if err := track.WriteRTP(packet); err != nil || nativeWriter.packets != 1 || browserWriter.packets != 1 || !bytes.Equal(nativeWriter.payload, packet.Payload) {
		t.Fatal("authorized binding changed/dropped payload")
	}
	authorized.Store(false)
	if err := track.WriteRTP(packet); !errors.Is(err, io.ErrClosedPipe) || nativeWriter.packets != 1 || browserWriter.packets != 2 {
		t.Fatal("revoked recipient still wrote RTP or stopped browser")
	}
	if err := guarded.Unbind(nativeContext); err != nil {
		t.Fatal(err)
	}
	if err := track.WriteRTP(packet); err != nil || browserWriter.packets != 3 {
		t.Fatal("native unbind damaged browser recipient")
	}
	writer := authorizedRTPWriter{writer: nativeWriter, authorize: authorized.Load}
	if n, err := writer.Write([]byte("private")); n != 0 || err != io.ErrClosedPipe {
		t.Fatal("revoked raw writer passed payload")
	}
	authorized.Store(true)
	if n, err := writer.Write([]byte("allowed")); err != nil || n != 7 || nativeWriter.packets != 2 {
		t.Fatal("allowed raw writer failed")
	}
}

func TestPublisherAuthorizationStopsActualForwardLoop(t *testing.T) {
	for _, native := range []bool{true, false} {
		var authorized atomic.Bool
		authorized.Store(true)
		publisher := &Peer{ID: uuid.New()}
		if native {
			publisher.authorizeMedia = authorized.Load
		}
		r := &Room{trackLocals: map[string]*TrackInfo{}, peers: map[uuid.UUID]*Peer{}, subs: map[uuid.UUID]*Subscriptions{}, lastMedia: map[uuid.UUID]MediaState{}, notifier: &mediaNotifier{}}
		info := &TrackInfo{SenderID: publisher.ID, publisher: publisher, Kind: webrtc.RTPCodecTypeAudio, Source: SourceAudio}
		writer := &failingLocal{}
		writer.checkIn = func() {
			if writer.writes == 2 {
				authorized.Store(false)
			}
		}
		r.forward(&fakeRemote{n: 5}, writer, "audio", info)
		want := 5
		if native {
			want = 2
		}
		if writer.writes != want {
			t.Fatal("publisher cutoff/default browser flow violated")
		}
	}
	s := newTestSFU(t)
	if _, _, err := s.JoinWithAuthorization(uuid.New(), uuid.New(), nil, nil, nil); err == nil {
		t.Fatal("missing native media latch accepted")
	}
	if _, _, err := s.JoinWithAuthorization(uuid.New(), uuid.New(), nil, nil, func() bool { return false }); err == nil {
		t.Fatal("expired native media latch accepted")
	}
}

func (completeAuthorizationContext) HeaderExtensions() []webrtc.RTPHeaderExtensionParameter {
	return nil
}
func (completeAuthorizationContext) RTCPReader() interceptor.RTCPReader { return nil }

// Exercise Pion's complete default interceptor chain, not a simulated cache:
// a real RTCP NACK must retransmit a previously cached packet while authorized,
// and must hit the innermost gate after revocation without closing the chain.
func TestNativeAuthorizationCutsOffDefaultNACKRetransmissions(t *testing.T) {
	for _, mode := range []string{"native-recipient", "native-source", "browser"} {
		t.Run(mode, func(t *testing.T) {
			var live atomic.Bool
			live.Store(true)
			denied := make(chan struct{}, 1)
			registry := &interceptor.Registry{}
			if mode != "browser" {
				gate := func() bool {
					if live.Load() {
						return true
					}
					select {
					case denied <- struct{}{}:
					default:
					}
					return false
				}
				if mode == "native-recipient" {
					registry.Add(mediaAuthorizationFactory{authorize: gate})
				} else {
					sources := &sourceAuthorizationRegistry{}
					sources.bind("real-sender", gate)
					registry.Add(mediaAuthorizationFactory{sources: sources})
				}
			}
			engine := &webrtc.MediaEngine{}
			if err := engine.RegisterDefaultCodecs(); err != nil {
				t.Fatal(err)
			}
			if err := webrtc.RegisterDefaultInterceptors(engine, registry); err != nil {
				t.Fatal(err)
			}
			chain, err := registry.Build("actual-native-pc")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = chain.Close() })
			chain.BindRTCPWriter(interceptor.RTCPWriterFunc(func([]rtcp.Packet, interceptor.Attributes) (int, error) { return 0, nil }))
			info := &interceptor.StreamInfo{ID: "real-sender", SSRC: 123, PayloadType: 111, MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, RTCPFeedback: []interceptor.RTCPFeedback{{Type: "nack"}}}
			packets := make(chan []byte, 8)
			writer := chain.BindLocalStream(info, interceptor.RTPWriterFunc(func(_ *rtp.Header, payload []byte, _ interceptor.Attributes) (int, error) {
				packets <- append([]byte(nil), payload...)
				return len(payload), nil
			}))
			payload := []byte("byte-exact-public-marker")
			if _, err := writer.Write(&rtp.Header{Version: 2, SSRC: 123, SequenceNumber: 42}, payload, nil); err != nil {
				t.Fatal(err)
			}
			assertPacket := func() {
				t.Helper()
				select {
				case got := <-packets:
					if !bytes.Equal(got, payload) {
						t.Fatal("interceptor changed payload")
					}
				case <-time.After(time.Second):
					t.Fatal("actual RTP write or cached retransmission missing")
				}
			}
			assertPacket()
			nack, err := rtcp.Marshal([]rtcp.Packet{&rtcp.TransportLayerNack{MediaSSRC: 123, Nacks: []rtcp.NackPair{{PacketID: 42}}}})
			if err != nil {
				t.Fatal(err)
			}
			reader := chain.BindRTCPReader(interceptor.RTCPReaderFunc(func(buf []byte, _ interceptor.Attributes) (int, interceptor.Attributes, error) {
				return copy(buf, nack), nil, nil
			}))
			request := func() {
				t.Helper()
				if _, _, err := reader.Read(make([]byte, 1500), nil); err != nil {
					t.Fatal(err)
				}
			}
			request()
			assertPacket() // proves a genuine cached NACK resend, not only first write.
			live.Store(false)
			request()
			if mode != "browser" {
				select {
				case <-denied:
				case <-time.After(time.Second):
					t.Fatal("cached NACK resend did not reach native gate")
				}
				select {
				case <-packets:
					t.Fatal("revoked cached packet reached final RTP writer")
				default:
				}
				if _, err := writer.Write(&rtp.Header{Version: 2, SSRC: 123, SequenceNumber: 43}, payload, nil); !errors.Is(err, io.ErrClosedPipe) {
					t.Fatal("new packet bypassed revoked gate")
				}
			} else {
				assertPacket()
			}
		})
	}
}

func TestNativeMediaFactoryOrdinaryBrowserWriterUnchanged(t *testing.T) {
	i, err := (mediaAuthorizationFactory{sources: &sourceAuthorizationRegistry{}}).NewInterceptor("browser")
	if err != nil {
		t.Fatal(err)
	}
	var writes int
	writer := i.BindLocalStream(&interceptor.StreamInfo{ID: "ordinary-browser-source"}, interceptor.RTPWriterFunc(func(*rtp.Header, []byte, interceptor.Attributes) (int, error) { writes++; return 1, nil }))
	if _, err := writer.Write(&rtp.Header{}, nil, nil); err != nil || writes != 1 {
		t.Fatal("ordinary browser stream was changed")
	}
}

func TestSourceAuthorizationTransfersActualBindingAndCapturesLatch(t *testing.T) {
	track, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, "audio", "publisher")
	if err != nil {
		t.Fatal(err)
	}
	var live atomic.Bool
	live.Store(true)
	writer := &authorizationWriter{}
	ctx := completeAuthorizationContext{authorizationContext: authorizationContext{id: "exact-sender", writer: writer}}
	sources := &sourceAuthorizationRegistry{}
	guard := &authorizedTrack{TrackLocal: track, authorize: func() bool { return true }, source: live.Load, sources: sources}
	if _, err := guard.Bind(ctx); err != nil {
		t.Fatal(err)
	}
	factory, err := (mediaAuthorizationFactory{sources: sources}).NewInterceptor("browser")
	if err != nil {
		t.Fatal(err)
	}
	final := factory.BindLocalStream(&interceptor.StreamInfo{ID: ctx.ID()}, interceptor.RTPWriterFunc(func(header *rtp.Header, payload []byte, _ interceptor.Attributes) (int, error) {
		return writer.WriteRTP(header, payload)
	}))
	if sources.capture(ctx.ID()) != nil {
		t.Fatal("source association was retained after capture")
	}
	if _, err := final.Write(&rtp.Header{}, []byte("public-marker"), nil); err != nil || writer.packets != 1 {
		t.Fatal("valid exact source callback failed")
	}
	if err := guard.Unbind(ctx); err != nil {
		t.Fatal(err)
	}
	live.Store(false)
	if _, err := final.Write(&rtp.Header{}, []byte("public-marker"), nil); !errors.Is(err, io.ErrClosedPipe) || writer.packets != 1 {
		t.Fatal("unbind/deletion made captured source fail open")
	}
	missing := &authorizedTrack{TrackLocal: track, authorize: func() bool { return true }, source: live.Load}
	if _, err := missing.Bind(ctx); err == nil {
		t.Fatal("native publisher bound without source authorization registry")
	}
	old := sources.bind("replacement", live.Load)
	current := sources.bind("replacement", live.Load)
	sources.remove("replacement", old)
	if sources.capture("replacement") != current {
		t.Fatal("stale source cleanup deleted successor binding")
	}
}
