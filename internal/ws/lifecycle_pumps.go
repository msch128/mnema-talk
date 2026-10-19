package ws

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
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
	if c.native != nil && messageType != websocket.TextMessage {
		c.hub.terminateNativeClient(c)
		return false
	}
	// Per-connection flood guard: excess events are dropped.
	now := time.Now()
	if !budget.allowFrame(now) {
		return true
	}
	var ev struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(message, &ev); err != nil {
		return true
	}
	if c.native != nil && !c.commandsAllowed() {
		c.hub.terminateNativeClient(c)
		return false
	}
	if c.handleNativeControl(ev.Type, message) {
		return true
	}
	if budget.allowEvent(ev.Type, len(ev.Payload), now) {
		c.handle(ev.Type, ev.Payload)
	}
	return true
}

func (c *Client) writePump() {
	ping := time.NewTicker(c.hub.pingEvery())
	revalidate := time.NewTicker(c.hub.revalidatePeriod())
	defer func() {
		ping.Stop()
		revalidate.Stop()
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
		}
	}
}

// writeQueued sends one queued message; false ends the connection.
func (c *Client) writeQueued(msg []byte) bool {
	if c.native != nil && !c.commandsAllowed() {
		c.hub.terminateNativeClient(c)
		return false
	}
	deadline := time.Now().Add(writeWait)
	if c.native != nil {
		_, _, leaseDeadline := c.native.snapshot()
		deadline = minTime(deadline, leaseDeadline)
	}
	_ = c.conn.SetWriteDeadline(deadline)
	return c.conn.WriteMessage(websocket.TextMessage, msg) == nil
}

// writePing sends a keep-alive ping; false ends the connection.
func (c *Client) writePing() bool {
	if c.native != nil && !c.commandsAllowed() {
		c.hub.terminateNativeClient(c)
		return false
	}
	_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
	return c.conn.WriteMessage(websocket.PingMessage, nil) == nil
}

// sessionStillValid checks a browser connection's token version, so a
// password change or account deletion ends live connections too. It closes
// the socket and reports false when the session was revoked. Native
// connections have their own lease and are not checked here.
func (c *Client) sessionStillValid() bool {
	if c.native != nil {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	tv, err := c.hub.tokenVersion(ctx, c.User.ID)
	cancel()
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && tv != c.tokenVersion) {
		c.writeSessionRevokedClose()
		return false
	}
	if err != nil {
		slog.Warn("revalidate token version failed", "user", c.User.ID, "err", err)
	}
	return true
}

func (c *Client) writeSessionRevokedClose() {
	// Revalidation may run after the previous ping's write deadline expired.
	// Control writes use their own fresh bounded deadline before teardown.
	_ = c.conn.WriteControl(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "session revoked"), time.Now().Add(writeWait))
}
