package gateway

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/msch128/mnema-talk/internal/bot"
)

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

	rd := &reader{g: g, c: c, header: header, tm: tm, deadline: time.Now().Add(tm.Identify)}
	defer func() {
		if rd.sess != nil {
			rd.sess.detach(c)
		}
	}()
	rd.loop()
}

// reader is the state of one connection's read loop: the session once the
// bot has identified or resumed, and the deadline for its next frame.
type reader struct {
	g      *Gateway
	c      *connection
	header *bot.Credentials
	tm     Timings
	sess   *session
	frames frameLimiter

	deadline time.Time
}

func (rd *reader) loop() {
	ws := rd.c.ws
	ws.SetReadLimit(maxFrameBytes)
	for {
		_ = ws.SetReadDeadline(rd.deadline)
		_, msg, err := ws.ReadMessage()
		if err != nil {
			rd.closeForReadError(err)
			return
		}
		if !rd.frames.allow(time.Now()) {
			rd.c.closeWith(CloseRateLimited, "Rate limited")
			return
		}
		var f frame
		if json.Unmarshal(msg, &f) != nil {
			rd.c.closeWith(4002, "Decode error")
			return
		}
		if !rd.dispatch(f) {
			return
		}
	}
}

// closeForReadError closes the connection with the code a failed read means.
func (rd *reader) closeForReadError(err error) {
	var ne net.Error
	switch {
	case errors.As(err, &ne) && ne.Timeout() && rd.sess == nil:
		rd.c.closeWith(CloseNotAuthenticated, "Not authenticated")
	case errors.As(err, &ne) && ne.Timeout():
		rd.c.closeWith(CloseSessionTimeout, "Session timed out")
	case errors.Is(err, websocket.ErrReadLimit):
		rd.c.closeWith(websocket.CloseMessageTooBig, "frame too large")
	}
}

// armHeartbeatDeadline gives the bot 1.5 heartbeat intervals for its next frame.
func (rd *reader) armHeartbeatDeadline() {
	rd.deadline = time.Now().Add(rd.tm.Heartbeat * 3 / 2)
}

// dispatch handles one frame; false ends the connection.
func (rd *reader) dispatch(f frame) bool {
	switch f.Op {
	case OpHeartbeat:
		rd.c.trySend(marshal(OpHeartbeatAck, nil))
		if rd.sess != nil {
			rd.armHeartbeatDeadline()
		}
		return true
	case OpIdentify:
		return rd.onIdentify(f)
	case OpPresenceUpdate:
		return rd.onPresenceUpdate(f)
	case OpResume:
		return rd.onResume(f)
	default:
		rd.c.closeWith(4001, "Unknown opcode")
		return false
	}
}

func (rd *reader) onIdentify(f frame) bool {
	if rd.sess != nil {
		rd.c.closeWith(CloseAlreadyIdentified, "Already authenticated")
		return false
	}
	if rd.sess = rd.g.identify(rd.c, f.D, rd.header); rd.sess == nil {
		return false
	}
	rd.armHeartbeatDeadline()
	return true
}

func (rd *reader) onPresenceUpdate(f frame) bool {
	if rd.sess == nil {
		rd.c.closeWith(CloseNotAuthenticated, "Not authenticated")
		return false
	}
	p, err := parsePresence(f.D)
	if err != nil {
		rd.c.closeWith(4002, "Decode error")
		return false
	}
	// Over the limit, an update is dropped (Discord does the same).
	if ok, wait := rd.g.presLimit.Allow(rd.sess.botID.String()); ok && wait == 0 {
		rd.g.setPresence(rd.sess.userID, p)
	}
	return true
}

func (rd *reader) onResume(f frame) bool {
	if rd.sess != nil {
		rd.c.closeWith(CloseAlreadyIdentified, "Already authenticated")
		return false
	}
	s, fatal := rd.g.resume(rd.c, f.D, rd.header)
	if fatal {
		return false
	}
	if s != nil {
		rd.sess = s
		rd.armHeartbeatDeadline()
	}
	return true
}
