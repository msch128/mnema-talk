package ws

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/mnema-xyz/talk/internal/metrics"
)

func (c *Client) readPump() {
	// Two steps, so a panic while unregistering still closes the socket.
	defer c.close()
	defer c.hub.unregister(c)

	c.conn.SetReadLimit(maxMessageBytes)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	var budget eventBudget
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil || !c.dispatchFrame(&budget, message) {
			return
		}
	}
}

// dispatchFrame routes one received frame. It reports false when the
// connection must end.
func (c *Client) dispatchFrame(budget *eventBudget, message []byte) bool {
	if c.ending.Load() {
		return true // being banned or kicked: the notice flushes, nothing is acted on
	}
	// Per-connection flood guard: excess events are dropped.
	now := time.Now()
	if !budget.allowFrame(now) {
		return true
	}
	if !c.hub.userBudgets.allowFrame(c.User.ID, now) {
		countDroppedFrame()
		return true
	}
	// A frame too big for its type is dropped before any decoding.
	if frameTooLarge(message) {
		metrics.WSDropped.With("oversize_event").Inc()
		return true
	}
	var ev struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(message, &ev); err != nil {
		return true
	}
	if budget.allowEvent(ev.Type, len(ev.Payload), now) && c.userAllowsEvent(ev.Type, len(ev.Payload), now) {
		c.handle(ev.Type, ev.Payload)
	}
	return true
}

// userAllowsEvent applies the account-wide budget shared by all of the user's
// connections.
func (c *Client) userAllowsEvent(eventType string, payloadLen int, now time.Time) bool {
	if c.hub.userBudgets.allowEvent(c.User.ID, eventType, payloadLen, now) {
		return true
	}
	countDroppedEvent(eventType)
	return false
}

func (c *Client) writePump() {
	ping := time.NewTicker(c.hub.pingEvery())
	revalidate := time.NewTicker(c.hub.revalidatePeriod())
	expired, stopExpiry := c.sessionExpiry()
	defer func() {
		ping.Stop()
		revalidate.Stop()
		stopExpiry()
		c.close()
	}()

	for {
		select {
		case <-c.done:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
			return
		case msg := <-c.send:
			if !c.writeBatch(msg) {
				return
			}
		case <-ping.C:
			if !c.writePing() {
				return
			}
		case <-revalidate.C:
			if !c.sessionStillValid() {
				return
			}
		case <-expired:
			c.writeSessionRevokedClose()
			return
		}
	}
}

// writeQueued sends one queued message; false ends the connection.
func (c *Client) writeQueued(msg []byte) bool {
	_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
	return c.conn.WriteMessage(websocket.TextMessage, msg) == nil
}

// writePing sends a keep-alive ping; false ends the connection.
func (c *Client) writePing() bool {
	_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
	return c.conn.WriteMessage(websocket.PingMessage, nil) == nil
}

// sessionStillValid checks a browser connection against its session: the
// token version (password change, logout everywhere, account deletion), the
// session id (single sign-out) and the token's own expiry. It closes the socket
// and reports false when the session is gone. A database error keeps the socket
// for a few rounds and then closes it, since the session can no longer be
// proven alive.
func (c *Client) sessionStillValid() bool {
	if c.sessionExpired(time.Now()) {
		c.writeSessionRevokedClose()
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	tv, revoked, err := c.hub.sessionState(ctx, c.User.ID, c.sessionID)
	cancel()
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (revoked || tv != c.tokenVersion)) {
		c.writeSessionRevokedClose()
		return false
	}
	if err == nil {
		c.revalidateFails = 0
		return true
	}
	c.logger().Warn("revalidate session failed", "user", c.User.ID, "err", err)
	c.revalidateFails++
	if c.revalidateFails >= maxRevalidateFailures {
		c.writeSessionRevokedClose()
		return false
	}
	return true
}

// sessionExpired reports that the session token's expiry has passed.
func (c *Client) sessionExpired(now time.Time) bool {
	return !c.sessionExp.IsZero() && !now.Before(c.sessionExp)
}

// sessionExpiry returns a channel that fires at the session token's expiry, and
// a stop function. Without an expiry (in-memory) it never fires.
func (c *Client) sessionExpiry() (<-chan time.Time, func()) {
	if c.sessionExp.IsZero() {
		return nil, func() {}
	}
	t := time.NewTimer(time.Until(c.sessionExp))
	return t.C, func() { t.Stop() }
}

func (c *Client) writeSessionRevokedClose() {
	// Revalidation may run after the previous ping's write deadline expired.
	// Control writes use their own fresh bounded deadline before teardown.
	_ = c.conn.WriteControl(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "session revoked"), time.Now().Add(writeWait))
}

// batchConn lets the write loop send the frames that are already queued with
// one write to the network instead of one write each. Every presence change is
// a frame to every connection: when a restarted server takes all its members
// back within seconds, each connection is sent one frame per member that comes
// online, and one system call per frame was most of the server's work.
//
// Outside a batch every write goes straight through. Inside one (hold until
// flush) writes are gathered, up to batchBytes; a write that would pass that
// first sends what was gathered. Control frames other goroutines write in the
// meantime (a pong, a close) join the batch in order and leave with it.
type batchConn struct {
	net.Conn
	mu      sync.Mutex
	holding bool
	buf     []byte
}

// batchBytes caps what one batch gathers before it is sent.
const batchBytes = 32 << 10

// keptBatchBuffer is the largest buffer kept between batches: an idle
// connection should not hold on to the memory of a burst.
const keptBatchBuffer = 4 << 10

func (b *batchConn) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.holding && len(b.buf)+len(p) <= batchBytes {
		b.buf = append(b.buf, p...)
		return len(p), nil
	}
	if err := b.sendLocked(); err != nil {
		return 0, err
	}
	return b.Conn.Write(p)
}

// hold starts a batch.
func (b *batchConn) hold() {
	b.mu.Lock()
	b.holding = true
	b.mu.Unlock()
}

// flush ends the batch and sends what it gathered.
func (b *batchConn) flush() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.holding = false
	return b.sendLocked()
}

func (b *batchConn) sendLocked() error {
	if len(b.buf) == 0 {
		return nil
	}
	_, err := b.Conn.Write(b.buf)
	if cap(b.buf) > keptBatchBuffer {
		b.buf = nil
	} else {
		b.buf = b.buf[:0]
	}
	return err
}

// batchingWriter hands the upgrader a connection that can batch.
type batchingWriter struct {
	http.ResponseWriter
}

var errNoHijack = errors.New("websocket: response does not support hijacking")

func (w batchingWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, brw, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err != nil {
		if errors.Is(err, http.ErrNotSupported) {
			err = errNoHijack
		}
		return nil, nil, err
	}
	return &batchConn{Conn: conn}, brw, nil
}

func (w batchingWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// maxBatchFrames caps the frames one batch takes from the send buffer, so a
// steady stream still reaches the network in pieces.
const maxBatchFrames = 64

// writeBatch sends msg and the frames queued behind it, up to maxBatchFrames,
// in one batch when the connection can batch. false ends the connection.
func (c *Client) writeBatch(msg []byte) bool {
	batch, _ := c.conn.NetConn().(*batchConn)
	if batch == nil {
		return c.writeQueued(msg)
	}
	batch.hold()
	ok := c.writeQueued(msg)
	for n := 1; ok && n < maxBatchFrames; n++ {
		select {
		case next := <-c.send:
			ok = c.writeQueued(next)
		default:
			n = maxBatchFrames
		}
	}
	return batch.flush() == nil && ok
}
