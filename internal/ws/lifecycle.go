package ws

import (
	"context"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/metrics"
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
// @Description Every frame in both directions is a JSON text message `{"type": string, "payload": any}`. At most a few events per second per connection (and per account across its connections) are processed; excess client events, and events whose payload is over the cap for their type (1 KiB, more only for an SDP answer, ICE candidate, diagnostic and media handshake), are dropped.
// @Description
// @Description Server to client, on connect (to this connection only):
// @Description - `presence_snapshot`: map of user id to live status for every online user.
// @Description - `voice_snapshot`: map of voice channel id to a map of user id to User plus `joined_at`.
// @Description - `voice_rooms`: `{started: {channelId: time}, now}`; the server clock lets clients count up.
// @Description
// @Description Server to client, broadcast to all connections:
// @Description Channel events (`message_*`, `typing`, `voice_state_update`, `webrtc_media_state`) only reach connections whose user may view that channel; the connect snapshots list only the Talks the user may view.
// @Description - `message_create`, `message_update`: Message.
// @Description - `message_delete`: `{id, channel_id, parent_id}`.
// @Description - `message_reaction`: `{message_id, channel_id, reactions}`.
// @Description - `channels_changed`: no payload; refetch GET /api/channels (sent after any category/channel/layout admin change).
// @Description - `permissions_changed`: no payload; refetch GET /api/permissions and GET /api/channels. Sent to the users whose roles or permissions may have changed (everybody after a channel, category, overwrite or @everyone change).
// @Description - `roles_changed`: no payload; refetch GET /api/roles and GET /api/members (a role was created, edited, deleted, reordered or assigned).
// @Description - `emoji_changed`: no payload; refetch GET /api/emoji (a custom emoji was added, renamed or deleted).
// @Description - `member_joined`: public User of a newly registered member.
// @Description - `user_update`: public User after a profile, status or avatar change, or `{id, disabled}` when an admin disables or enables an account.
// @Description - `user_stats`: `{user_id, voice_seconds}` after a voice stay ends.
// @Description - `presence_update`: `{user_id, status}` where status is online, away, dnd, focus or offline; for a bot that set an activity also `activity: {type, name}` (Discord activity type).
// @Description - `voice_state_update`: `{action: "join", channel_id, user, started_at}` or `{action: "leave", channel_id, user_id}`.
// @Description - `typing`: `{channel_id, user_id}` (to everyone except the typist).
// @Description
// @Description Server to client, targeted:
// @Description - `block_updated`: to the user's own sessions only; `{user_id, blocked}` after they blocked or unblocked a member (refetch GET /api/me/blocks). The blocked member is never told.
// @Description - `dm_created`: to every member's sessions; the direct message (`DirectMessage`) a conversation was started with, or the one the receiver was just added to.
// @Description - `dm_member_added`: to the other members of a group conversation; `{dm_id, member}`.
// @Description - `dm_member_removed`: to the remaining members and the leaver's sessions; `{dm_id, member}`.
// @Description - `dm_prefs_updated`: to the user's own sessions only; `{dm_id}` after they closed, reopened or (un)favourited a conversation (refetch GET /api/dms/{id}), or `{dm_id: null}` after their direct message settings changed (refetch GET /api/dms).
// @Description - `dm_call_ring`: to the members' sessions that asked for conversations (`?dm=1`), the caller's own included; `{conversation_id, talk_id, message_id, caller_id, talk_name, ring_seconds}` after a Talk was started from, or shared into, a conversation. Everyone but the caller has an incoming call for `ring_seconds` (counted from receipt), unless a `dm_call_end` comes first. Additive: a client that ignores it still gets the invitation card as `message_create`.
// @Description - `dm_call_end`: to the same sessions; `{conversation_id, talk_id, reason, active, user_id?, message_id?}` when the call stopped ringing. `reason` is `answered` (a member other than the caller entered the Talk; `user_id` says who, so their other devices and the other members drop the incoming call), `cancelled` (the caller left before anybody joined, or started another Talk in the conversation), `timeout` (nobody joined in time) or `ended` (the last member left). `active` is true while somebody is still in the Talk. After `cancelled` and `timeout` the server has posted a "missed call" message (an embed with `call: "missed"`, written by the caller; `message_id`), announced by `message_create` just before.
// @Description - `talk_leader`: `{channel_id, leader_id}` to everyone who may view a temporary Talk (opened from a Create-a-Talk lobby) when its leader changes; `leader_id` is null when nobody leads it.
// @Description - `talk_moved`: `{from_channel_id, channel_id}` to the connection whose `voice_join` of a Create-a-Talk lobby opened the temporary Talk `channel_id`; the client then sends its ordinary `voice_join` for that Talk. The connection is never seated in the lobby itself.
// @Description - `read_state`: to the user's own sessions only; `{channel_id, last_read_at, unread_count, mention_count}`, `{channel_id, last_read_at, refresh: true}`, or `{refresh: true}` alone after a notification setting changed or everything was marked read (reload read states and notification settings).
// @Description - `webrtc_media_state`: `{channel_id, user_id, screen, camera}` to voice-room members.
// @Description - `voice_speaking`: `{channel_id, user_id, active}` to voice-room members, only when the state changes.
// @Description - `screen_viewers`: `{channel_id, user_id, viewers}` to voice-room members: `viewers` are the distinct users in the room watching `user_id`'s screen share (sorted ids, never the sharer). Sent whenever that set changes (`viewers: []` once when a watched share ends or loses its last viewer), and on `voice_join` one per live share to the joining connection only.
// @Description - `webrtc_offer` (SDP offer), `webrtc_candidate` (ICE candidate): SFU signalling.
// @Description - `screen_layers`: `{channel_id, user_id, layers}` to voice-room members: the layers of `user_id`'s live screen share, best first, each `{width, height, fps}` as the sharer declared them. Sent when they change, as an empty list once the share ends, and on `voice_join` one per live share to the joining connection only. A share of a client without layers has none.
// @Description - `screen_quality`: `{channel_id, user_id, layer, auto}` to the watching connection only: the layer of `user_id`'s screen share it receives now (`-1` before the first picture) and whether the server chooses it (`auto`).
// @Description - `cameras_capped`: `{channel_id, user_ids}` to the viewing connection only: whose cameras the server does not forward because the viewer asked for more than 24 at once (the lowest user ids are forwarded). Sent when that set changes, as an empty list once nothing is capped; the client shows those tiles without a picture.
// @Description - `screen_layers_wanted`: `{channel_id, layers, source?}` to the sharing connection only: the indexes of its layers some viewer receives or is about to (`source: "camera"` for the layers of its camera, absent for its screen share). A sender may stop encoding the others; layer 0 is always listed while nobody watches.
// @Description - `media_handshake`: `{channel_id, user_id, wire}` to the other connections in that voice room. `wire` is opaque: the server checks only its framing (an MLS key package, welcome or public message, or a heartbeat or capability message) and never reads or logs the content.
// @Description - `voice_kicked`: `{channel_id, reason?}` when a moderator removes the user from voice, `reason: "room_full"` when a voice join is refused because the room is full (the channel's `user_limit`; members with Disconnect members in that Talk, Administrators and the Owner may exceed it), `reason: "rules_not_accepted"` when the community's rules have not been accepted yet (POST /api/onboarding/accept), `reason: "talk_rate_limited"` or `"talk_unavailable"` when joining a Create-a-Talk lobby opened no Talk (5 new Talks per 10 minutes and member, 50 open Talks per lobby), or `reason: "connection_lost"` when the media connection of the call failed and the server dropped it (join again), `reason: "media_unavailable"` when the media connection of a join could not be set up (the seat is given up; join again), `reason: "joined_elsewhere"` to a connection whose Talk the same account joined on another connection (one connection per account and Talk holds the media), `reason: "voice_taken_over"` to a connection whose voice (in any Talk) the same account moved to another connection with `voice_join` and `takeover: true`; only its voice ends, the WebSocket stays open, or `reason: "missing_permission"` when the user may not (or no longer may) view and connect to that Talk.
// @Description - `pong`: `{t}` echoing a `ping`.
// @Description
// @Description Client to server:
// @Description - `ping` `{t}`; `presence_idle` `{idle: bool}`.
// @Description - `typing` `{channel_id}` (text and voice channels, rate-limited; ignored without View channels and Send messages).
// @Description - `voice_join` `{channel_id, takeover?, resume?, tab?}` (needs View channels and Connect, otherwise `voice_kicked` with `reason: "missing_permission"`). Without `takeover`, the account's other connection in the same Talk is told `joined_elsewhere` and a connection in another Talk keeps its seat. With `takeover: true` (the client sends it after the user confirmed "take over here") the join is completed first and only then does every other connection of the account leave voice, each told `voice_taken_over`; of two takeovers at once the later seat wins. A client that reconnected sends `resume: true` to continue its call, and `tab`, an id its page keeps across reconnects (up to 64 characters): if a takeover ended that page's call and another connection of the account is in voice, it is answered with `voice_kicked` `voice_taken_over` instead of taking the voice back; `voice_leave`; `voice_speaking` `{active}`. The SFU drops audio without Speak and camera/screen without Video.
// @Description - `webrtc_answer` (SDP answer), `webrtc_candidate` (ICE candidate), `webrtc_request_keyframe` (optionally `{source: "screen"|"camera"}`: a keyframe of the sender's own video of that source, e.g. after a camera change, so its viewers get a fresh picture; without it, of the videos the sender watches).
// @Description - `media_handshake` `{channel_id, wire}`: opaque handshake bytes for the current voice room.
// @Description - `webrtc_subscribe` `{kind: "screen"|"camera", user_id, on}`, `{kind: "camera", all: true, on}` or `{kind: "camera", user_ids: [up to 16 ids], on}` (several cameras with one renegotiation; a client that shows only some cameras asks for just those). A screen subscription may carry `quality`: `"auto"` or the index of a layer (0 is the best); it is applied before the first picture and again whenever sent. Without one, or with `"auto"`, the server chooses: it starts on the best layer, moves down one layer at a time when the viewer's receiver reports show lasting loss, a rising round trip time or repeated keyframe requests, and tries one layer up again after a long stretch without trouble (a failed attempt makes the next one wait twice as long). A camera subscription may carry `fit_px` (0 to 16384): how tall in device pixels the viewer's picture of that camera is; the server then gives it the smallest layer of a camera sent as layers that still has the pixels, and the message changes nothing else (it is sent only to a server whose `server_info` has `camera_layers`).
// @Description - `webrtc_layers` `{layers: [{width, height, fps}, ...]}` (1 to 3, best first, strictly shrinking): the layers of the sender's own screen share, or of its camera with `source: "camera"` (declared before its first video; a camera without layers is one plain track). They are sent on the SFU's lower video lines.
// @Description - `webrtc_screenshare_stop`, `webrtc_camera_stop`.
// @Description
// @Description Unknown event types are ignored. The server closes the connection when the session is revoked (sign-out of that session, sign-out everywhere, password change) or expires, or the account is disabled. A user keeps at most 8 browser connections: a newer one closes the oldest with close code 4001 ("too many connections"), after which that client should not reconnect by itself. Upgrades are rate-limited per address and per user (429), and `voice_join` beyond about one per second (burst 3) is answered with `voice_kicked`.
// @ID connectWebSocket
// @Tags Realtime
// @Produce json
// @Security cookieAuth
// @Param Upgrade header string true "Must be websocket." Enums(websocket)
// @Success 101 "Switching Protocols; the connection is now a WebSocket."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: cross-origin request rejected by the CSRF check."
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
	user, tv, ok := h.authenticated(w, r)
	if !ok {
		return
	}
	if h.isClosed() {
		httpx.WriteError(w, httpx.ErrUnavailable("server shutting down"))
		return
	}
	if !websocket.IsWebSocketUpgrade(r) {
		h.noteUpgrade(false)
	}
	if !h.upgradeAllowed(w, user.ID) {
		return
	}
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		// The upgrader already answered (e.g. 403 for a foreign origin).
		slog.Warn("websocket upgrade failed", "err", err)
		return
	}
	h.noteUpgrade(true)
	c := newClient(h, conn, *user, tv)
	c.dmAware = r.URL.Query().Get("dm") == "1"
	h.bindSession(c, r)
	if !h.admitPending(c) {
		return
	}
	if !h.confirmHandshake(r, c, user.ID, tv) {
		h.cancelPending(c)
		return
	}
	metrics.WSConnections.Inc()
	go c.writePump()
	go c.readPump()
}

