package sfu

import (
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/pion/interceptor"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// fakeRemote yields n RTP packets, then fails like an ended remote track.
type fakeRemote struct {
	n    int
	sent int
}

func (f *fakeRemote) Read(b []byte) (int, interceptor.Attributes, error) {
	if f.sent >= f.n {
		return 0, nil, io.EOF
	}
	f.sent++
	pkt := rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: uint16(f.sent), SSRC: 1}, Payload: []byte{1, 2, 3}}
	raw, err := pkt.Marshal()
	if err != nil {
		return 0, nil, err
	}
	return copy(b, raw), nil, nil
}

// failingLocal reports the error a single stopped subscriber binding causes.
type failingLocal struct {
	writes  int
	checkIn func()
}

func (f *failingLocal) WriteRTP(*rtp.Packet) error {
	f.writes++
	if f.checkIn != nil {
		f.checkIn()
	}
	return io.ErrClosedPipe
}

// A write error from one subscriber must not unpublish the track for the
// whole room: forwarding goes on until the publisher's track ends.
func TestForwardIgnoresSubscriberWriteErrors(t *testing.T) {
	user := uuid.New()
	peer := &Peer{ID: user}
	r := &Room{
		peers:       map[uuid.UUID]*Peer{}, // no live connections to signal
		trackLocals: map[string]*TrackInfo{},
		subs:        map[uuid.UUID]*Subscriptions{},
		lastMedia:   map[uuid.UUID]MediaState{},
		notifier:    &mediaNotifier{},
	}
	key := trackKey(user, "audio")
	info := &TrackInfo{SenderID: user, Kind: webrtc.RTPCodecTypeAudio, Source: SourceAudio, publisher: peer}
	r.trackLocals[key] = info

	local := &failingLocal{}
	local.checkIn = func() {
		r.mu.RLock()
		defer r.mu.RUnlock()
		if r.trackLocals[key] != info {
			t.Errorf("track removed after %d writes; a write error must not end it", local.writes)
		}
	}
	r.forward(&fakeRemote{n: 5}, local, key, info)

	if local.writes != 5 {
		t.Fatalf("writes = %d, want all 5 packets forwarded", local.writes)
	}
	r.mu.RLock()
	_, still := r.trackLocals[key]
	r.mu.RUnlock()
	if still {
		t.Fatal("track still published after the publisher's track ended")
	}
}
