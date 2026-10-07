package ws

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/sfu"
)

// Client is one WebSocket connection.
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte
	User auth.User

	tokenVersion int
	closeOnce    sync.Once
	// closing is set by the first deliver that finds the buffer full, so a
	// slow client's later events don't each start another close.
	closing atomic.Bool

	// send is never closed: SendEvent runs from goroutines other than the
	// read loop (SFU callbacks, admin kicks), and a send on a closed channel
	// panics. done is closed instead once the client is unregistered; the
	// write loop exits on it and deliver drops further events.
	done     chan struct{}
	doneOnce sync.Once
	closed   atomic.Bool

	// idle is set by the client after a while without input; it turns an
	// "online" user into "away" while all of their connections are idle.
	idle bool // guarded by hub.mu

	// voiceMu serializes this connection's voice joins and leaves (its own
	// read loop, an admin kick, a deleted channel), so a leave never runs
	// between a join's presence update and its SFU peer being recorded.
	voiceMu sync.Mutex

	mu      sync.Mutex
	voiceCh *uuid.UUID
	// sfuPeer is this connection's own media peer. Another connection of the
	// same user (reconnect, second tab) gets its own, and tearing this one
	// down never touches the other.
	sfuPeer *sfu.Peer

	// typing is when a typing notice was last relayed, per channel.
	typing map[uuid.UUID]time.Time
	// speaking is the last relayed speaking state; speakWindow/speakCount
	// cap how often it may change.
	speaking    bool
	speakWindow time.Time
	speakCount  int
	// diagWindow/diagCount rate-limit logged client diagnostics.
	diagWindow time.Time
	diagCount  int
}

func newClient(h *Hub, conn *websocket.Conn, user auth.User, tokenVersion int) *Client {
	return &Client{hub: h, conn: conn, send: make(chan []byte, sendBuffer), done: make(chan struct{}),
		User: user, tokenVersion: tokenVersion}
}

// close tears the connection down once; readPump then unregisters the client.
func (c *Client) close() {
	c.closeOnce.Do(func() {
		if c.conn != nil {
			_ = c.conn.Close()
		}
	})
}

// shutdown marks the client as gone: later events are dropped and the write
// loop stops. Safe to call more than once and concurrently with deliver.
func (c *Client) shutdown() {
	c.doneOnce.Do(func() {
		c.closed.Store(true)
		if c.done != nil {
			close(c.done)
		}
	})
}

// deliver queues data for c; a client whose buffer is full is too slow to keep
// up and gets disconnected rather than blocking everyone else.
func (c *Client) deliver(data []byte) {
	if c.closed.Load() {
		return
	}
	select {
	case c.send <- data:
	default:
		if c.closing.CompareAndSwap(false, true) {
			go c.close()
		}
	}
}

func (c *Client) SendEvent(eventType string, payload any) {
	if data := encode(eventType, payload); data != nil {
		c.deliver(data)
	}
}

const (
	// typingThrottle is how often one connection's typing notice is relayed
	// per channel; clients show "… schreibt" for a few seconds after the last
	// notice.
	typingThrottle = 3 * time.Second
	// maxTypingChannels bounds the per-connection typing memory.
	maxTypingChannels = 32
	// maxSpeakingChangesPerSec caps how often one connection's speaking
	// indicator may flip; a real voice activity detector stays far below it.
	maxSpeakingChangesPerSec = 8
	// Client diagnostics are logged at most diagPerWindow times per diagWindow.
	diagPerWindow = 5
	diagWindow    = 10 * time.Second
)

// allowTyping reports whether a typing notice for chID may be relayed now.
// The throttle is per channel, so alternating between channels cannot
// bypass it.
func (c *Client) allowTyping(chID uuid.UUID, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if last, ok := c.typing[chID]; ok && now.Sub(last) < typingThrottle {
		return false
	}
	if c.typing == nil {
		c.typing = map[uuid.UUID]time.Time{}
	}
	if len(c.typing) >= maxTypingChannels {
		for id, at := range c.typing {
			if now.Sub(at) >= typingThrottle {
				delete(c.typing, id)
			}
		}
		if len(c.typing) >= maxTypingChannels {
			return false
		}
	}
	c.typing[chID] = now
	return true
}

// speakingChanged records a speaking state and reports whether it should be
// relayed: only changes count, and at most maxSpeakingChangesPerSec of them
// per second. Stopping is always relayed so nobody stays shown as speaking.
func (c *Client) speakingChanged(active bool, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if active == c.speaking {
		return false
	}
	if active {
		if now.Sub(c.speakWindow) >= time.Second {
			c.speakWindow, c.speakCount = now, 0
		}
		if c.speakCount >= maxSpeakingChangesPerSec {
			return false
		}
		c.speakCount++
	}
	c.speaking = active
	return true
}

// allowDiag rate-limits logged client diagnostics.
func (c *Client) allowDiag(now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if now.Sub(c.diagWindow) >= diagWindow {
		c.diagWindow, c.diagCount = now, 0
	}
	if c.diagCount >= diagPerWindow {
		return false
	}
	c.diagCount++
	return true
}

func (c *Client) currentVoice() *uuid.UUID {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.voiceCh
}

// setVoice records the voice room this connection is in; nil drops the SFU
// peer too. A room change resets the speaking indicator.
func (c *Client) setVoice(ch *uuid.UUID) {
	c.mu.Lock()
	c.voiceCh = ch
	c.speaking = false
	if ch == nil {
		c.sfuPeer = nil
	}
	c.mu.Unlock()
}

func (c *Client) setPeer(p *sfu.Peer) {
	c.mu.Lock()
	c.sfuPeer = p
	c.mu.Unlock()
}

func (c *Client) peer() *sfu.Peer {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sfuPeer
}
