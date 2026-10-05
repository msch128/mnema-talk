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
	Sessions *auth.Sessions
	SFU      *sfu.SFU
	Origins  []string

	mu      sync.RWMutex
	clients map[*Client]struct{}
	online  map[uuid.UUID]int
	// chosen is the presence each connected user picked (online, away, dnd, focus).
	chosen map[uuid.UUID]string
	// voice maps channel → user → user info for everyone currently in a voice room.
	voice map[uuid.UUID]map[uuid.UUID]auth.User
	// grace holds users whose connection dropped while in voice. They stay listed
	// in the room for VoiceGrace so a page reload rejoins without a leave/join
	// flicker for everyone else (like Discord).
	grace map[uuid.UUID]*graceLeave
	// voiceSince is when each user in a voice room joined it; roomSince is
	// when each room got its first member. Both survive the grace period.
	voiceSince map[uuid.UUID]time.Time
	roomSince  map[uuid.UUID]time.Time

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

func NewHub(p *db.Pool, sessions *auth.Sessions, voiceSFU *sfu.SFU, origins []string) *Hub {
	h := &Hub{
		DB:       p,
		Sessions: sessions,
		SFU:      voiceSFU,
		Origins:  origins,
		clients:  map[*Client]struct{}{},
		online:   map[uuid.UUID]int{},
		chosen:   map[uuid.UUID]string{},
		voice:    map[uuid.UUID]map[uuid.UUID]auth.User{},
		grace:    map[uuid.UUID]*graceLeave{},

		voiceSince: map[uuid.UUID]time.Time{},
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

	// idle is set by the client after a while without input; it turns an
	// "online" user into "away" while all of their connections are idle.
	idle bool // guarded by hub.mu

	mu      sync.Mutex
	voiceCh *uuid.UUID
	// sfuPeer is this connection's own media peer. Another connection of the
	// same user (reconnect, second tab) gets its own, and tearing this one
	// down never touches the other.
	sfuPeer *sfu.Peer

	// Typing notices are relayed at most once per typingThrottle per channel.
	lastTypingCh uuid.UUID
	lastTypingAt time.Time
}

// typingThrottle is how often one connection's typing notice is relayed;
// clients show "… schreibt" for a few seconds after the last notice.
const typingThrottle = 3 * time.Second

// allowTyping reports whether a typing notice for chID may be relayed now.
func (c *Client) allowTyping(chID uuid.UUID, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if chID == c.lastTypingCh && now.Sub(c.lastTypingAt) < typingThrottle {
		return false
	}
	c.lastTypingCh, c.lastTypingAt = chID, now
	return true
}

func (c *Client) currentVoice() *uuid.UUID {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.voiceCh
}

func (c *Client) setVoice(ch *uuid.UUID) {
	c.mu.Lock()
	c.voiceCh = ch
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

// close tears the connection down once; readPump then unregisters the client.
func (c *Client) close() {
	c.closeOnce.Do(func() { _ = c.conn.Close() })
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
	user, err := h.Sessions.Authenticate(r)
	if err != nil {
		if _, ok := httpx.AsAPIError(err); !ok {
			err = httpx.ErrServer(err)
		}
		httpx.WriteError(w, err)
		return
	}
	tv, err := h.tokenVersion(r.Context(), user.ID)
	if err != nil {
		httpx.WriteError(w, httpx.ErrServer(err))
		return
	}
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		// The upgrader already answered (e.g. 403 for a foreign origin).
		slog.Warn("websocket upgrade failed", "err", err)
		return
	}
	c := &Client{hub: h, conn: conn, send: make(chan []byte, sendBuffer), User: *user, tokenVersion: tv}
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
	if ch := c.currentVoice(); ch != nil {
		// The media connection is gone, but presence lingers for the grace
		// period so a quick reconnect resumes the call seamlessly.
		if h.SFU != nil {
			h.SFU.RemovePeer(*ch, c.peer())
		}
		c.setVoice(nil)
		// A reconnect may already be back in the room before the old socket
		// unregisters; then the user never left and no grace timer runs.
		if !h.userInVoice(c.User.ID, *ch, c) {
			h.startGrace(c.User.ID, *ch)
		}
	}
	h.mu.Lock()
	if _, ok := h.clients[c]; !ok {
		h.mu.Unlock()
		return
	}
	before := h.statusLocked(c.User.ID)
	delete(h.clients, c)
	close(c.send)
	h.online[c.User.ID]--
	offline := h.online[c.User.ID] <= 0
	if offline {
		delete(h.online, c.User.ID)
		delete(h.chosen, c.User.ID)
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
func (h *Hub) OnlineUserIDs() []uuid.UUID { return h.onlineUsers() }

func (h *Hub) onlineUsers() []uuid.UUID {
	h.mu.RLock()
	defer h.mu.RUnlock()
	ids := make([]uuid.UUID, 0, len(h.online))
	for id := range h.online {
		ids = append(ids, id)
	}
	return ids
}

// VoiceUser is a member of a voice room and since when they are in it.
type VoiceUser struct {
	auth.User
	JoinedAt time.Time `json:"joined_at"`
}

// voiceSnapshot copies the current voice rooms (channel → users).
func (h *Hub) voiceSnapshot() map[uuid.UUID]map[uuid.UUID]VoiceUser {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make(map[uuid.UUID]map[uuid.UUID]VoiceUser, len(h.voice))
	for chID, users := range h.voice {
		cp := make(map[uuid.UUID]VoiceUser, len(users))
		for id, u := range users {
			cp[id] = VoiceUser{User: u.Public(), JoinedAt: h.voiceSince[id]}
		}
		out[chID] = cp
	}
	return out
}

// voiceRooms says since when each voice room is occupied, plus the server's
// clock so clients can count up without trusting their own.
func (h *Hub) voiceRooms() map[string]any {
	h.mu.RLock()
	defer h.mu.RUnlock()
	started := make(map[uuid.UUID]time.Time, len(h.roomSince))
	for id, t := range h.roomSince {
		started[id] = t
	}
	return map[string]any{"started": started, "now": time.Now()}
}

// voiceChannel returns chID if it exists and is a voice channel.
func (h *Hub) voiceChannel(ctx context.Context, chID uuid.UUID) (*chat.ChannelInfo, bool) {
	ch, err := chat.LoadChannel(ctx, h.DB, chID)
	if err != nil || ch.Type != chat.ChannelTypeVoice {
		return nil, false
	}
	return ch, true
}

// joinVoice puts c into ch's voice room. Re-joining the same room keeps the
// presence entry but renegotiates the SFU peer, which is how a client attaches
// its microphone once getUserMedia resolves.
func (h *Hub) joinVoice(c *Client, ch *chat.ChannelInfo) {
	// A user returning within the grace period resumes silently in the same
	// room; joining a different room ends the lingering presence first.
	rejoin := false
	if pending := h.takeGrace(c.User.ID); pending != nil {
		if pending.channelID == ch.ID {
			rejoin = true
		} else {
			h.removePresence(c.User.ID, pending.channelID)
		}
	}
	if cur := c.currentVoice(); cur != nil {
		if *cur == ch.ID {
			rejoin = true
		} else {
			h.leaveVoice(c, *cur)
		}
	}
	h.mu.Lock()
	now := time.Now()
	if h.voice[ch.ID] == nil {
		h.voice[ch.ID] = map[uuid.UUID]auth.User{}
	}
	if _, ok := h.roomSince[ch.ID]; !ok {
		h.roomSince[ch.ID] = now
	}
	// Already present through another connection (reconnect, second tab).
	if _, present := h.voice[ch.ID][c.User.ID]; present {
		rejoin = true
	}
	if _, ok := h.voiceSince[c.User.ID]; !ok || !rejoin {
		h.voiceSince[c.User.ID] = now
	}
	h.voice[ch.ID][c.User.ID] = c.User
	joined := VoiceUser{User: c.User.Public(), JoinedAt: h.voiceSince[c.User.ID]}
	roomStarted := h.roomSince[ch.ID]
	h.mu.Unlock()
	id := ch.ID
	c.setVoice(&id)

	if h.SFU != nil {
		_, peer, err := h.SFU.Join(ch.ID, c.User.ID,
			func(offer webrtc.SessionDescription) { c.SendEvent("webrtc_offer", offer) },
			func(cand *webrtc.ICECandidateInit) { c.SendEvent("webrtc_candidate", cand) })
		if err != nil {
			slog.Error("sfu join failed", "user", c.User.ID, "channel", ch.ID, "err", err)
		}
		c.setPeer(peer)
		// A late joiner learns who shares a screen or runs a camera.
		if room := h.SFU.Room(ch.ID); room != nil {
			for uid, st := range room.MediaStates() {
				if uid != c.User.ID {
					c.SendEvent("webrtc_media_state", mediaStatePayload(ch.ID, uid, st))
				}
			}
		}
	}
	slog.Info("voice join", "user", c.User.Username, "channel", ch.ID, "rejoin", rejoin)
	if !rejoin {
		h.Broadcast("voice_state_update", map[string]any{"action": "join", "channel_id": ch.ID, "user": joined, "started_at": roomStarted})
	}
}

// KickFromVoice ends userID's voice presence on every connection and in a
// pending grace period. It reports whether the user was in a voice room.
func (h *Hub) KickFromVoice(userID uuid.UUID) bool {
	h.mu.RLock()
	var mine []*Client
	for c := range h.clients {
		if c.User.ID == userID {
			mine = append(mine, c)
		}
	}
	h.mu.RUnlock()

	kicked := false
	for _, c := range mine {
		if cur := c.currentVoice(); cur != nil {
			ch := *cur
			h.leaveVoice(c, ch)
			c.SendEvent("voice_kicked", map[string]any{"channel_id": ch})
			kicked = true
		}
	}
	if g := h.takeGrace(userID); g != nil {
		h.removePresence(userID, g.channelID)
		kicked = true
	}
	return kicked
}

// DisconnectUser closes every live connection of userID.
func (h *Hub) DisconnectUser(userID uuid.UUID) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		if c.User.ID == userID {
			c.close()
		}
	}
}

// userInVoice reports whether another connection than except of userID is in chID's voice room.
func (h *Hub) userInVoice(userID, chID uuid.UUID, except *Client) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for other := range h.clients {
		if other == except || other.User.ID != userID {
			continue
		}
		if cur := other.currentVoice(); cur != nil && *cur == chID {
			return true
		}
	}
	return false
}

