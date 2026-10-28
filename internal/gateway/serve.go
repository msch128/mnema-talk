package gateway

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/msch128/mnema-talk/internal/bot"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// Hello is the d of HELLO.
type Hello struct {
	// HeartbeatInterval is how often the bot must send HEARTBEAT, in
	// milliseconds. A bot that stays silent for 1.5 intervals is closed with
	// 4009.
	HeartbeatInterval int `json:"heartbeat_interval"`
}

// Identify is the d of IDENTIFY.
type Identify struct {
	// Token may be left out when the upgrade carried the Authorization header.
	Token string `json:"token"`
	// Intents is the sum of the intent bits the bot wants; it must be within
	// what an admin allowed the bot.
	Intents    *int64         `json:"intents"`
	Properties map[string]any `json:"properties"`
	// Presence is the status and activity to start with, as in PRESENCE_UPDATE.
	Presence *PresenceUpdate `json:"presence"`
}

// Resume is the d of RESUME.
type Resume struct {
	Token     string `json:"token"`
	SessionID string `json:"session_id"`
	// Seq is the last sequence number the bot processed.
	Seq int64 `json:"seq"`
}

// Ready is the d of the READY dispatch.
type Ready struct {
	V         int       `json:"v"`
	User      ReadyUser `json:"user"`
	SessionID string    `json:"session_id"`
	Intents   int64     `json:"intents"`
	ResumeURL string    `json:"resume_gateway_url"`
	Heartbeat int       `json:"heartbeat_interval"`
}

// ReadyUser is the bot as READY shows it.
type ReadyUser struct {
	ID        uuid.UUID `json:"id"`
	BotID     uuid.UUID `json:"bot_id"`
	Username  string    `json:"username"`
	Bot       bool      `json:"bot"`
	AvatarURL string    `json:"avatar_url,omitempty"`
}

// frameLimiter allows framesPerMinute frames in each minute.
type frameLimiter struct {
	start time.Time
	count int
}

func (l *frameLimiter) allow(now time.Time) bool {
	if now.Sub(l.start) > time.Minute {
		l.start, l.count = now, 0
	}
	l.count++
	return l.count <= framesPerMinute
}

func (g *Gateway) serve(w http.ResponseWriter, r *http.Request) {
	if ok, retry := g.ips.Allow(httpx.ClientIPKey(r)); !ok {
		httpx.WriteDiscordRateLimit(w, retry, true)
		return
	}
	ws, err := g.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return // the upgrader answered
	}
	c := newConnection(ws)
	go c.writeLoop()
	g.run(r, c)
	c.closeWith(websocket.CloseNormalClosure, "")
	<-c.done
}

// credentials resolves the token of IDENTIFY or RESUME (or the header's).
func (g *Gateway) credentials(c *connection, token string, header *bot.Credentials) *bot.Credentials {
	if token == "" {
		if header == nil {
			c.closeWith(CloseAuthFailed, "Authentication failed")
		}
		return header
	}
	creds, err := g.Auth.Lookup(context.Background(), token)
	if err != nil {
		slog.Debug("gateway token lookup", "err", err)
		c.closeWith(CloseAuthFailed, "Authentication failed")
		return nil
	}
	return creds
}

func validIntents(intents *int64, allowed int64) int {
	if intents == nil || *intents < 0 || *intents&^bot.AllIntents != 0 {
		return CloseInvalidIntents
	}
	if *intents&^allowed != 0 {
		return CloseDisallowedIntents
	}
	return 0
}

func (g *Gateway) sendReady(s *session, creds *bot.Credentials) error {
	ready, err := json.Marshal(Ready{
		V: Version, SessionID: s.id, Intents: s.intents, ResumeURL: g.wsURL(), Heartbeat: int(g.timings().Heartbeat / time.Millisecond),
		User: ReadyUser{ID: creds.User.ID, BotID: creds.Bot.BotID, Username: creds.User.DisplayName, Bot: true, AvatarURL: creds.User.AvatarURL},
	})
	if err != nil {
		return err
	}
	s.enqueue("READY", ready)
	return nil
}

func (g *Gateway) initSessionPresence(s *session, p *PresenceUpdate) {
	if p == nil {
		g.resetIdlePresence(s)
		return
	}
	validated, err := p.validate()
	if err != nil {
		g.resetIdlePresence(s)
		return
	}
	g.setPresence(s.userID, validated)
}

