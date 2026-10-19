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
		if err != nil {
			return
		}
		if c.native != nil && messageType != websocket.TextMessage {
			c.hub.terminateNativeClient(c)
			return
		}
		// Per-connection flood guard: excess events are dropped.
		now := time.Now()
		if !budget.allowFrame(now) {
			continue
		}
		var ev struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(message, &ev); err != nil {
			continue
		}
		if c.native != nil && !c.nativeLive() {
			c.hub.terminateNativeClient(c)
			return
		}
		if c.handleNativeControl(ev.Type, message) {
			continue
		}
		if !budget.allowEvent(ev.Type, len(ev.Payload), now) {
			continue
		}
		c.handle(ev.Type, ev.Payload)
	}
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
			if c.native != nil && !c.nativeLive() {
				c.hub.terminateNativeClient(c)
				return
			}
			deadline := time.Now().Add(writeWait)
			if c.native != nil {
				_, _, leaseDeadline := c.native.snapshot()
				deadline = minTime(deadline, leaseDeadline)
			}
			_ = c.conn.SetWriteDeadline(deadline)
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ping.C:
			if c.native != nil && !c.nativeLive() {
				c.hub.terminateNativeClient(c)
				return
			}
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-revalidate.C:
			if c.native != nil {
				continue
			}
			// A password change or account deletion ends live connections too.
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			tv, err := c.hub.tokenVersion(ctx, c.User.ID)
			cancel()
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && tv != c.tokenVersion) {
				c.writeSessionRevokedClose()
				return
			} else if err != nil {
				slog.Warn("revalidate token version failed", "user", c.User.ID, "err", err)
			}
		}
	}
}

func (c *Client) writeSessionRevokedClose() {
	// Revalidation may run after the previous ping's write deadline expired.
	// Control writes use their own fresh bounded deadline before teardown.
	_ = c.conn.WriteControl(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "session revoked"), time.Now().Add(writeWait))
}