// leaveVoice is an explicit leave: presence ends immediately.
func (h *Hub) leaveVoice(c *Client, chID uuid.UUID) {
	if h.SFU != nil {
		h.SFU.RemovePeer(chID, c.peer())
	}
	h.takeGrace(c.User.ID)
	c.setVoice(nil)
	h.removePresence(c.User.ID, chID)
}

// removePresence drops userID from chID's room and announces it if they were there.
func (h *Hub) removePresence(userID, chID uuid.UUID) {
	h.mu.Lock()
	_, wasIn := h.voice[chID][userID]
	since, hadSince := h.voiceSince[userID]
	if wasIn {
		delete(h.voiceSince, userID)
	}
	if users, ok := h.voice[chID]; ok {
		delete(users, userID)
		if len(users) == 0 {
			delete(h.voice, chID)
			delete(h.roomSince, chID)
		}
	}
	h.mu.Unlock()
	if wasIn {
		slog.Info("voice leave", "user", userID, "channel", chID)
		h.Broadcast("voice_state_update", map[string]any{"action": "leave", "channel_id": chID, "user_id": userID})
		if hadSince {
			h.addVoiceTime(userID, time.Since(since))
		}
	}
}

// addVoiceTime adds one finished stay in a voice room to the user's total and
// tells everyone the new total.
func (h *Hub) addVoiceTime(userID uuid.UUID, d time.Duration) {
	secs := int64(d / time.Second)
	if h.DB == nil || secs <= 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var total int64
	err := h.DB.QueryRow(ctx, `UPDATE users SET voice_seconds = voice_seconds + $1 WHERE id = $2 RETURNING voice_seconds`,
		secs, userID).Scan(&total)
	if err != nil {
		slog.Warn("record voice time", "user", userID, "err", err)
		return
	}
	h.Broadcast("user_stats", map[string]any{"user_id": userID, "voice_seconds": total})
}

