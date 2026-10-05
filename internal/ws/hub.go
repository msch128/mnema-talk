// Package ws is the real-time hub: one WebSocket per browser session carrying
// chat events, presence, voice state and WebRTC signaling for the SFU.
package ws

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/sfu"
	"github.com/pion/webrtc/v4"
)

const (
	sendBuffer      = 256
	maxMessageBytes = 64 << 10 // SDP offers are a few KB; nothing legitimate is larger
	pongWait        = 60 * time.Second
	pingInterval    = 30 * time.Second
	writeWait       = 10 * time.Second
	revalidateEvery = 2 * time.Minute
	maxEventsPerSec = 40
)

type Event struct {
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}

// Hub tracks connected clients, online presence and voice rooms. It implements
// events.Publisher.
type Hub struct {
	DB       *db.Pool
	Sessions Authenticator
	SFU      *sfu.SFU
	Origins  []string

	mu      sync.RWMutex
	clients map[*Client]struct{}
	online  map[uuid.UUID]int
	// chosen is the presence each connected user picked (online, away, dnd, focus).
	chosen map[uuid.UUID]string
	// profiles is the latest public profile of each connected user; it
	// follows user_update events so voice payloads never show a stale name
	// or avatar.
	profiles map[uuid.UUID]auth.User
	// voice maps channel → user → user info for everyone currently in a voice room.
	voice map[uuid.UUID]map[uuid.UUID]auth.User
	// grace holds users whose connection dropped while in a voice room. They
	// stay listed in it for VoiceGrace so a page reload rejoins without a
	// leave/join flicker for everyone else (like Discord).
	grace map[voiceKey]*graceLeave
	// voiceSince is when a user joined a voice room; roomSince is when each
	// room got its first member. Both survive the grace period.
	voiceSince map[voiceKey]time.Time
	// voiceMute is the mute/deafen state of a user in a voice room.
	voiceMute map[voiceKey]MuteState
	roomSince map[uuid.UUID]time.Time

	// VoiceGrace is how long a dropped voice user stays in the room.
	VoiceGrace time.Duration

	upgrader websocket.Upgrader
}

type graceLeave struct {
	channelID uuid.UUID
	timer     *time.Timer
}

// DefaultVoiceGrace matches the client, which auto-rejoins within 30 seconds.
const DefaultVoiceGrace = 30 * time.Second

// Authenticator resolves a request's session cookie (implemented by
// *auth.Sessions). The token version is the one in the verified cookie.
type Authenticator interface {
	AuthenticateRequest(r *http.Request) (*auth.User, int, error)
}

func NewHub(p *db.Pool, sessions Authenticator, voiceSFU *sfu.SFU, origins []string) *Hub {
	h := &Hub{
		DB:       p,
		Sessions: sessions,
		SFU:      voiceSFU,
		Origins:  origins,
		clients:  map[*Client]struct{}{},
		online:   map[uuid.UUID]int{},
		chosen:   map[uuid.UUID]string{},
		profiles: map[uuid.UUID]auth.User{},
		voice:    map[uuid.UUID]map[uuid.UUID]auth.User{},
		grace:    map[voiceKey]*graceLeave{},

		voiceSince: map[voiceKey]time.Time{},
		voiceMute:  map[voiceKey]MuteState{},
		roomSince:  map[uuid.UUID]time.Time{},

		VoiceGrace: DefaultVoiceGrace,
	}
	h.upgrader = websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		// Cross-site WebSocket hijacking guard: only the app's own origin may connect.
		CheckOrigin: func(r *http.Request) bool {
			return r.Header.Get("Origin") != "" && httpx.CheckSameOrigin(r, h.Origins)
		},
	}
	if voiceSFU != nil {
		voiceSFU.SetMediaStateHandler(h.announceMediaState)
	}
	return h
}

// announceMediaState tells all clients that userID started or stopped
// a screen share or camera so they can show the LIVE indicator and offer to watch.
func (h *Hub) announceMediaState(roomID, userID uuid.UUID, st sfu.MediaState) {
	h.Broadcast("webrtc_media_state", mediaStatePayload(roomID, userID, st))
}

