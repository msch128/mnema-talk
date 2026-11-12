// Package ws is the real-time hub: one WebSocket per browser session carrying
// chat events, presence, voice state and WebRTC signaling for the SFU.
package ws

import (
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/sfu"
)

// Hub tracks connected clients, online presence and voice rooms. It implements
// events.Publisher.
type Hub struct {
	DB       *db.Pool
	Sessions Authenticator
	SFU      *sfu.SFU
	Origins  []string
	// Perms decides who may view, join and speak in which channel. NewHub
	// requires it.
	Perms *perm.Service
	// permRetry re-enforces permission changes whose state failed to load.
	permRetry permRetry
	// Tap, when set, sees every event the hub publishes (channel is uuid.Nil
	// for events to everybody), in the publisher's goroutine, before any
	// connection is handed the event, and never while h.mu is held (every
	// caller of tapped and dmTapped calls them before taking it;
	// TestTapsRunOutsideTheHubLock guards that). So a tap may block for a
	// bounded time (it holds up that one publication, nothing else) and may
	// publish itself: what it publishes reaches each connection before the
	// event it was shown (the thread follow sync relies on that). The bot
	// gateway, Web Push and the thread follow sync listen here.
	Tap func(channel uuid.UUID, ev events.Event)
	// DMTap is Tap for the events of direct message conversations, under the
	// same rules. Those never reach Tap (outgoing webhooks, the bot gateway):
	// only Web Push and the thread follow sync listen here.
	DMTap func(channel uuid.UUID, ev events.Event)
	// Presence, when set, hears every join and leave of a Talk (a quick
	// rejoin is neither). The audit log listens here. It must not block.
	Presence func(userID, channelID uuid.UUID, joined bool)
	// UpgradeSeen, when set, hears of every signed-in request for /api/ws:
	// true when it upgraded to a WebSocket, false when it arrived without the
	// upgrade headers, which is what a reverse proxy that does not pass
	// WebSockets produces. It must not block.
	UpgradeSeen func(upgraded bool)
	// Lobby, when set, opens the temporary Talk a join of a Create-a-Talk
	// lobby leads to. Without it a lobby is an ordinary Talk.
	Lobby LobbyOpener

	mu      sync.RWMutex
	closed  bool // guarded by mu; shutdown rejects further registrations
	clients map[*Client]struct{}
	// pending handshakes can be revoked, but receive no events or presence.
	pending map[*Client]struct{}
	online  map[uuid.UUID]int
	// botLive holds the bots with a live gateway connection; they are online
	// without a WebSocket client (see SetBotPresence).
	botLive map[uuid.UUID]bool
	// botState is the status and activity a connected bot set itself.
	botState map[uuid.UUID]botState
	// active counts each user's connections that are not idle, so a user's
	// status is O(1) to derive instead of a scan of every client.
	active map[uuid.UUID]int
	// chosen is the presence each connected user picked (online, away, dnd, focus).
	chosen map[uuid.UUID]string
	// profiles is the latest public profile of each connected user; it
	// follows user_update events so voice payloads never show a stale name
	// or avatar.
	profiles map[uuid.UUID]auth.User
	// voice maps channel → user → user info for everyone currently in a voice room.
	voice map[uuid.UUID]map[uuid.UUID]auth.User
	// rooms lists the registered connections of each voice room, so an event
	// of one room does not scan every connection. It follows Client.setVoice
	// and forget; a connection has no room before it is registered.
	//
	// Lock order: Client.voiceMu, then mu, then Client.mu. setVoice takes mu
	// itself, so its callers hold at most a voiceMu, never mu (not even
	// read-locked: a waiting writer blocks new readers) and never a Client.mu.
	// Nothing is called while mu is held that takes a voiceMu or a lock of
	// the SFU.
	rooms map[uuid.UUID]map[*Client]struct{}
	// grace holds users whose connection dropped while in a voice room. They
	// stay listed in it for VoiceGrace so a page reload rejoins without a
	// leave/join flicker for everyone else (like Discord).
	grace map[voiceKey]*graceLeave
	// voiceSince is when a user joined a voice room; roomSince is when each
	// room got its first member. Both survive the grace period.
	voiceSince map[voiceKey]time.Time
	// seatClasses says how a seat's time counts, decided when the seat is
	// taken: the Talk may be gone by the time the stay ends.
	seatClasses map[voiceKey]seatClass
	// voiceMute is the mute/deafen state of a user in a voice room.
	voiceMute map[voiceKey]MuteState
	roomSince map[uuid.UUID]time.Time

	// VoiceGrace is how long a dropped voice user stays in the room.
	VoiceGrace time.Duration
	// graceKeep is a seam for tests: when set, it replaces the check by which
	// the grace timer keeps a seat that a connection of the user holds again.
	graceKeep func(voiceKey) bool

	// PingInterval and RevalidateInterval pace the keep-alive ping and the
	// session re-check of every connection. Zero means the defaults
	// (pingInterval, revalidateEvery); tests shorten them.
	PingInterval       time.Duration
	RevalidateInterval time.Duration

	// Version is the server's release version, sent to every new connection
	// (server_info) so browsers can offer a reload after an update. Only the
	// version string: no commit or build details.
	Version string

	// UpgradeLimit, when set, limits WebSocket upgrades per user (the router
	// sets it); the per-address limit sits in front of the route.
	UpgradeLimit *httpx.RateLimiter

	// afterSeat, when set, runs in joinVoice right after the seat is taken
	// (tests only).
	afterSeat func()
	// afterPeer is the same right after the media peer is attached.
	afterPeer func()

	upgrader websocket.Upgrader

	// voiceGate limits voice join/leave churn per user; vtimes debounces the
	// writes of voice time.
	voiceGate voiceGate
	vtimes    voiceTimes
	// seatClock numbers live voice seats (Client.seat) of a hub without an
	// SFU, so of two connections of one user the later one is known.
	seatClock atomic.Uint64
	// taken marks stays a takeover ended (see resumeVoice).
	taken takeoverMarks
	// stage holds talk power and raised hands of Stage seats; stageGate and
	// reactionGate limit Stage commands and live reactions per member.
	stage        stageBook
	stageGate    windowGate
	reactionGate windowGate
	// userBudgets is the per-account flood guard across connections.
	userBudgets userBudgets
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

// NewHub returns a hub that asks perms who may view, join and speak in which
// channel, and re-checks live connections whenever permissions change (it
// registers itself as the service's notifier). It panics on a nil service.
func NewHub(p *db.Pool, sessions Authenticator, voiceSFU *sfu.SFU, origins []string, perms *perm.Service) *Hub {
	h := &Hub{
		DB:       p,
		Perms:    perm.Must(perms, "websocket hub"),
		Sessions: sessions,
		SFU:      voiceSFU,
		Origins:  origins,
		clients:  map[*Client]struct{}{},
		pending:  map[*Client]struct{}{},
		online:   map[uuid.UUID]int{},
		botLive:  map[uuid.UUID]bool{},
		botState: map[uuid.UUID]botState{},
		active:   map[uuid.UUID]int{},
		chosen:   map[uuid.UUID]string{},
		profiles: map[uuid.UUID]auth.User{},
		voice:    map[uuid.UUID]map[uuid.UUID]auth.User{},
		rooms:    map[uuid.UUID]map[*Client]struct{}{},
		grace:    map[voiceKey]*graceLeave{},

		voiceSince:  map[voiceKey]time.Time{},
		seatClasses: map[voiceKey]seatClass{},
		voiceMute:   map[voiceKey]MuteState{},
		roomSince:   map[uuid.UUID]time.Time{},

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
		voiceSFU.SetScreenViewersHandler(h.announceScreenViewers)
		voiceSFU.SetScreenLayersHandler(h.announceScreenLayers)
		voiceSFU.SetScreenQualityHandler(h.announceScreenQuality)
		voiceSFU.SetLayersWantedHandler(h.announceLayersWanted)
		voiceSFU.SetCamerasCappedHandler(h.announceCamerasCapped)
		voiceSFU.SetCameraLayersWantedHandler(h.announceCameraLayersWanted)
	}
	perms.SetNotifier(h)
	return h
}
