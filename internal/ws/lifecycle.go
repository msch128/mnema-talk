package ws

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
)

const (
	sendBuffer      = 256
	maxMessageBytes = 64 << 10 // SDP offers are a few KB; nothing legitimate is larger
	pongWait        = 60 * time.Second
	pingInterval    = 30 * time.Second
	writeWait       = 10 * time.Second
	revalidateEvery = 2 * time.Minute
)

// HandleWebSocket authenticates the session cookie, checks the origin and
// upgrades the connection.
//
// @Summary WebSocket event stream
// @Description Upgrades to a WebSocket (requires a same-origin Origin header; a foreign origin is refused with 403 by the upgrade handler). OpenAPI cannot model WebSocket frames, so the protocol is described here.
// @Description
// @Description Every frame in both directions is a JSON text message `{"type": string, "payload": any}`. At most a few events per second per connection are processed; excess client events are dropped.
// @Description
// @Description Server to client, on connect (to this connection only):
// @Description - `presence_snapshot`: map of user id to live status for every online user.
// @Description - `voice_snapshot`: map of voice channel id to a map of user id to User plus `joined_at`.
// @Description - `voice_rooms`: `{started: {channelId: time}, now}`; the server clock lets clients count up.
// @Description
// @Description Server to client, broadcast to all connections:
// @Description - `message_create`, `message_update`: Message.
// @Description - `message_delete`: `{id, channel_id, parent_id}`.
// @Description - `message_reaction`: `{message_id, reactions}`.
// @Description - `channels_changed`: no payload; refetch GET /api/channels (sent after any category/channel/layout admin change).
// @Description - `member_joined`: public User of a newly registered member.
// @Description - `user_update`: public User after a profile, status or avatar change, or `{id, disabled}` when an admin disables or enables an account.
// @Description - `user_stats`: `{user_id, voice_seconds}` after a voice stay ends.
// @Description - `presence_update`: `{user_id, status}` where status is online, away, dnd, focus or offline.
// @Description - `voice_state_update`: `{action: "join", channel_id, user, started_at}` or `{action: "leave", channel_id, user_id}`.
// @Description - `typing`: `{channel_id, user_id}` (to everyone except the typist).
// @Description
// @Description Server to client, targeted:
// @Description - `read_state`: to the user's own sessions only; `{channel_id, last_read_at, unread_count, mention_count}`, `{channel_id, last_read_at, refresh: true}` or `{channel_id, notify_level}`.
// @Description - `webrtc_media_state`: `{channel_id, user_id, screen, camera}` to voice-room members.
// @Description - `voice_speaking`: `{channel_id, user_id, active}` to voice-room members, only when the state changes.
// @Description - `screen_viewers`: `{channel_id, user_id, viewers}` to voice-room members: `viewers` are the distinct users in the room watching `user_id`'s screen share (sorted ids, never the sharer). Sent whenever that set changes (`viewers: []` once when a watched share ends or loses its last viewer), and on `voice_join` one per live share to the joining connection only.
// @Description - `webrtc_offer` (SDP offer), `webrtc_candidate` (ICE candidate): SFU signalling.
// @Description - `voice_kicked`: `{channel_id}` when an admin removes the user from voice.
// @Description - `pong`: `{t}` echoing a `ping`.
// @Description
// @Description Client to server:
// @Description - `ping` `{t}`; `presence_idle` `{idle: bool}`.
// @Description - `typing` `{channel_id}` (text and voice channels, rate-limited).
// @Description - `voice_join` `{channel_id}`; `voice_leave`; `voice_speaking` `{active}`.
// @Description - `webrtc_answer` (SDP answer), `webrtc_candidate` (ICE candidate), `webrtc_request_keyframe`.
// @Description - `webrtc_subscribe` `{kind: "screen"|"camera", user_id, on}` or `{kind: "camera", all: true, on}`.
// @Description - `webrtc_screenshare_stop`, `webrtc_camera_stop`.
// @Description
// @Description Unknown event types are ignored. The server closes the connection when the session is revoked or the account is disabled.
// @ID connectWebSocket
// @Tags Realtime
// @Produce json
// @Security cookieAuth
// @Param Upgrade header string true "Must be websocket." Enums(websocket)
// @Success 101 "Switching Protocols; the connection is now a WebSocket."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Failure 503 {object} httpx.ErrorResponse "UNAVAILABLE: server shutting down."
// @Router /api/ws [get]
func (h *Hub) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	if h.isClosed() {
		httpx.WriteError(w, httpx.ErrUnavailable("server shutting down"))
		return
	}
	// The version is the cookie's own: re-reading it from the database here
	// would let a revocation between the two reads slip through, and the
	// connection would then outlive it.
	user, tv, err := h.Sessions.AuthenticateRequest(r)
	if err != nil {
		if _, ok := httpx.AsAPIError(err); !ok {
			err = httpx.ErrServer(err)
		}
		httpx.WriteError(w, err)
		return
	}
	if h.isClosed() {
		httpx.WriteError(w, httpx.ErrUnavailable("server shutting down"))
		return
	}
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		// The upgrader already answered (e.g. 403 for a foreign origin).
		slog.Warn("websocket upgrade failed", "err", err)
		return
	}
	c := newClient(h, conn, *user, tv)
	if !h.register(c) {
		c.shutdown()
		c.close()
		return
	}
	go c.writePump()
	go c.readPump()
}