func mediaStatePayload(roomID, userID uuid.UUID, st sfu.MediaState) map[string]any {
	return map[string]any{"channel_id": roomID, "user_id": userID, "screen": st.Screen, "camera": st.Camera}
}

// Client is one WebSocket connection.
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte
	User auth.User

	tokenVersion int
	closeOnce    sync.Once

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

func encode(eventType string, payload any) []byte {
	data, err := json.Marshal(Event{Type: eventType, Payload: payload})
	if err != nil {
		slog.Error("encode ws event", "type", eventType, "err", err)
		return nil
	}
	return data
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
		go c.close()
	}
}

func (c *Client) SendEvent(eventType string, payload any) {
	if data := encode(eventType, payload); data != nil {
		c.deliver(data)
	}
}

// Broadcast sends an event to every connected client.
func (h *Hub) Broadcast(eventType string, payload any) {
	data := encode(eventType, payload)
	if data == nil {
		return
	}
	if eventType == "user_update" {
		h.applyUserUpdate(payload)
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		c.deliver(data)
	}
}

// broadcastExcept sends an event to every connected user except userID.
func (h *Hub) broadcastExcept(userID uuid.UUID, eventType string, payload any) {
	data := encode(eventType, payload)
	if data == nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		if c.User.ID != userID {
			c.deliver(data)
		}
	}
}

// SendToUsers sends an event to all sessions of the given users.
func (h *Hub) SendToUsers(userIDs []uuid.UUID, eventType string, payload any) {
	data := encode(eventType, payload)
	if data == nil {
		return
	}
	want := make(map[uuid.UUID]bool, len(userIDs))
	for _, id := range userIDs {
		want[id] = true
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		if want[c.User.ID] {
			c.deliver(data)
		}
	}
}

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
// @Description - `voice_speaking`: `{channel_id, user_id, active}`.
// @Description - `typing`: `{channel_id, user_id}` (to everyone except the typist).
// @Description
// @Description Server to client, targeted:
// @Description - `read_state`: to the user's own sessions only; `{channel_id, last_read_at, unread_count, mention_count}`, `{channel_id, last_read_at, refresh: true}` or `{channel_id, notify_level}`.
// @Description - `webrtc_media_state`: `{channel_id, user_id, screen, camera}` to voice-room members.
// @Description - `webrtc_offer` (SDP offer), `webrtc_candidate` (ICE candidate): SFU signalling.
// @Description - `voice_kicked`: `{channel_id}` when an admin removes the user from voice.
// @Description - `pong`: `{t}` echoing a `ping`.
// @Description
// @Description Client to server:
// @Description - `ping` `{t}`; `presence_idle` `{idle: bool}`.
// @Description - `typing` `{channel_id}` (text channels only, rate-limited).
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
// @Router /api/ws [get]
func (h *Hub) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
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
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		// The upgrader already answered (e.g. 403 for a foreign origin).
		slog.Warn("websocket upgrade failed", "err", err)
		return
	}
	c := newClient(h, conn, *user, tv)
	h.register(c)
	go c.writePump()
	go c.readPump()
}

func (h *Hub) tokenVersion(ctx context.Context, userID uuid.UUID) (int, error) {
	var tv int
	err := h.DB.QueryRow(ctx, `SELECT token_version FROM users WHERE id = $1`, userID).Scan(&tv)
	return tv, err
}

