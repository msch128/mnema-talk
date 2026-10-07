package sfu

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/pion/interceptor"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// SFU callbacks log asynchronously. TextHandler serializes writes, but test
// snapshots also need to share that lock rather than read a raw bytes.Buffer.
type synchronizedLogCapture struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *synchronizedLogCapture) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *synchronizedLogCapture) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

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

// failingLocal reports the error a single stopped subscriber binding causes,
// or err when set.
type failingLocal struct {
	writes  int
	checkIn func()
	err     error
}

func (f *failingLocal) WriteRTP(*rtp.Packet) error {
	f.writes++
	if f.checkIn != nil {
		f.checkIn()
	}
	if f.err != nil {
		return f.err
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

// A forwarding error other than a stopped subscriber is logged, once per
// track; a stopped subscriber is not logged at all.
func TestForwardLogsWriteErrorsOnce(t *testing.T) {
	var buf synchronizedLogCapture
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	user := uuid.New()
	r := &Room{peers: map[uuid.UUID]*Peer{}, trackLocals: map[string]*TrackInfo{}, notifier: &mediaNotifier{}}
	forward := func(err error) {
		info := &TrackInfo{SenderID: user, Kind: webrtc.RTPCodecTypeVideo, Source: SourceScreen, publisher: &Peer{ID: user}}
		r.forward(&fakeRemote{n: 5}, &failingLocal{err: err}, trackKey(user, "screen"), info)
	}

	forward(nil) // io.ErrClosedPipe
	if strings.Contains(buf.String(), "sfu forward rtp") {
		t.Fatalf("stopped subscriber was logged: %s", buf.String())
	}
	forward(errors.New("codec mismatch"))
	// Connection-state callbacks can keep logging after another test closes
	// its SFU. Exercise those writes while inspecting the forwarding log.
	const peerEvents = 100
	start := make(chan struct{})
	var logged sync.WaitGroup
	logged.Add(1)
	go func() {
		defer logged.Done()
		<-start
		for range peerEvents {
			slog.Info("sfu peer state", "user", user, "state", "closed")
		}
	}()
	t.Cleanup(logged.Wait)
	close(start)
	for range peerEvents {
		if n := strings.Count(buf.String(), "sfu forward rtp"); n != 1 {
			t.Fatalf("logged %d times, want once: %s", n, buf.String())
		}
	}
	logged.Wait()
	if n := strings.Count(buf.String(), "level=WARN msg=\"sfu forward rtp\""); n != 1 {
		t.Fatalf("forwarding error was not logged once at WARN: %s", buf.String())
	}
	if n := strings.Count(buf.String(), "msg=\"sfu peer state\" user="+user.String()); n != peerEvents {
		t.Fatalf("captured %d peer events, want %d: %s", n, peerEvents, buf.String())
	}
}
