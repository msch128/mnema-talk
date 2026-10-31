package main

import (
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/msch128/mnema-talk/loadtest/viewer/internal/lt"
	"github.com/pion/webrtc/v4"
)

type token = lt.Token

// quality is the layer this member asks for (QUALITY), nil to leave it to the
// server.
func (m *member) quality() any {
	switch q := os.Getenv("QUALITY"); q {
	case "", "auto":
		return nil
	case "spread":
		return m.index % 3
	default:
		if n, err := strconv.Atoi(q); err == nil && n >= 0 && n < 3 {
			return n
		}
		return nil
	}
}

// member is one simulated Talk participant.
type member struct {
	id    string
	index int
	role  pubRole
	// cameras is what the member opts out of.
	cameras cameraPlan
	api     *webrtc.API

	conn    *websocket.Conn
	pc      *webrtc.PeerConnection
	writeMu sync.Mutex
	stop    chan struct{}

	startedAt time.Time // voice_join sent
	connected atomic.Bool
	everConn  atomic.Bool
	connMs    atomic.Int64 // join -> first "connected"
	videoMs   atomic.Int64 // join -> first video byte, plus one
	flaps     atomic.Int64 // connected -> not connected transitions

	videoB atomic.Int64
	audioB atomic.Int64
	audioT atomic.Int64
	offers atomic.Int64

	candSent, candRecv, candErr atomic.Int64
	maxStallMs                  atomic.Int64

	failed atomic.Value // string

	// Published media (synthetic, see publish.go) and received tracks.
	pubOnce sync.Once
	sentB   atomic.Int64
	sentPkt atomic.Int64 // packets handed to the connection
	trackMu sync.Mutex
	tracks  []*trackStats

	// Remote candidates that arrive before the offer is applied, and local
	// candidates gathered before the answer is on its way, are queued:
	// pion rejects AddICECandidate without a remote description, and the
	// server rejects candidates before it has the answer.
	candMu     sync.Mutex
	remoteSet  bool
	pendingIn  []webrtc.ICECandidateInit
	answerSent bool
	pendingOut []webrtc.ICECandidateInit
	closeOnce  sync.Once

	// Diagnostics: a timeline of state changes, what became of the server's
	// candidates, and the ICE statistics taken when the member finished.
	diagMu     sync.Mutex
	timeline   []string
	remoteSeen []string
	snapshot   *iceSnapshot
	snapOnce   sync.Once
	timedOut   atomic.Bool
}

func (m *member) send(kind string, payload any) {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	if m.conn == nil {
		return
	}
	_ = m.conn.WriteJSON(map[string]any{"type": kind, "payload": payload})
}

func (m *member) close() {
	m.closeOnce.Do(func() {
		close(m.stop)
		m.send("voice_leave", map[string]any{})
		if m.conn != nil {
			_ = m.conn.Close()
		}
		if m.pc != nil {
			_ = m.pc.Close()
		}
	})
}

// closeAll closes the members concurrently and returns after at most limit,
// with the number that finished. Pion's Close can block on a lock held by an
// ICE start that never completes, so the caller must not depend on it.
func closeAll(members []*member, limit time.Duration) int {
	fns := make([]func(), len(members))
	for i, m := range members {
		fns[i] = m.close
	}
	return runBounded(fns, limit)
}

// addRemote applies a candidate, or queues it until the offer is applied.
func (m *member) addRemote(c webrtc.ICECandidateInit) {
	m.candMu.Lock()
	defer m.candMu.Unlock()
	if !m.remoteSet {
		m.pendingIn = append(m.pendingIn, c)
		m.noteRemote(c, "queued")
		return
	}
	m.candRecv.Add(1)
	if err := m.pc.AddICECandidate(c); err != nil {
		m.candErr.Add(1)
		m.noteRemote(c, "error("+err.Error()+")")
		return
	}
	m.noteRemote(c, "added")
}

func (m *member) remoteApplied() {
	m.candMu.Lock()
	defer m.candMu.Unlock()
	m.remoteSet = true
	for _, c := range m.pendingIn {
		m.candRecv.Add(1)
		if err := m.pc.AddICECandidate(c); err != nil {
			m.candErr.Add(1)
			m.noteRemote(c, "queued-error("+err.Error()+")")
			continue
		}
		m.noteRemote(c, "queued-added")
	}
	m.pendingIn = nil
}

// sendCandidate sends a local candidate once the answer has been sent.
func (m *member) sendCandidate(c webrtc.ICECandidateInit) {
	m.candMu.Lock()
	if !m.answerSent {
		m.pendingOut = append(m.pendingOut, c)
		m.candMu.Unlock()
		return
	}
	m.candMu.Unlock()
	m.candSent.Add(1)
	m.send("webrtc_candidate", c)
}

func (m *member) answerWasSent() {
	m.candMu.Lock()
	m.answerSent = true
	out := m.pendingOut
	m.pendingOut = nil
	m.candMu.Unlock()
	for _, c := range out {
		m.candSent.Add(1)
		m.send("webrtc_candidate", c)
	}
}