func (h *Hub) register(c *Client) {
	h.mu.Lock()
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

// StatusOffline is the live status of a user without an open connection.
const StatusOffline = "offline"

// statusLocked is userID's live status: offline without a connection, else
// the chosen presence, where "online" reads as "away" while every connection
// of the user is idle. Callers hold h.mu.
func (h *Hub) statusLocked(userID uuid.UUID) string {
	if h.online[userID] <= 0 {
		return StatusOffline
	}
	chosen := h.chosen[userID]
	if chosen == "" {
		chosen = auth.PresenceOnline
	}
	if chosen != auth.PresenceOnline {
		return chosen
	}
	for c := range h.clients {
		if c.User.ID == userID && !c.idle {
			return auth.PresenceOnline
		}
	}
	return auth.PresenceAway
}

// announcePresence tells everyone about a changed live status.
func (h *Hub) announcePresence(userID uuid.UUID, before, after string) {
	if before != after {
		h.Broadcast("presence_update", map[string]any{"user_id": userID, "status": after})
	}
}

// SetPresence applies a newly chosen presence to userID's live status.
func (h *Hub) SetPresence(userID uuid.UUID, presence string) {
	if !auth.ValidPresence(presence) {
		return
	}
	h.mu.Lock()
	before := h.statusLocked(userID)
	if h.online[userID] > 0 {
		h.chosen[userID] = presence
	}
	after := h.statusLocked(userID)
	h.mu.Unlock()
	h.announcePresence(userID, before, after)
}

// setIdle records whether c has gone idle and announces a resulting change.
func (h *Hub) setIdle(c *Client, idle bool) {
	h.mu.Lock()
	if _, ok := h.clients[c]; !ok {
		h.mu.Unlock()
		return
	}
	before := h.statusLocked(c.User.ID)
	c.idle = idle
	after := h.statusLocked(c.User.ID)
	h.mu.Unlock()
	h.announcePresence(c.User.ID, before, after)
}

// OnlineCount returns the number of distinct online users.
func (h *Hub) OnlineCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.online)
}

// presenceSnapshot maps every connected user to their live status.
func (h *Hub) presenceSnapshot() map[uuid.UUID]string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make(map[uuid.UUID]string, len(h.online))
	for id := range h.online {
		out[id] = h.statusLocked(id)
	}
	return out
}

// OnlineUserIDs lists every user with an open connection (for @here).
func (h *Hub) OnlineUserIDs() []uuid.UUID {
	h.mu.RLock()
	defer h.mu.RUnlock()
	ids := make([]uuid.UUID, 0, len(h.online))
	for id := range h.online {
		ids = append(ids, id)
	}
	return ids
}

// MuteState is whether a voice member muted their microphone or deafened.
type MuteState struct {
	Muted    bool `json:"muted"`
	Deafened bool `json:"deafened"`
}

// VoiceUser is a member of a voice room, since when they are in it and
// whether they are muted or deafened.
type VoiceUser struct {
	auth.User
	JoinedAt time.Time `json:"joined_at"`
	MuteState
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

	windowStart, count := time.Now(), 0
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		// Per-connection flood guard: excess events are dropped.
		if now := time.Now(); now.Sub(windowStart) >= time.Second {
			windowStart, count = now, 0
		}
		if count++; count > maxEventsPerSec {
			continue
		}
		var ev struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(message, &ev); err != nil {
			continue
		}
		c.handle(ev.Type, ev.Payload)
	}
}