// startGrace keeps userID listed in chID for VoiceGrace, then removes them
// unless takeGrace claimed the slot first.
func (h *Hub) startGrace(userID, chID uuid.UUID) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if old := h.grace[userID]; old != nil {
		old.timer.Stop()
	}
	g := &graceLeave{channelID: chID}
	g.timer = time.AfterFunc(h.VoiceGrace, func() {
		h.mu.Lock()
		current := h.grace[userID] == g
		if current {
			delete(h.grace, userID)
		}
		h.mu.Unlock()
		if current {
			h.removePresence(userID, chID)
		}
	})
	h.grace[userID] = g
}

// takeGrace cancels and returns a pending grace leave for userID, if any.
func (h *Hub) takeGrace(userID uuid.UUID) *graceLeave {
	h.mu.Lock()
	defer h.mu.Unlock()
	g := h.grace[userID]
	if g != nil {
		g.timer.Stop()
		delete(h.grace, userID)
	}
	return g
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
		if cur := c.currentVoice(); cur != nil {
			h.leaveVoice(c, *cur)
		}

	case "typing":
		if !c.allowTyping(p.ChannelID, time.Now()) {
			return
		}
		if ch, err := chat.LoadChannel(ctx, h.DB, p.ChannelID); err == nil && ch.Type == chat.ChannelTypeText {
			h.broadcastExcept(c.User.ID, "typing", map[string]any{"channel_id": ch.ID, "user_id": c.User.ID})
		}

	case "voice_speaking":
		if cur := c.currentVoice(); cur != nil {
			h.Broadcast("voice_speaking", map[string]any{"channel_id": *cur, "user_id": c.User.ID, "active": p.Active})
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
		if len(payload) <= 4096 {
			slog.Info("webrtc client diag", "user", c.User.Username, "diag", json.RawMessage(payload))
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
	var req struct {
		Kind   string `json:"kind"`
		UserID string `json:"user_id"`
		All    bool   `json:"all"`
		On     bool   `json:"on"`
	}
	if json.Unmarshal(payload, &req) != nil {
		return
	}
	kind := sfu.Source(req.Kind)
	if kind != sfu.SourceScreen && kind != sfu.SourceCamera {
		return
	}
	if req.All && kind != sfu.SourceCamera {
		return
	}
	var publisher uuid.UUID
	if !req.All {
		id, err := uuid.Parse(req.UserID)
		if err != nil || id == uuid.Nil || id == c.User.ID {
			return
		}
		publisher = id
	}
	cur := c.currentVoice()
	if cur == nil || c.hub.SFU == nil {
		return
	}
	room := c.hub.SFU.Room(*cur)
	if room == nil {
		return
	}
	if err := room.Subscribe(c.User.ID, publisher, kind, req.All, req.On); err != nil {
		slog.Debug("sfu subscribe rejected", "user", c.User.ID, "err", err)
	}
}

func (c *Client) peer() *sfu.Peer {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sfuPeer
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
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
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