func (h *Hub) tokenVersion(ctx context.Context, userID uuid.UUID) (int, error) {
	var tv int
	err := h.DB.QueryRow(ctx, `SELECT token_version FROM users WHERE id = $1`, userID).Scan(&tv)
	return tv, err
}

func (h *Hub) register(c *Client) bool {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return false
	}
	before := h.statusLocked(c.User.ID)
	h.clients[c] = struct{}{}
	h.online[c.User.ID]++
	// The session was just loaded from the database: the freshest profile.
	h.profiles[c.User.ID] = c.User.Public()
	// The session was just loaded, so its presence is the latest choice.
	if auth.ValidPresence(c.User.Presence) {
		h.chosen[c.User.ID] = c.User.Presence
	} else if h.chosen[c.User.ID] == "" {
		h.chosen[c.User.ID] = auth.PresenceOnline
	}
	after := h.statusLocked(c.User.ID)
	h.mu.Unlock()

	slog.Info("ws connected", "user", c.User.Username)
	h.announcePresence(c.User.ID, before, after)
	c.SendEvent("server_info", h.serverInfo())
	c.SendEvent("presence_snapshot", h.presenceSnapshot())
	c.SendEvent("voice_snapshot", h.voiceSnapshot())
	c.SendEvent("voice_rooms", h.voiceRooms())
	if h.SFU != nil {
		for chID, states := range h.SFU.AllMediaStates() {
			for uid, st := range states {
				c.SendEvent("webrtc_media_state", mediaStatePayload(chID, uid, st))
			}
		}
	}
	return true
}

func (h *Hub) isClosed() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.closed
}

// Close rejects new connections and terminates upgraded sockets. HTTP server
// shutdown does not close hijacked WebSockets. Peer teardown belongs to the
// SFU owner; closing sockets here never waits on its network operations.
func (h *Hub) Close() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	clients := make([]*Client, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	for key, grace := range h.grace {
		grace.timer.Stop()
		delete(h.grace, key)
	}
	h.mu.Unlock()
	for _, c := range clients {
		c.shutdown()
		c.close()
	}
}

func (h *Hub) unregister(c *Client) {
	h.dropVoiceForGrace(c)
	h.mu.Lock()
	if _, ok := h.clients[c]; !ok {
		h.mu.Unlock()
		return
	}
	before := h.statusLocked(c.User.ID)
	delete(h.clients, c)
	c.shutdown()
	h.online[c.User.ID]--
	offline := h.online[c.User.ID] <= 0
	if offline {
		delete(h.online, c.User.ID)
		delete(h.chosen, c.User.ID)
		delete(h.profiles, c.User.ID)
	}
	after := h.statusLocked(c.User.ID)
	h.mu.Unlock()

	slog.Info("ws disconnected", "user", c.User.Username)
	if offline {
		if h.DB != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if _, err := h.DB.Exec(ctx, `UPDATE users SET last_seen_at = NOW() WHERE id = $1`, c.User.ID); err != nil {
				slog.Warn("record last seen", "user", c.User.ID, "err", err)
			}
			cancel()
		}
	}
	h.announcePresence(c.User.ID, before, after)
}

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
		_, message, err := c.conn.ReadMessage()
		if err != nil {
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
		if !budget.allowEvent(ev.Type, len(ev.Payload), now) {
			continue
		}
		c.handle(ev.Type, ev.Payload)
	}
}

func (c *Client) writePump() {
	ping := time.NewTicker(pingInterval)
	revalidate := time.NewTicker(revalidateEvery)
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
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ping.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-revalidate.C:
			// A password change or account deletion ends live connections too.
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			tv, err := c.hub.tokenVersion(ctx, c.User.ID)
			cancel()
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && tv != c.tokenVersion) {
				_ = c.conn.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "session revoked"))
				return
			} else if err != nil {
				slog.Warn("revalidate token version failed", "user", c.User.ID, "err", err)
			}
		}
	}
}