// registerPending admits a handshake. A browser user keeps at most
// maxBrowserSockets connections: the oldest ones beyond that are returned
// to be evicted by the caller (outside the lock).
func (h *Hub) registerPending(c *Client) (bool, []*Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return false, nil
	}
	var evicted []*Client
	evicted = h.browserOverflowLocked(c)
	h.pending[c] = struct{}{}
	return true, evicted
}

// browserOverflowLocked returns the oldest connections (open or pending) of
// c's user that must go for c to fit under maxBrowserSockets. Callers hold h.mu.
func (h *Hub) browserOverflowLocked(c *Client) []*Client {
	var mine []*Client
	for _, set := range []map[*Client]struct{}{h.clients, h.pending} {
		for existing := range set {
			if existing.User.ID == c.User.ID && !existing.closed.Load() {
				mine = append(mine, existing)
			}
		}
	}
	sort.Slice(mine, func(i, j int) bool { return mine[i].connectedAt.Before(mine[j].connectedAt) })
	var evicted []*Client
	for len(mine) >= maxBrowserSockets {
		evicted = append(evicted, mine[0])
		mine = mine[1:]
	}
	return evicted
}

func (h *Hub) cancelPending(c *Client) {
	h.mu.Lock()
	delete(h.pending, c)
	c.shutdown()
	h.mu.Unlock()
	c.close()
}

