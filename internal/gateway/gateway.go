// Package gateway is the WebSocket gateway of the bot API, modelled on
// Discord's: HELLO, IDENTIFY with intents, READY, heartbeats, DISPATCH events
// with sequence numbers, and RESUME after a short disconnect. Which events a
// bot receives is decided by its allowed intents and, live for every event, by
// what its roles and overwrites let it view.
package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/bot"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Opcodes, as in Discord's gateway.
const (
	OpDispatch       = 0
	OpHeartbeat      = 1
	OpIdentify       = 2
	OpResume         = 6
	OpReconnect      = 7
	OpInvalidSession = 9
	OpHello          = 10
	OpHeartbeatAck   = 11
)

// Close codes, as in Discord's gateway where one fits.
const (
	CloseUnknownError      = 4000
	CloseNotAuthenticated  = 4003
	CloseAuthFailed        = 4004
	CloseAlreadyIdentified = 4005
	CloseInvalidSeq        = 4007
	CloseRateLimited       = 4008
	CloseSessionTimeout    = 4009
	CloseInvalidIntents    = 4013
	CloseDisallowedIntents = 4014
)

// Version is the gateway protocol version, in READY.
const Version = 1

const (
	defaultHeartbeat = 41250 * time.Millisecond
	// maxFrameBytes is the largest frame a bot may send (Discord: 4096).
	maxFrameBytes = 4096
	// framesPerMinute bounds what one connection may send (Discord: 120).
	framesPerMinute = 120
	// maxConnsPerBot bounds the live connections of one bot.
	maxConnsPerBot = 3
	// maxSessionsPerBot bounds live plus resumable sessions of one bot.
	maxSessionsPerBot = 6
	// bufferSize is how many events a session keeps for RESUME.
	bufferSize = 512
	sendQueue  = 256
	writeWait  = 10 * time.Second
	// identifyPerMinute bounds IDENTIFY and RESUME per bot.
	identifyPerMinute = 20
	// tokenCheckEvery is how often live sessions are checked against their
	// tokens (expiry, or a change made straight in the database).
	tokenCheckEvery = 30 * time.Second
)

// Gateway serves the bot gateway and fans events out to bot sessions.
type Gateway struct {
	Auth  *bot.Authenticator
	Perms *perm.Service
	DB    *db.Pool
	// PublicURL builds the address returned by GET /gateway.
	PublicURL string
	// OnPresence, when set before Run, is told when a bot gains its first or
	// loses its last live connection (the member list shows it online or
	// offline). A session that is only resumable does not count as live.
	OnPresence func(userID uuid.UUID, online bool)

	tmu sync.RWMutex
	tm  Timings

	upgrader websocket.Upgrader
	ips      *httpx.RateLimiter
	idents   *httpx.RateLimiter

	mu       sync.Mutex
	sessions map[string]*session
	byBot    map[uuid.UUID]map[string]*session
	closed   bool
	stop     chan struct{}
	// announced is the set of bots OnPresence last reported online.
	announced map[uuid.UUID]bool
}

// Config is what a Gateway needs.
type Config struct {
	Auth  *bot.Authenticator
	Perms *perm.Service
	DB    *db.Pool
	// PublicURL builds the address returned by GET /gateway.
	PublicURL string
}

// New returns a Gateway; Run starts its housekeeping.
func New(c Config) *Gateway {
	g := &Gateway{Auth: c.Auth, Perms: c.Perms, DB: c.DB, PublicURL: c.PublicURL}
	g.tm = Timings{Heartbeat: defaultHeartbeat, Identify: 10 * time.Second, Resume: 2 * time.Minute, TokenCheck: tokenCheckEvery}
	g.upgrader = websocket.Upgrader{ReadBufferSize: 4096, WriteBufferSize: 4096}
	g.ips = httpx.NewRateLimiter(30, time.Minute).Named("bot_gateway_ip")
	g.idents = httpx.NewRateLimiter(identifyPerMinute, time.Minute).Named("bot_gateway_identify")
	g.sessions = map[string]*session{}
	g.byBot = map[uuid.UUID]map[string]*session{}
	g.stop = make(chan struct{})
	return g
}

// Timings are the gateway's clocks; tests shorten them.
type Timings struct {
	// Heartbeat is the interval HELLO asks for; a silent bot is closed after
	// 1.5 intervals.
	Heartbeat time.Duration
	// Identify is how long a connection may take to IDENTIFY or RESUME.
	Identify time.Duration
	// Resume is how long a session stays resumable after its connection ended.
	Resume time.Duration
	// TokenCheck is how often live sessions are checked against their tokens.
	TokenCheck time.Duration
}

