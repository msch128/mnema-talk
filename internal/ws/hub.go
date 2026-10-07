// Package ws is the real-time hub: one WebSocket per browser session carrying
// chat events, presence, voice state and WebRTC signaling for the SFU.
package ws

import (
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/sfu"
)

// Hub tracks connected clients, online presence and voice rooms. It implements
// events.Publisher.
type Hub struct {
	DB       *db.Pool
	Sessions Authenticator
	SFU      *sfu.SFU
	Origins  []string

	mu      sync.RWMutex
	closed  bool // guarded by mu; shutdown rejects further registrations
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

	// Version is the server's release version, sent to every new connection
	// (server_info) so browsers can offer a reload after an update. Only the
	// version string: no commit or build details.
	Version string

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
		voiceSFU.SetScreenViewersHandler(h.announceScreenViewers)
	}
	return h
}