func (h *Hub) register(c *Client) bool {
	return h.registerVerified(c, nil)
}

// A fresh user promotes an existing pending handshake atomically. The nil
// case registers the in-memory clients used by internal hub callers.
func (h *Hub) registerVerified(c *Client, fresh *auth.User) bool {
	before, after, ok := h.admitVerified(c, fresh)
	if !ok {
		return false
	}
	h.greet(c, before, after)
	return true
}

// pingEvery and revalidatePeriod resolve the pacing overrides (a nil hub, as
// in writer unit tests, uses the defaults).
func (h *Hub) pingEvery() time.Duration {
	if h != nil && h.PingInterval > 0 {
		return h.PingInterval
	}
	return pingInterval
}

func (h *Hub) revalidatePeriod() time.Duration {
	if h != nil && h.RevalidateInterval > 0 {
		return h.RevalidateInterval
	}
	return revalidateEvery
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
	// Stays in progress are settled now: once closed, the connections'
	// own clean-up no longer reaches removePresence to bank them.
	now := time.Now()
	stays := make(map[voiceKey]time.Duration, len(h.voiceSince))
	for key, since := range h.voiceSince {
		stays[key] = now.Sub(since)
		delete(h.voiceSince, key)
	}
	clients := make([]*Client, 0, len(h.clients))
	online := make([]uuid.UUID, 0, len(h.online))
	for c := range h.clients {
		clients = append(clients, c)
	}
	for id := range h.online {
		online = append(online, id)
	}
	for c := range h.pending {
		delete(h.pending, c)
		clients = append(clients, c)
	}
	for key, grace := range h.grace {
		grace.timer.Stop()
		delete(h.grace, key)
	}
	h.mu.Unlock()
	// Everyone still connected goes offline now. Record it here, while the
	// database is still open: the connections' own clean-up runs after the
	// server has moved on to closing it.
	for key, d := range stays {
		h.addVoiceTime(key.user, key.ch, d)
	}
	h.flushAllVoiceTime()
	h.recordLastSeen(online...)
	for _, c := range clients {
		c.shutdown()
		c.close()
	}
}