// SetTimings replaces the clocks for connections made afterwards.
func (g *Gateway) SetTimings(t Timings) {
	g.tmu.Lock()
	g.tm = t
	g.tmu.Unlock()
}

func (g *Gateway) timings() Timings {
	g.tmu.RLock()
	defer g.tmu.RUnlock()
	return g.tm
}

// Run does the housekeeping until ctx ends: it drops sessions nobody resumed
// in time and closes those whose token ended.
func (g *Gateway) Run(ctx context.Context) {
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	last := time.Now()
	for {
		select {
		case <-ctx.Done():
			g.Close()
			g.syncPresence()
			return
		case now := <-tick.C:
			g.expire()
			g.syncPresence()
			if now.Sub(last) >= g.timings().TokenCheck {
				last = now
				g.checkTokens(ctx)
			}
		}
	}
}

// syncPresence reports the bots whose online state changed since the last call.
func (g *Gateway) syncPresence() {
	if g.OnPresence == nil {
		return
	}
	g.mu.Lock()
	live := map[uuid.UUID]bool{}
	for _, s := range g.sessions {
		if s.live() {
			live[s.userID] = true
		}
	}
	var changes []uuid.UUID
	for id := range live {
		if !g.announced[id] {
			changes = append(changes, id)
		}
	}
	for id := range g.announced {
		if !live[id] {
			changes = append(changes, id)
		}
	}
	g.announced = live
	g.mu.Unlock()
	for _, id := range changes {
		g.OnPresence(id, live[id])
	}
}

// Close ends every connection.
func (g *Gateway) Close() {
	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		return
	}
	g.closed = true
	close(g.stop)
	all := make([]*session, 0, len(g.sessions))
	for _, s := range g.sessions {
		all = append(all, s)
	}
	g.sessions = map[string]*session{}
	g.byBot = map[uuid.UUID]map[string]*session{}
	g.mu.Unlock()
	for _, s := range all {
		s.closeConn(websocket.CloseGoingAway, "server shutting down")
	}
}

// Info is the answer of GET /api/bot/v1/gateway.
type Info struct {
	// URL is the WebSocket address of the gateway.
	URL string `json:"url"`
	// HeartbeatInterval is the interval the gateway will ask for, in
	// milliseconds (also in HELLO).
	HeartbeatInterval int `json:"heartbeat_interval"`
	// MaxConnections is how many gateway connections one bot may hold.
	MaxConnections int `json:"max_connections"`
}

// Handle serves /api/bot/v1/gateway: the WebSocket upgrade, or, for a plain
// request, the gateway's address (that one needs the token like every other
// bot route).
//
// @Summary Get the gateway address
// @Description Returns the WebSocket address of the gateway. The same path is the gateway itself when the request is a WebSocket upgrade; its protocol (HELLO, IDENTIFY, READY, heartbeats, DISPATCH, RESUME) is described in doc/bots.md because OpenAPI cannot express it. Authenticate the upgrade with `Authorization: Bot <token>` (never a query parameter) or with the token in IDENTIFY.
// @ID botGateway
// @Tags Bot API
// @Produce json
// @Security botAuth
// @Success 200 {object} Info "The gateway."
// @Failure 401 {object} httpx.DiscordError "Missing or invalid token (0)."
// @Failure 429 {object} httpx.DiscordRateLimit "Rate limited; see retry_after and Retry-After."
// @Router /api/bot/v1/gateway [get]
func (g *Gateway) Handle(w http.ResponseWriter, r *http.Request) {
	if websocket.IsWebSocketUpgrade(r) {
		g.serve(w, r)
		return
	}
	g.Auth.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, Info{URL: g.wsURL(), HeartbeatInterval: int(g.timings().Heartbeat / time.Millisecond), MaxConnections: maxConnsPerBot})
	})).ServeHTTP(w, r)
}

func (g *Gateway) wsURL() string {
	u := strings.TrimRight(g.PublicURL, "/")
	switch {
	case strings.HasPrefix(u, "https://"):
		u = "wss://" + strings.TrimPrefix(u, "https://")
	case strings.HasPrefix(u, "http://"):
		u = "ws://" + strings.TrimPrefix(u, "http://")
	}
	return u + bot.BasePath + "/gateway"
}

// frame is a gateway message. S and T are only set on dispatches.
type frame struct {
	Op int             `json:"op"`
	D  json.RawMessage `json:"d"`
	S  *int64          `json:"s"`
	T  *string         `json:"t"`
}

func marshal(op int, d any) []byte {
	raw, _ := json.Marshal(d) // plain data: cannot fail
	out, _ := json.Marshal(frame{Op: op, D: raw})
	return out
}

