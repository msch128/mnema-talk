package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net"
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

// run is the read loop of one connection.
func (g *Gateway) run(r *http.Request, c *connection) {
	tm := g.timings()
	c.trySend(marshal(OpHello, Hello{HeartbeatInterval: int(tm.Heartbeat / time.Millisecond)}))

	// A bot that can set headers authenticates the upgrade itself; others
	// send the token in IDENTIFY. Either way the token is checked before
	// anything is sent but HELLO.
	var header *bot.Credentials
	if tok, ok := bot.BearerToken(r); ok {
		creds, err := g.Auth.Lookup(r.Context(), tok)
		if err != nil {
			c.closeWith(CloseAuthFailed, "Authentication failed")
			return
		}
		header = creds
	}

	var sess *session
	defer func() {
		if sess != nil {
			sess.detach(c)
		}
	}()
	ws := c.ws
	ws.SetReadLimit(maxFrameBytes)
	deadline := time.Now().Add(tm.Identify)
	var frames frameLimiter
	for {
		_ = ws.SetReadDeadline(deadline)
		_, msg, err := ws.ReadMessage()
		if err != nil {
			var ne net.Error
			switch {
			case errors.As(err, &ne) && ne.Timeout() && sess == nil:
				c.closeWith(CloseNotAuthenticated, "Not authenticated")
			case errors.As(err, &ne) && ne.Timeout():
				c.closeWith(CloseSessionTimeout, "Session timed out")
			case errors.Is(err, websocket.ErrReadLimit):
				c.closeWith(websocket.CloseMessageTooBig, "frame too large")
			}
			return
		}
		if !frames.allow(time.Now()) {
			c.closeWith(CloseRateLimited, "Rate limited")
			return
		}
		var f frame
		if json.Unmarshal(msg, &f) != nil {
			c.closeWith(4002, "Decode error")
			return
		}
		switch f.Op {
		case OpHeartbeat:
			c.trySend(marshal(OpHeartbeatAck, nil))
			if sess != nil {
				deadline = time.Now().Add(tm.Heartbeat * 3 / 2)
			}
		case OpIdentify:
			if sess != nil {
				c.closeWith(CloseAlreadyIdentified, "Already authenticated")
				return
			}
			if sess = g.identify(c, f.D, header); sess == nil {
				return
			}
			deadline = time.Now().Add(tm.Heartbeat * 3 / 2)
		case OpResume:
			if sess != nil {
				c.closeWith(CloseAlreadyIdentified, "Already authenticated")
				return
			}
			s, fatal := g.resume(c, f.D, header)
			if fatal {
				return
			}
			if s != nil {
				sess = s
				deadline = time.Now().Add(tm.Heartbeat * 3 / 2)
			}
		default:
			c.closeWith(4001, "Unknown opcode")
			return
		}
	}
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
		c.closeWith(CloseAuthFailed, "Authentication failed")
		return nil
	}
	return creds
}

func (g *Gateway) identify(c *connection, d json.RawMessage, header *bot.Credentials) *session {
	var req Identify
	if json.Unmarshal(d, &req) != nil {
		c.closeWith(4002, "Decode error")
		return nil
	}
	creds := g.credentials(c, req.Token, header)
	if creds == nil {
		return nil
	}
	if ok, _ := g.idents.Allow(creds.Bot.BotID.String()); !ok {
		c.closeWith(CloseRateLimited, "Rate limited")
		return nil
	}
	if req.Intents == nil || *req.Intents < 0 || *req.Intents&^bot.AllIntents != 0 {
		c.closeWith(CloseInvalidIntents, "Invalid intents")
		return nil
	}
	if *req.Intents&^creds.Bot.Intents != 0 {
		c.closeWith(CloseDisallowedIntents, "Disallowed intents")
		return nil
	}
	s := &session{id: newSessionID(), botID: creds.Bot.BotID, userID: creds.User.ID, tokenID: creds.Bot.TokenID, intents: *req.Intents, startedAt: time.Now()}
	if !g.register(s) {
		c.closeWith(CloseRateLimited, "Too many connections")
		return nil
	}
	s.attach(c)
	// The token may have been revoked between its lookup and the registration.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !g.stillGood(ctx, s) {
		return nil
	}
	ready, _ := json.Marshal(Ready{
		V: Version, SessionID: s.id, Intents: s.intents, ResumeURL: g.wsURL(), Heartbeat: int(g.timings().Heartbeat / time.Millisecond),
		User: ReadyUser{ID: creds.User.ID, BotID: creds.Bot.BotID, Username: creds.User.DisplayName, Bot: true, AvatarURL: creds.User.AvatarURL},
	})
	s.enqueue("READY", ready)
	return s
}

// resume continues a session on c. It returns the session when it resumed,
// nil with fatal false when the bot must identify again (INVALID_SESSION), and
// fatal true when the connection was closed.
func (g *Gateway) resume(c *connection, d json.RawMessage, header *bot.Credentials) (s *session, fatal bool) {
	var req Resume
	if json.Unmarshal(d, &req) != nil {
		c.closeWith(4002, "Decode error")
		return nil, true
	}
	creds := g.credentials(c, req.Token, header)
	if creds == nil {
		return nil, true
	}
	if ok, _ := g.idents.Allow(creds.Bot.BotID.String()); !ok {
		c.closeWith(CloseRateLimited, "Rate limited")
		return nil, true
	}
	g.mu.Lock()
	s = g.sessions[req.SessionID]
	g.mu.Unlock()
	if s == nil || s.botID != creds.Bot.BotID || req.Seq < 0 {
		c.trySend(marshal(OpInvalidSession, false))
		return nil, false
	}
	replaced, ok := s.resumeOn(c, req.Seq)
	if !ok {
		c.trySend(marshal(OpInvalidSession, false))
		return nil, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !g.adopt(ctx, s, creds.Bot.TokenID) {
		c.closeWith(CloseAuthFailed, "Authentication failed")
		return nil, true
	}
	if replaced != nil && replaced != c {
		replaced.closeWith(CloseUnknownError, "Session resumed elsewhere")
	}
	return s, false
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
	live := 0
	var oldest *session
	for _, o := range mine {
		if o.live() {
			live++
			continue
		}
		if oldest == nil || o.detachedAt.Before(oldest.detachedAt) {
			oldest = o
		}
	}
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