func (g *Gateway) sessionCredentials(c *connection, token string, header *bot.Credentials) *bot.Credentials {
	creds := g.credentials(c, token, header)
	if creds == nil {
		return nil
	}
	if ok, _ := g.idents.Allow(creds.Bot.BotID.String()); !ok {
		c.closeWith(CloseRateLimited, "Rate limited")
		return nil
	}
	return creds
}

func (g *Gateway) identify(c *connection, d json.RawMessage, header *bot.Credentials) *session {
	var req Identify
	if err := json.Unmarshal(d, &req); err != nil {
		slog.Debug("gateway decode", "err", err)
		c.closeWith(4002, "Decode error")
		return nil
	}
	creds := g.sessionCredentials(c, req.Token, header)
	if creds == nil {
		return nil
	}
	if code := validIntents(req.Intents, creds.Bot.Intents); code != 0 {
		reason := "Invalid intents"
		if code == CloseDisallowedIntents {
			reason = "Disallowed intents"
		}
		c.closeWith(code, reason)
		return nil
	}
	s := &session{id: newSessionID(), botID: creds.Bot.BotID, userID: creds.User.ID, tokenID: creds.Bot.TokenID, intents: *req.Intents, startedAt: time.Now(), conn: c}
	if !g.register(s) {
		c.closeWith(CloseRateLimited, "Too many connections")
		return nil
	}
	// The token may have been revoked between its lookup and the registration.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !g.stillGood(ctx, s) {
		return nil
	}
	if err := g.sendReady(s, creds); err != nil {
		slog.Error("gateway ready", "err", err)
		c.closeWith(CloseUnknownError, "Internal error")
		return nil
	}
	g.initSessionPresence(s, req.Presence)
	return s
}

func (g *Gateway) lookupSession(id string) *session {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.sessions[id]
}

func validResumeSession(s *session, botID uuid.UUID, seq int64) bool {
	return s != nil && s.botID == botID && seq >= 0
}

func closeReplaced(replaced, current *connection) {
	if replaced != nil && replaced != current {
		replaced.closeWith(CloseUnknownError, "Session resumed elsewhere")
	}
}

func (g *Gateway) adoptSession(s *session, tokenID uuid.UUID) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return g.adopt(ctx, s, tokenID)
}

// resume continues a session on c. It returns the session when it resumed,
// nil with fatal false when the bot must identify again (INVALID_SESSION), and
// fatal true when the connection was closed.
func (g *Gateway) resume(c *connection, d json.RawMessage, header *bot.Credentials) (s *session, fatal bool) {
	var req Resume
	if err := json.Unmarshal(d, &req); err != nil {
		slog.Debug("gateway decode", "err", err)
		c.closeWith(4002, "Decode error")
		return nil, true
	}
	creds := g.sessionCredentials(c, req.Token, header)
	if creds == nil {
		return nil, true
	}
	s = g.lookupSession(req.SessionID)
	if !validResumeSession(s, creds.Bot.BotID, req.Seq) {
		c.trySend(marshal(OpInvalidSession, false))
		return nil, false
	}
	replaced, ok := s.resumeOn(c, req.Seq)
	if !ok {
		c.trySend(marshal(OpInvalidSession, false))
		return nil, false
	}
	// The replaced connection ends whether or not the adoption holds.
	closeReplaced(replaced, c)
	if !g.adoptSession(s, creds.Bot.TokenID) {
		c.closeWith(CloseAuthFailed, "Authentication failed")
		return nil, true
	}
	return s, false
}

func scanSessions(mine map[string]*session) (live int, oldest *session) {
	var oldestAt time.Time
	for _, o := range mine {
		at, isLive := o.detachedSince()
		if isLive {
			live++
		} else if oldest == nil || at.Before(oldestAt) {
			oldest, oldestAt = o, at
		}
	}
	return live, oldest
}

// register adds a session unless the bot is at its limits. Detached sessions
// make room for new ones, oldest first.
func (g *Gateway) register(s *session) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return false
	}
	mine := g.byBot[s.botID]
	live, oldest := scanSessions(mine)
	if live >= maxConnsPerBot {
		return false
	}
	if len(mine) >= maxSessionsPerBot && oldest != nil {
		g.removeLocked(oldest.id, oldest)
	}
	if g.byBot[s.botID] == nil {
		g.byBot[s.botID] = map[string]*session{}
	}
	g.sessions[s.id] = s
	g.byBot[s.botID][s.id] = s
	return true
}
