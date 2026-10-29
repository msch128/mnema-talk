package ws

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/metrics"
	"github.com/msch128/mnema-talk/internal/sfu"
)

// Client is one WebSocket connection.
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte
	User auth.User

	tokenVersion int
	// dmAware is set by connections that asked for direct message events
	// (/api/ws?dm=1). A bundle from before direct messages does not, and
	// would not know what to do with the messages of a conversation.
	dmAware bool
	// sessionID and sessionExp identify the browser session behind the
	// connection (empty for in-memory clients).
	sessionID  string
	sessionExp time.Time
	// revalidateFails counts consecutive revalidation database errors.
	revalidateFails int
	closeOnce       sync.Once
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

	// connectedAt orders a user's browser sockets for eviction.
	connectedAt time.Time

	// voiceMu serializes this connection's voice joins and leaves (its own
	// read loop, an admin kick, a deleted channel), so a leave never runs
	// between a join's presence update and its SFU peer being recorded.
	voiceMu sync.Mutex

	mu      sync.Mutex
	voiceCh *uuid.UUID
	// seat orders the seated voice connections of one user: the hub's clock
	// at the moment the SFU join succeeded, 0 while not seated yet.
	seat uint64
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
	// permSent/permPending coalesce permissions_changed (see notifyPermissions).
	permSent    time.Time
	permPending bool
	// diagWindow/diagCount rate-limit logged client diagnostics.
	diagWindow time.Time
	diagCount  int
}

func newClient(h *Hub, conn *websocket.Conn, user auth.User, tokenVersion int) *Client {
	return &Client{hub: h, conn: conn, send: make(chan []byte, sendBuffer), done: make(chan struct{}),
		User: user, tokenVersion: tokenVersion, connectedAt: time.Now()}
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
		metrics.WSEventsSent.Inc()
	default:
		metrics.WSDropped.With("slow_client").Inc()
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
	c.seat = 0
	c.speaking = false
	if ch == nil {
		c.sfuPeer = nil
	}
	c.mu.Unlock()
}

// stampSeat records that c's seat in its voice room is live, newer than every
// seat stamped before.
func (c *Client) stampSeat(clock uint64) {
	c.mu.Lock()
	c.seat = clock
	c.mu.Unlock()
}

func (c *Client) seatStamp() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.seat
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

const (
	// maxBrowserSockets bounds the browser connections of one user; every
	// per-connection budget multiplies with them.
	maxBrowserSockets = 8
	// closeReplaced is the close code of an evicted connection. The web
	// client does not reconnect on it by itself (it would evict another tab
	// in turn); the tab reconnects when its user returns to it.
	closeReplaced = 4001
)

// evict closes a connection that gave way to a newer one of the same user.
func (c *Client) evict() {
	if c.conn != nil {
		_ = c.conn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(closeReplaced, "too many connections"), time.Now().Add(writeWait))
	}
	c.shutdown()
	c.close()
}