func (h *Hub) recordLastSeen(ids ...uuid.UUID) {
	if h.DB == nil || len(ids) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := h.DB.Exec(ctx, `UPDATE users SET last_seen_at = NOW() WHERE id = ANY($1)`, ids); err != nil {
		slog.Warn("record last seen", "users", len(ids), "err", err)
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
	// Close has recorded last seen for everyone already.
	closing := h.closed
	delete(h.clients, c)
	c.shutdown()
	if !c.idle {
		if h.active[c.User.ID]--; h.active[c.User.ID] <= 0 {
			delete(h.active, c.User.ID)
		}
	}
	h.online[c.User.ID]--
	offline := h.online[c.User.ID] <= 0
	if offline {
		delete(h.online, c.User.ID)
		delete(h.chosen, c.User.ID)
		delete(h.profiles, c.User.ID)
		h.voiceGate.forget(c.User.ID)
		h.stageGate.forget(c.User.ID)
		h.reactionGate.forget(c.User.ID)
		h.userBudgets.forget(c.User.ID)
	}
	after := h.statusLocked(c.User.ID)
	h.mu.Unlock()

	slog.Info("ws disconnected", "user", c.User.Username)
	if offline && !closing {
		h.recordLastSeen(c.User.ID)
	}
	h.announcePresence(c.User.ID, before, after)
}

func (h *Hub) noteUpgrade(upgraded bool) {
	if h.UpgradeSeen != nil {
		h.UpgradeSeen(upgraded)
	}
}
