package sfu

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// enableDTX is what the web client does to the SFU's offer: it asks for
// discontinuous transmission on the Opus line (usedtx=1).
func enableDTX(sdp string) string {
	return strings.ReplaceAll(sdp, "useinbandfec=1", "useinbandfec=1;usedtx=1")
}

// sentPacket is what a DTX-style publisher wrote.
type sentPacket struct {
	ts     uint32
	marker bool
	size   int
}

// dtxPublisher publishes an Opus track that is written by hand, the way a
// browser with DTX does: 20 ms frames while somebody speaks, then, without any
// packet for the silent time, a one-byte comfort-noise frame every 400 ms; the
// first frame after the silence carries the marker bit. Sequence numbers stay
// consecutive, the timestamps jump.
type dtxPublisher struct {
	track *webrtc.TrackLocalStaticRTP
	mu    sync.Mutex
	sent  map[uint16]sentPacket
	seq   uint16
	ts    uint32
}

func newDTXPublisher(t *testing.T, c *client) *dtxPublisher {
	t.Helper()
	track, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus}, "audio", "stream-"+c.id.String())
	if err != nil {
		t.Fatal(err)
	}
	sender, err := c.pc.AddTrack(track)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			if _, _, err := sender.ReadRTCP(); err != nil {
				return
			}
		}
	}()
	return &dtxPublisher{track: track, sent: map[uint16]sentPacket{}, seq: 100}
}

func (p *dtxPublisher) write(payload []byte, marker bool, advance uint32) {
	p.mu.Lock()
	p.seq++
	p.ts += advance
	pkt := &rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: 111, SequenceNumber: p.seq, Timestamp: p.ts, Marker: marker}, Payload: payload}
	p.sent[p.seq] = sentPacket{ts: p.ts, marker: marker, size: len(payload)}
	p.mu.Unlock()
	_ = p.track.WriteRTP(pkt)
}

// cycle is one stretch of speech followed by silence: the packets stop for a
// while, and a comfort-noise frame marks every 400 ms of it.
func (p *dtxPublisher) cycle(silence time.Duration) {
	const frame, sid = 960, 19200 // 20 ms and 400 ms at 48 kHz
	for i := 0; i < 4; i++ {
		p.write([]byte{0xfc, 0xff, 0xfe, byte(i)}, i == 0 && p.seq > 100, frame)
		time.Sleep(20 * time.Millisecond)
	}
	p.write([]byte{0xf8}, false, sid)
	time.Sleep(silence)
	p.ts += uint32(silence.Seconds() * 48000) // the clock ran on while no packet was sent
}

func (p *dtxPublisher) record(seq uint16) (sentPacket, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	rec, ok := p.sent[seq]
	return rec, ok
}

// A publisher that sends DTX (usedtx=1 negotiated, packets stopping in the
// silence) is forwarded intact: the SFU takes the parameter, keeps every
// sequence number, timestamp gap, marker and one-byte frame, and does not see
// the silence as a track that went away.
func TestOpusDtxIsNegotiatedAndItsGapsAreForwarded(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()

	pub := newClient(t)
	pub.offerTweak = enableDTX
	dtx := newDTXPublisher(t, pub)
	pub.join(s, room)
	sub := newClient(t)
	sub.join(s, room)

	// Packets flow while the publisher cycles through speech and silence; the
	// first ones may be written before the SFU has the track.
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				dtx.cycle(700 * time.Millisecond)
			}
		}
	}()
	t.Cleanup(func() { close(stop); <-done })

	waitTrack(t, sub)
	var got []*rtp.Packet
	deadline := time.After(20 * time.Second)
	for len(got) < 14 {
		select {
		case pkt := <-sub.received:
			got = append(got, pkt)
		case err := <-sub.errors:
			t.Fatalf("signaling failed: %v", err)
		case <-deadline:
			t.Fatalf("only %d packets arrived", len(got))
		}
	}

	var oneByte, gaps, markers int
	for i, pkt := range got {
		rec, ok := dtx.record(pkt.SequenceNumber)
		if !ok {
			t.Fatalf("packet %d was never sent", pkt.SequenceNumber)
		}
		if pkt.Timestamp != rec.ts || pkt.Marker != rec.marker || len(pkt.Payload) != rec.size {
			t.Fatalf("packet %d changed on the way: ts %d/%d marker %v/%v size %d/%d", pkt.SequenceNumber, pkt.Timestamp, rec.ts, pkt.Marker, rec.marker, len(pkt.Payload), rec.size)
		}
		if len(pkt.Payload) == 1 {
			oneByte++
		}
		if pkt.Marker {
			markers++
		}
		if i == 0 {
			continue
		}
		prev := got[i-1]
		if pkt.SequenceNumber != prev.SequenceNumber+1 {
			t.Fatalf("a packet went missing between %d and %d", prev.SequenceNumber, pkt.SequenceNumber)
		}
		if pkt.Timestamp-prev.Timestamp > 960*2 {
			gaps++
		}
	}
	if oneByte == 0 || gaps == 0 || markers == 0 {
		t.Fatalf("the DTX pattern did not arrive: %d comfort-noise frames, %d timestamp gaps, %d markers", oneByte, gaps, markers)
	}

	// The silence did not unpublish the track: it is still there, and the
	// publisher's answer said it takes DTX.
	r := s.Room(room)
	r.mu.RLock()
	var live bool
	for _, info := range r.trackLocals {
		if info.SenderID == pub.id && info.Kind == webrtc.RTPCodecTypeAudio {
			live = true
		}
	}
	r.mu.RUnlock()
	if !live {
		t.Fatal("the publisher's audio track was dropped")
	}
	answer, _ := pub.answerSDP.Load().(string)
	if !strings.Contains(answer, "usedtx=1") {
		t.Fatalf("the publisher's answer does not carry usedtx=1:\n%s", answer)
	}
	select {
	case err := <-pub.errors:
		t.Fatalf("the SFU refused the answer: %v", err)
	default:
	}
}

// A quiet stretch of several seconds, with packets only now and then, must not
// make the SFU do anything about the track (it is not a stall).
func TestAQuietPublisherIsNotDropped(t *testing.T) {
	s := newTestSFU(t)
	room := uuid.New()
	pub := newClient(t)
	pub.offerTweak = enableDTX
	dtx := newDTXPublisher(t, pub)
	pub.join(s, room)
	sub := newClient(t)
	sub.join(s, room)

	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				dtx.write([]byte{0xf8}, false, 19200)
				time.Sleep(400 * time.Millisecond)
			}
		}
	}()
	waitTrack(t, sub)
	received := 0
	timeout := time.After(10 * time.Second)
	for received < 8 {
		select {
		case <-sub.received:
			received++
		case <-timeout:
			t.Fatalf("only %d comfort-noise frames arrived in 10 s", received)
		}
	}
	if _, ok := s.Room(room).MediaStates()[pub.id]; ok {
		t.Fatal("audio must not show as a screen or camera")
	}
}
