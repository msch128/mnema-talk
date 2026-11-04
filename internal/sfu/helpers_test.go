package sfu

import (
	"bytes"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

// Test-only views into a room.

// GetPeer returns the user's current connection in the room.
func (r *Room) GetPeer(userID uuid.UUID) *Peer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.peers[userID]
}

// Receives reports whether viewer currently receives publisher's video of the given kind.
func (r *Room) Receives(viewer, publisher uuid.UUID, kind Source) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return viewer != publisher && r.subs[viewer].Wants(publisher, webrtc.RTPCodecTypeVideo, kind)
}

// ScreenQuality is what viewer currently receives of publisher's screen
// share, if it receives it.
func (r *Room) ScreenQuality(viewer, publisher uuid.UUID) (QualityState, bool) {
	r.mu.RLock()
	g := r.groups[publisher]
	r.mu.RUnlock()
	if g == nil {
		return QualityState{}, false
	}
	vt := g.viewerFor(viewer)
	if vt == nil {
		return QualityState{}, false
	}
	return vt.state(), true
}

// synchronizedLogCapture collects log output. SFU callbacks log
// asynchronously, so a test that reads the log while they run needs the same
// lock the handler writes under.
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

// captureSFULog redirects the default logger into the returned capture for the
// rest of the test.
func captureSFULog(t *testing.T) *synchronizedLogCapture {
	t.Helper()
	buf := &synchronizedLogCapture{}
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	return buf
}

// waitSFUCondition polls condition until it holds and fails the test, naming
// label, if it does not within ten seconds.
func waitSFUCondition(t *testing.T, label string, condition func() bool) {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		if condition() {
			return
		}
		select {
		case <-timeout:
			t.Fatalf("timed out waiting for %s", label)
		case <-time.After(time.Millisecond):
		}
	}
}