func newSessionID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Revoke ends the live connections and the resumable sessions of a bot: of one
// token when tokenID is set, of all its tokens otherwise. Open connections get
// close code 4004, as for a failed login.
func (g *Gateway) Revoke(botID uuid.UUID, tokenID *uuid.UUID) {
	g.mu.Lock()
	var victims []*session
	for id, s := range g.byBot[botID] {
		if tokenID == nil || s.tokenID == *tokenID {
			victims = append(victims, s)
			delete(g.sessions, id)
			delete(g.byBot[botID], id)
		}
	}
	if len(g.byBot[botID]) == 0 {
		delete(g.byBot, botID)
	}
	g.mu.Unlock()
	for _, s := range victims {
		s.closeConn(CloseAuthFailed, "Authentication failed")
	}
}

// expire drops detached sessions that were not resumed in time.
func (g *Gateway) expire() {
	now, window := time.Now(), g.timings().Resume
	g.mu.Lock()
	defer g.mu.Unlock()
	for id, s := range g.sessions {
		if s.expired(now, window) {
			g.removeLocked(id, s)
		}
	}
}

func (g *Gateway) removeLocked(id string, s *session) {
	delete(g.sessions, id)
	if m := g.byBot[s.botID]; m != nil {
		delete(m, id)
		if len(m) == 0 {
			delete(g.byBot, s.botID)
		}
	}
}

// validTokens returns which of the tokens are still good: not revoked, not
// expired and of a bot that is not disabled.
func (g *Gateway) validTokens(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	var live []uuid.UUID
	rows, err := g.DB.Query(ctx, `
		SELECT t.id FROM bot_tokens t JOIN bots b ON b.id = t.bot_id
		WHERE t.id = ANY($1) AND t.revoked_at IS NULL AND (t.expires_at IS NULL OR t.expires_at > NOW()) AND NOT b.disabled`, ids)
	if err == nil {
		live, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	}
	valid := make(map[uuid.UUID]bool, len(live))
	for _, id := range live {
		valid[id] = true
	}
	return valid, err
}

// checkTokens closes the sessions whose token was revoked or expired, or whose
// bot was disabled or deleted, however that happened.
func (g *Gateway) checkTokens(ctx context.Context) {
	g.mu.Lock()
	ids := make([]uuid.UUID, 0, len(g.sessions))
	seen := map[uuid.UUID]bool{}
	for _, s := range g.sessions {
		if !seen[s.tokenID] {
			seen[s.tokenID] = true
			ids = append(ids, s.tokenID)
		}
	}
	g.mu.Unlock()
	if len(ids) == 0 || g.DB == nil {
		return
	}
	valid, err := g.validTokens(ctx, ids)
	if err != nil {
		slog.Warn("gateway token check", "err", err)
		return
	}
	for _, id := range ids {
		if !valid[id] {
			g.revokeToken(id)
		}
	}
}

// adopt hands a session over to the token that resumes it, unless the session
// ended meanwhile (a Revoke between the lookup and now), and checks that the
// token is still good: its revocation may have happened before the swap, when
// Revoke could not find the session under this token yet. The session ends
// when either fails.
func (g *Gateway) adopt(ctx context.Context, s *session, tokenID uuid.UUID) bool {
	g.mu.Lock()
	if g.sessions[s.id] != s {
		g.mu.Unlock()
		return false
	}
	s.tokenID = tokenID
	g.mu.Unlock()
	return g.stillGood(ctx, s)
}

// stillGood verifies the session's token against the database and ends the
// session when it is not good (or cannot be checked).
func (g *Gateway) stillGood(ctx context.Context, s *session) bool {
	g.mu.Lock()
	tokenID := s.tokenID
	g.mu.Unlock()
	if g.DB != nil {
		valid, err := g.validTokens(ctx, []uuid.UUID{tokenID})
		if err == nil && valid[tokenID] {
			return true
		}
	}
	g.mu.Lock()
	if g.sessions[s.id] == s {
		g.removeLocked(s.id, s)
	}
	g.mu.Unlock()
	s.closeConn(CloseAuthFailed, "Authentication failed")
	return false
}

func (g *Gateway) revokeToken(tokenID uuid.UUID) {
	g.mu.Lock()
	var victims []*session
	for id, s := range g.sessions {
		if s.tokenID == tokenID {
			victims = append(victims, s)
			g.removeLocked(id, s)
		}
	}
	g.mu.Unlock()
	for _, s := range victims {
		s.closeConn(CloseAuthFailed, "Authentication failed")
	}
}
