package gateway

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type entry struct {
	seq  int64
	data []byte
}

// session is one logical gateway session of a bot. It outlives its
// connection for the resume window and keeps the last events for RESUME.
type session struct {
	id      string
	botID   uuid.UUID
	userID  uuid.UUID
	tokenID uuid.UUID
	intents int64
	// startedAt orders the sessions of a bot.
	startedAt time.Time

	mu sync.Mutex
	// starting is true from the registration of an identifying session until
	// its READY is queued: events published meanwhile are not part of the
	// session, so that READY stays the first dispatch.
	starting   bool
	seq        int64
	ring       []entry
	conn       *connection
	detachedAt time.Time
}

// enqueue numbers an event, keeps it for RESUME and sends it to the live
// connection. It never blocks: a connection that cannot keep up is closed and
// can resume. A session that has not sent READY yet takes no events.
func (s *session) enqueue(name string, d json.RawMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.starting {
		return
	}
	s.pushLocked(name, d)
}

// enqueueReady queues READY, the first dispatch of a session, and opens the
// session to events.
func (s *session) enqueueReady(d json.RawMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.starting = false
	s.pushLocked("READY", d)
}

// accepting reports whether the session has a connection and is past READY.
func (s *session) accepting() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn != nil && !s.conn.closed() && !s.starting
}

func (s *session) pushLocked(name string, d json.RawMessage) {
	s.seq++
	data := dispatch(s.seq, name, d)
	e := entry{seq: s.seq, data: data}
	if len(s.ring) < bufferSize {
		s.ring = append(s.ring, e)
	} else {
		s.ring[(s.seq-1)%bufferSize] = e
	}
	if s.conn != nil {
		s.conn.trySend(data)
	}
}

// oldestLocked is the sequence number of the oldest event still kept. Event n
// lives in slot (n-1) mod bufferSize, so the ring needs no shifting.
func (s *session) oldestLocked() int64 {
	return s.seq - int64(len(s.ring)) + 1
}

func dispatch(seq int64, name string, d json.RawMessage) []byte {
	out, err := json.Marshal(frame{Op: OpDispatch, D: d, S: &seq, T: &name})
	if err != nil {
		return nil
	}
	return out
}

// attach makes c the live connection and returns the one it replaces.
func (s *session) attach(c *connection) *connection {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.conn
	s.conn = c
	return old
}

// detach ends c's hold on the session; the session stays resumable.
func (s *session) detach(c *connection) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == c {
		s.conn = nil
		s.detachedAt = time.Now()
	}
}

// detachedSince reports when the session lost its connection; live is true
// while it has one.
func (s *session) detachedSince() (at time.Time, live bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.detachedAt, s.conn != nil
}

func (s *session) live() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn != nil && !s.conn.closed()
}

func (s *session) expired(now time.Time, window time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn == nil && now.Sub(s.detachedAt) > window
}

func (s *session) closeConn(code int, reason string) {
	s.mu.Lock()
	c := s.conn
	s.mu.Unlock()
	if c != nil {
		c.closeWith(code, reason)
	}
}

// resumeOn replays the events after seq to c and makes c the live connection.
// It reports false when the events cannot be replayed (seq is ahead of the
// session or older than the buffer).
func (s *session) resumeOn(c *connection, seq int64) (replaced *connection, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if seq > s.seq {
		return nil, false
	}
	if seq < s.seq {
		if s.oldestLocked() > seq+1 {
			return nil, false
		}
		for n := seq + 1; n <= s.seq; n++ {
			c.trySend(s.ring[(n-1)%bufferSize].data)
		}
	}
	// RESUMED carries the session's current sequence number without using one.
	cur := s.seq
	name := "RESUMED"
	out, err := json.Marshal(frame{Op: OpDispatch, D: json.RawMessage(`{}`), S: &cur, T: &name})
	if err != nil {
		return nil, false
	}
	c.trySend(out)
	replaced = s.conn
	s.conn = c
	return replaced, true
}

// connection is one WebSocket of a session.
type connection struct {
	ws   *websocket.Conn
	send chan []byte
	done chan struct{}
	once sync.Once

	closeCode   int
	closeReason string
}

func newConnection(ws *websocket.Conn) *connection {
	return &connection{ws: ws, send: make(chan []byte, sendQueue), done: make(chan struct{})}
}

// trySend queues a frame; a full queue closes the connection (1013, try again
// later) instead of blocking the publisher.
func (c *connection) trySend(b []byte) {
	select {
	case <-c.done:
	case c.send <- b:
	default:
		c.closeWith(websocket.CloseTryAgainLater, "too slow")
	}
}

// closeWith ends the connection with a close code; the first call wins.
func (c *connection) closeWith(code int, reason string) {
	c.once.Do(func() {
		c.closeCode, c.closeReason = code, reason
		close(c.done)
	})
}

func (c *connection) closed() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

// writeLoop is the only writer of the socket. After done it flushes what is
// queued, sends the close frame and closes the socket, which ends the reader.
func (c *connection) writeLoop() {
	defer c.ws.Close()
	for {
		select {
		case b := <-c.send:
			if !c.writeText(b) {
				c.closeWith(websocket.CloseAbnormalClosure, "")
				return
			}
		case <-c.done:
			if c.flush() {
				_ = c.ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(c.closeCode, c.closeReason), time.Now().Add(writeWait))
			}
			return
		}
	}
}

func (c *connection) writeText(b []byte) bool {
	_ = c.ws.SetWriteDeadline(time.Now().Add(writeWait))
	return c.ws.WriteMessage(websocket.TextMessage, b) == nil
}

// flush writes what is still queued; false means a write failed.
func (c *connection) flush() bool {
	for {
		select {
		case b := <-c.send:
			if !c.writeText(b) {
				return false
			}
		default:
			return true
		}
	}
}