func (c *Client) handle(eventType string, payload json.RawMessage) {
	h := c.hub
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var p struct {
		ChannelID uuid.UUID `json:"channel_id"`
		Active    bool      `json:"active"`
		Idle      bool      `json:"idle"`
		T         float64   `json:"t"`
	}
	_ = json.Unmarshal(payload, &p)

	switch eventType {
	case "ping":
		c.SendEvent("pong", map[string]any{"t": p.T})

	case "presence_idle":
		h.setIdle(c, p.Idle)

	case "voice_join":
		if ch, ok := h.voiceChannel(ctx, p.ChannelID); ok {
			h.joinVoice(c, ch)
		}

	case "voice_leave":
		h.leaveCurrentVoice(c)

	case "typing":
		if !c.allowTyping(p.ChannelID, time.Now()) {
			return
		}
		if ch, err := chat.LoadChannel(ctx, h.DB, p.ChannelID); err == nil && ch.Type == chat.ChannelTypeText {
			h.broadcastExcept(c.User.ID, "typing", map[string]any{"channel_id": ch.ID, "user_id": c.User.ID})
		}

	case "voice_speaking":
		if cur := c.currentVoice(); cur != nil {
			// A muted or deafened member is never shown as speaking.
			active := p.Active && !h.isMuted(c.User.ID, *cur)
			if c.speakingChanged(active, time.Now()) {
				h.sendToVoiceRoom(*cur, "voice_speaking", map[string]any{"channel_id": *cur, "user_id": c.User.ID, "active": active})
			}
		}

	case "voice_mute_state":
		var st MuteState
		if json.Unmarshal(payload, &st) == nil {
			h.setMuteState(c, st)
		}

	case "webrtc_answer":
		var answer webrtc.SessionDescription
		if err := json.Unmarshal(payload, &answer); err == nil {
			if peer := c.peer(); peer != nil {
				if err := peer.SetAnswer(answer); err != nil {
					slog.Warn("sfu set remote description", "user", c.User.ID, "err", err)
				}
			}
		}

	case "webrtc_candidate":
		var cand webrtc.ICECandidateInit
		if err := json.Unmarshal(payload, &cand); err == nil {
			slog.Debug("sfu remote candidate", "user", c.User.Username, "candidate", cand.Candidate)
			if peer := c.peer(); peer != nil {
				if err := peer.PC.AddICECandidate(cand); err != nil {
					slog.Debug("sfu add ice candidate", "user", c.User.ID, "err", err)
				}
			}
		}

	case "webrtc_diag":
		// A browser's own view of its voice connection, for troubleshooting.
		if len(payload) <= 4096 && c.allowDiag(time.Now()) {
			slog.Debug("webrtc client diag", "user", c.User.Username, "diag", json.RawMessage(payload))
		}

	case "webrtc_request_keyframe":
		if cur := c.currentVoice(); cur != nil && h.SFU != nil {
			if room := h.SFU.Room(*cur); room != nil {
				room.DispatchKeyframe(c.User.ID)
			}
		}

	case "webrtc_subscribe":
		c.handleSubscribe(payload)

	case "webrtc_screenshare_start":
		if cur := c.currentVoice(); cur != nil && h.SFU != nil {
			if room := h.SFU.Room(*cur); room != nil {
				room.DispatchKeyframe(c.User.ID)
			}
		}

	case "webrtc_screenshare_stop":
		if cur := c.currentVoice(); cur != nil && h.SFU != nil {
			if room := h.SFU.Room(*cur); room != nil {
				room.RemoveUserSource(c.User.ID, sfu.SourceScreen)
			}
		}

	case "webrtc_camera_stop":
		if cur := c.currentVoice(); cur != nil && h.SFU != nil {
			if room := h.SFU.Room(*cur); room != nil {
				room.RemoveUserSource(c.User.ID, sfu.SourceCamera)
			}
		}
	}
}

// handleSubscribe applies a viewer's choice of which video to receive:
// {kind: "screen"|"camera", user_id, on} for one publisher, or
// {kind: "camera", all: true, on} for every camera.
func (c *Client) handleSubscribe(payload json.RawMessage) {
	req, ok := parseSubscribe(payload, c.User.ID)
	if !ok {
		return
	}
	cur := c.currentVoice()
	if cur == nil || c.hub.SFU == nil {
		return
	}
	room := c.hub.SFU.Room(*cur)
	if room == nil {
		return
	}
	if err := room.Subscribe(c.User.ID, req.publisher, req.kind, req.all, req.on); err != nil {
		slog.Debug("sfu subscribe rejected", "user", c.User.ID, "err", err)
	}
}

// subscribeRequest is a validated webrtc_subscribe.
type subscribeRequest struct {
	kind      sfu.Source
	publisher uuid.UUID // uuid.Nil with all
	all       bool
	on        bool
}

// parseSubscribe validates a webrtc_subscribe payload from viewer: a screen
// or camera of one other user, or every camera at once.
func parseSubscribe(payload json.RawMessage, viewer uuid.UUID) (subscribeRequest, bool) {
	var req struct {
		Kind   string `json:"kind"`
		UserID string `json:"user_id"`
		All    bool   `json:"all"`
		On     bool   `json:"on"`
	}
	if json.Unmarshal(payload, &req) != nil {
		return subscribeRequest{}, false
	}
	kind := sfu.Source(req.Kind)
	if kind != sfu.SourceScreen && kind != sfu.SourceCamera {
		return subscribeRequest{}, false
	}
	if req.All && kind != sfu.SourceCamera {
		return subscribeRequest{}, false
	}
	out := subscribeRequest{kind: kind, all: req.All, on: req.On}
	if !req.All {
		id, err := uuid.Parse(req.UserID)
		if err != nil || id == uuid.Nil || id == viewer {
			return subscribeRequest{}, false
		}
		out.publisher = id
	}
	return out, true
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
