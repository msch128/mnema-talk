package ws

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/metrics"
)

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister(c)
		c.close()
	}()

	c.conn.SetReadLimit(maxMessageBytes)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	var budget eventBudget
	for {
		messageType, message, err := c.conn.ReadMessage()
		if err != nil || !c.dispatchFrame(&budget, messageType, message) {
			return
		}
	}
}

// dispatchFrame routes one received frame. It reports false when the
// connection must end.
func (c *Client) dispatchFrame(budget *eventBudget, messageType int, message []byte) bool {
	if c.rejectNativeBinary(messageType) {
		return false
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
	if !c.commandsAllowed() {
		c.hub.terminateNativeClient(c)
		return false
	}
	if c.handleNativeControl(ev.Type, message) {
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
			if !c.writeQueued(msg) {
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
	if !c.commandsAllowed() {
		c.hub.terminateNativeClient(c)
		return false
	}
	_ = c.conn.SetWriteDeadline(c.leaseWriteDeadline(time.Now().Add(writeWait)))
	return c.conn.WriteMessage(websocket.TextMessage, msg) == nil
}

// writePing sends a keep-alive ping; false ends the connection.
func (c *Client) writePing() bool {
	if !c.commandsAllowed() {
		c.hub.terminateNativeClient(c)
		return false
	}
	_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
	return c.conn.WriteMessage(websocket.PingMessage, nil) == nil
}

// sessionStillValid checks a browser connection against its session: the
// token version (password change, logout everywhere, account deletion), the
// session id (single sign-out) and the token's own expiry. It closes the socket
// and reports false when the session is gone. A database error keeps the socket
// for a few rounds and then closes it, since the session can no longer be
// proven alive. Native connections have their own lease and are not checked.
func (c *Client) sessionStillValid() bool {
	if c.hasNativeLease() {
		return true
	}
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
	slog.Warn("revalidate session failed", "user", c.User.ID, "err", err)
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
// a stop function. Without an expiry (native, in-memory) it never fires.
func (c *Client) sessionExpiry() (<-chan time.Time, func()) {
	if c.sessionExp.IsZero() || c.hasNativeLease() {
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
