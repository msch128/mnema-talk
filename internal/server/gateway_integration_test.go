//go:build integration

package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/msch128/mnema-talk/internal/bot"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/gateway"
	"github.com/msch128/mnema-talk/internal/perm"
)

type gwFrame struct {
	Op int             `json:"op"`
	D  json.RawMessage `json:"d"`
	S  *int64          `json:"s"`
	T  *string         `json:"t"`
}

type gwClient struct {
	t      *testing.T
	ws     *websocket.Conn
	frames chan gwFrame
	closed chan int // close code, 0 for an abnormal end
}

const (
	within   = 2 * time.Second
	quiet    = 400 * time.Millisecond
	allBotIn = bot.IntentGuilds | bot.IntentGuildMembers | bot.IntentGuildMessages | bot.IntentGuildMessageReactions
)

func (f *botFixture) dialGW(header string) *gwClient {
	f.t().Helper()
	return f.dialGWTo("/api/bot/v1/gateway", header)
}

func (f *botFixture) t() *testing.T { return f.client.t }

func (f *botFixture) dialGWTo(path, header string) *gwClient {
	t := f.t()
	hdr := http.Header{}
	if header != "" {
		hdr.Set("Authorization", header)
	}
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(f.a.srv.URL, "http")+path, hdr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	g := &gwClient{t: t, ws: ws, frames: make(chan gwFrame, 256), closed: make(chan int, 1)}
	go func() {
		for {
			_, raw, err := ws.ReadMessage()
			if err != nil {
				var ce *websocket.CloseError
				if errors.As(err, &ce) {
					g.closed <- ce.Code
				} else {
					g.closed <- 0
				}
				close(g.frames)
				return
			}
			var fr gwFrame
			if json.Unmarshal(raw, &fr) == nil {
				g.frames <- fr
			}
		}
	}()
	t.Cleanup(func() { ws.Close() })
	return g
}

func (g *gwClient) send(op int, d any) {
	g.t.Helper()
	raw, _ := json.Marshal(map[string]any{"op": op, "d": d})
	if err := g.ws.WriteMessage(websocket.TextMessage, raw); err != nil {
		g.t.Fatalf("send: %v", err)
	}
}

func (g *gwClient) next(d time.Duration) (gwFrame, bool) {
	select {
	case fr, ok := <-g.frames:
		return fr, ok
	case <-time.After(d):
		return gwFrame{}, false
	}
}

// dispatch waits for the next dispatch and returns its name, sequence and data.
func (g *gwClient) dispatch(d time.Duration) (string, int64, json.RawMessage, bool) {
	deadline := time.After(d)
	for {
		select {
		case fr, ok := <-g.frames:
			if !ok {
				return "", 0, nil, false
			}
			if fr.Op == gateway.OpDispatch && fr.T != nil && fr.S != nil {
				return *fr.T, *fr.S, fr.D, true
			}
		case <-deadline:
			return "", 0, nil, false
		}
	}
}

func (g *gwClient) expect(name string) (int64, json.RawMessage) {
	g.t.Helper()
	got, seq, d, ok := g.dispatch(within)
	if !ok || got != name {
		g.t.Fatalf("want %s, got %q (received: %v)", name, got, ok)
	}
	return seq, d
}

func (g *gwClient) nothing() {
	g.t.Helper()
	if name, _, d, ok := g.dispatch(quiet); ok {
		g.t.Fatalf("unexpected %s: %s", name, d)
	}
}

func (g *gwClient) waitClose(code int) {
	g.t.Helper()
	select {
	case got := <-g.closed:
		if got != code {
			g.t.Fatalf("closed with %d, want %d", got, code)
		}
	case <-time.After(within):
		g.t.Fatalf("not closed, want %d", code)
	}
}

func (g *gwClient) hello() gateway.Hello {
	g.t.Helper()
	fr, ok := g.next(within)
	if !ok || fr.Op != gateway.OpHello {
		g.t.Fatalf("no HELLO: %+v", fr)
	}
	var h gateway.Hello
	_ = json.Unmarshal(fr.D, &h)
	return h
}

// identify completes the handshake and returns READY.
func (g *gwClient) identify(token string, intents int64) gateway.Ready {
	g.t.Helper()
	g.hello()
	d := map[string]any{"intents": intents, "properties": map[string]any{"os": "test"}}
	if token != "" {
		d["token"] = token
	}
	g.send(gateway.OpIdentify, d)
	seq, raw := g.expect("READY")
	var r gateway.Ready
	_ = json.Unmarshal(raw, &r)
	if seq != 1 || r.SessionID == "" || r.V != gateway.Version {
		g.t.Fatalf("READY: %d %s", seq, raw)
	}
	return r
}

func gwFixture(t *testing.T) *botFixture {
	t.Helper()
	f := newBotFixture(t, true)
	wantStatus(t, "intents", f.owner.patch(f.botPath, map[string]any{"allowed_intents": allBotIn}), http.StatusOK)
	return f
}

func TestGatewayDeliversOnlyWhatTheBotMaySee(t *testing.T) {
	f := gwFixture(t)
	secret := f.a.createChannel(f.owner, "staff-only", "text")
	staff := createRole(t, f.owner, "Staff", 0)
	wantStatus(t, "private", putOverwrites(f.owner, "channels", secret,
		overwrite("role", everyoneID(t, f.owner), 0, perm.ViewChannel), overwrite("role", staff, perm.ViewChannel, 0)), http.StatusNoContent)

	g := f.dialGW("Bot " + f.token.Secret)
	g.identify("", bot.IntentGuildMessages)

	msg := postMessage(t, f.owner, f.open, map[string]any{"content": "hello bots"})
	seq, d := g.expect("MESSAGE_CREATE")
	var m chat.Message
	_ = json.Unmarshal(d, &m)
	if seq != 2 || m.ID != msg || m.Content != "hello bots" || m.ChannelID != f.open {
		t.Fatalf("event: %d %s", seq, d)
	}
	postMessage(t, f.owner, f.other, map[string]any{"content": "elsewhere"})
	postMessage(t, f.owner, secret, map[string]any{"content": "classified"})
	g.nothing()

	wantStatus(t, "edit", f.owner.put("/api/channels/"+f.open.String()+"/messages/"+msg.String(), map[string]any{"content": "edited"}), http.StatusOK)
	if seq, _ := g.expect("MESSAGE_UPDATE"); seq != 3 {
		t.Fatalf("seq %d", seq)
	}
	// Reactions are a separate intent the bot did not ask for.
	wantStatus(t, "react", f.owner.post("/api/messages/"+msg.String()+"/reactions", map[string]string{"emoji": "👍"}), http.StatusOK)
	g.nothing()
	wantStatus(t, "delete", f.owner.delete("/api/channels/"+f.open.String()+"/messages/"+msg.String()), http.StatusNoContent)
	_, d = g.expect("MESSAGE_DELETE")
	if !strings.Contains(string(d), msg.String()) {
		t.Fatalf("delete: %s", d)
	}

	// The bot's own messages come back too, and a REST post shows up.
	wantStatus(t, "bot posts", f.client.say(f.open, "from the bot"), http.StatusCreated)
	_, d = g.expect("MESSAGE_CREATE")
	_ = json.Unmarshal(d, &m)
	if m.UserID != f.bot.UserID || !m.IsBot {
		t.Fatalf("own message: %s", d)
	}
}

func TestGatewayIntentsGateEventClasses(t *testing.T) {
	f := gwFixture(t)
	g := f.dialGW("Bot " + f.token.Secret)
	g.identify("", bot.IntentGuildMessageReactions|bot.IntentGuilds)

	msg := postMessage(t, f.owner, f.open, map[string]any{"content": "react"})
	g.nothing() // messages were not asked for
	wantStatus(t, "react", f.owner.post("/api/messages/"+msg.String()+"/reactions", map[string]string{"emoji": "👍"}), http.StatusOK)
	_, d := g.expect("MESSAGE_REACTIONS_UPDATE")
	if !strings.Contains(string(d), f.open.String()) {
		t.Fatalf("reaction: %s", d)
	}
}

func TestGatewayPermissionChangesApplyLive(t *testing.T) {
	f := gwFixture(t)
	g := f.dialGW("Bot " + f.token.Secret)
	g.identify("", bot.IntentGuildMessages|bot.IntentGuilds)

	postMessage(t, f.owner, f.open, map[string]any{"content": "visible"})
	g.expect("MESSAGE_CREATE")

	// Taking the channel away stops events at once and prompts a refetch.
	wantStatus(t, "revoke access", putOverwrites(f.owner, "channels", f.open), http.StatusNoContent)
	var sawUpdate bool
	for i := 0; i < 3; i++ {
		name, _, _, ok := g.dispatch(quiet)
		if ok && name == "CHANNELS_UPDATE" {
			sawUpdate = true
		}
	}
	if !sawUpdate {
		t.Fatal("no CHANNELS_UPDATE after the access changed")
	}
	postMessage(t, f.owner, f.open, map[string]any{"content": "now hidden"})
	g.nothing()

	// Giving it back, events flow again.
	wantStatus(t, "restore", putOverwrites(f.owner, "channels", f.open, overwrite("role", f.roleID, perm.ViewChannel, 0)), http.StatusNoContent)
	g.dispatch(quiet)
	postMessage(t, f.owner, f.open, map[string]any{"content": "visible again"})
	deadline := time.Now().Add(within)
	for {
		name, _, d, ok := g.dispatch(time.Until(deadline))
		if !ok {
			t.Fatal("no event after access was restored")
		}
		if name == "MESSAGE_CREATE" && strings.Contains(string(d), "visible again") {
			break
		}
	}
}

func TestGatewayMemberAdd(t *testing.T) {
	f := gwFixture(t)
	g := f.dialGW("Bot " + f.token.Secret)
	g.identify("", bot.IntentGuildMembers)
	max := f.a.register(f.owner, "max")
	_, d := g.expect("GUILD_MEMBER_ADD")
	var m gateway.BotMember
	_ = json.Unmarshal(d, &m)
	if m.ID != max.user.ID || m.Username != "max" || strings.Contains(string(d), "bio") {
		t.Fatalf("member: %s", d)
	}
}

func TestGatewayHandshake(t *testing.T) {
	f := gwFixture(t)
	f.a.router.Gateway.SetTimings(gateway.Timings{Heartbeat: 150 * time.Millisecond, Identify: 400 * time.Millisecond, Resume: time.Minute, TokenCheck: time.Minute})

	// The token may come in IDENTIFY; the header is optional.
	g := f.dialGW("")
	r := g.identify(f.token.Secret, bot.IntentGuildMessages)
	if r.User.ID != f.bot.UserID || !r.User.Bot || r.User.Username != "helper" || r.Intents != bot.IntentGuildMessages || !strings.HasSuffix(r.ResumeURL, "/api/bot/v1/gateway") {
		t.Fatalf("READY: %+v", r)
	}
	// A heartbeat is answered; going silent ends the session (zombie).
	g.send(gateway.OpHeartbeat, nil)
	if fr, ok := g.next(within); !ok || fr.Op != gateway.OpHeartbeatAck {
		t.Fatalf("no ack: %+v", fr)
	}
	g.waitClose(gateway.CloseSessionTimeout)

	// Failures, each with Discord's close code.
	cases := []struct {
		name   string
		header string
		send   func(*gwClient)
		code   int
	}{
		{"wrong token in IDENTIFY", "", func(g *gwClient) {
			g.send(gateway.OpIdentify, map[string]any{"token": bot.TokenPrefix + strings.Repeat("Z", 43), "intents": 0})
		}, gateway.CloseAuthFailed},
		{"no token at all", "", func(g *gwClient) { g.send(gateway.OpIdentify, map[string]any{"intents": 0}) }, gateway.CloseAuthFailed},
		{"wrong token in the header", "Bot " + bot.TokenPrefix + strings.Repeat("Z", 43), func(*gwClient) {}, gateway.CloseAuthFailed},
		{"never identifies", "", func(*gwClient) {}, gateway.CloseNotAuthenticated},
		{"no intents", "Bot " + f.token.Secret, func(g *gwClient) { g.send(gateway.OpIdentify, map[string]any{}) }, gateway.CloseInvalidIntents},
		{"unknown intent bits", "Bot " + f.token.Secret, func(g *gwClient) { g.send(gateway.OpIdentify, map[string]any{"intents": 1 << 40}) }, gateway.CloseInvalidIntents},
		{"not decodable", "Bot " + f.token.Secret, func(g *gwClient) { _ = g.ws.WriteMessage(websocket.TextMessage, []byte("nope")) }, 4002},
		{"unknown opcode", "Bot " + f.token.Secret, func(g *gwClient) { g.send(42, nil) }, 4001},
		{"too large", "Bot " + f.token.Secret, func(g *gwClient) {
			_ = g.ws.WriteMessage(websocket.TextMessage, []byte(`{"op":1,"d":"`+strings.Repeat("x", 6000)+`"}`))
		}, websocket.CloseMessageTooBig},
		{"identify twice", "Bot " + f.token.Secret, func(g *gwClient) {
			g.send(gateway.OpIdentify, map[string]any{"intents": 0})
			g.send(gateway.OpIdentify, map[string]any{"intents": 0})
		}, gateway.CloseAlreadyIdentified},
	}
	for _, c := range cases {
		g := f.dialGW(c.header)
		c.send(g)
		t.Log(c.name)
		g.waitClose(c.code)
	}

	// Intents beyond what the admin allowed.
	wantStatus(t, "narrow", f.owner.patch(f.botPath, map[string]any{"allowed_intents": bot.IntentGuilds}), http.StatusOK)
	g = f.dialGW("Bot " + f.token.Secret)
	g.hello()
	g.send(gateway.OpIdentify, map[string]any{"intents": bot.IntentGuildMessages})
	g.waitClose(gateway.CloseDisallowedIntents)

	// The token is never taken from the URL.
	g = f.dialGWTo("/api/bot/v1/gateway?token="+f.token.Secret, "")
	g.hello()
	g.send(gateway.OpIdentify, map[string]any{"intents": 0})
	g.waitClose(gateway.CloseAuthFailed)

	// The plain GET tells the address and needs the token.
	wantStatus(t, "info without token", (&botClient{t: t, a: f.a}).get("/api/bot/v1/gateway"), http.StatusUnauthorized)
	res := f.client.get("/api/bot/v1/gateway")
	wantStatus(t, "info", res, http.StatusOK)
	var info gateway.Info
	res.decode(t, &info)
	if !strings.HasPrefix(info.URL, "ws://") || !strings.HasSuffix(info.URL, "/api/bot/v1/gateway") || info.HeartbeatInterval != 150 || info.MaxConnections < 1 {
		t.Fatalf("info: %+v", info)
	}
}

func TestGatewayResume(t *testing.T) {
	f := gwFixture(t)
	f.a.router.Gateway.SetTimings(gateway.Timings{Heartbeat: time.Minute, Identify: 10 * time.Second, Resume: 700 * time.Millisecond, TokenCheck: time.Minute})
	g := f.dialGW("Bot " + f.token.Secret)
	ready := g.identify("", bot.IntentGuildMessages)
	postMessage(t, f.owner, f.open, map[string]any{"content": "one"})
	if seq, _ := g.expect("MESSAGE_CREATE"); seq != 2 {
		t.Fatalf("seq %d", seq)
	}
	g.ws.Close()

	// Events while disconnected are kept.
	postMessage(t, f.owner, f.open, map[string]any{"content": "two"})
	postMessage(t, f.owner, f.open, map[string]any{"content": "three"})
	time.Sleep(50 * time.Millisecond)

	g2 := f.dialGW("")
	g2.hello()
	g2.send(gateway.OpResume, map[string]any{"token": f.token.Secret, "session_id": ready.SessionID, "seq": 2})
	for i, want := range []string{"two", "three"} {
		seq, d := g2.expect("MESSAGE_CREATE")
		if seq != int64(3+i) || !strings.Contains(string(d), want) {
			t.Fatalf("replay %d: %d %s", i, seq, d)
		}
	}
	if seq, _ := g2.expect("RESUMED"); seq != 4 {
		t.Fatalf("RESUMED seq %d", seq)
	}
	// Live again, in sequence.
	postMessage(t, f.owner, f.open, map[string]any{"content": "four"})
	if seq, _ := g2.expect("MESSAGE_CREATE"); seq != 5 {
		t.Fatalf("seq %d", seq)
	}

	// A resume replaces the connection that holds the session.
	g3 := f.dialGW("")
	g3.hello()
	g3.send(gateway.OpResume, map[string]any{"token": f.token.Secret, "session_id": ready.SessionID, "seq": 5})
	g3.expect("RESUMED")
	g2.waitClose(gateway.CloseUnknownError)

	// Not resumable: unknown session, a sequence ahead of the session, another bot's session.
	invalid := func(session string, seq int64, token string) {
		g := f.dialGW("")
		g.hello()
		g.send(gateway.OpResume, map[string]any{"token": token, "session_id": session, "seq": seq})
		fr, ok := g.next(within)
		if !ok || fr.Op != gateway.OpInvalidSession || string(fr.D) != "false" {
			t.Fatalf("want INVALID_SESSION, got %+v", fr)
		}
		// The connection stays open and can identify instead.
		g.send(gateway.OpIdentify, map[string]any{"token": token, "intents": 0})
		g.expect("READY")
	}
	invalid("nope", 0, f.token.Secret)
	invalid(ready.SessionID, 999, f.token.Secret)
	other := createBot(t, f.owner, "second")
	otherTok := issueToken(t, f.owner, other.ID, map[string]any{})
	invalid(ready.SessionID, 5, otherTok.Secret)

	// After the window the session is gone.
	g3.ws.Close()
	time.Sleep(1300 * time.Millisecond)
	invalid(ready.SessionID, 5, f.token.Secret)
}

func TestGatewayConnectionCap(t *testing.T) {
	f := gwFixture(t)
	for i := 0; i < 3; i++ {
		f.dialGW("Bot "+f.token.Secret).identify("", 0)
	}
	g := f.dialGW("Bot " + f.token.Secret)
	g.hello()
	g.send(gateway.OpIdentify, map[string]any{"intents": 0})
	g.waitClose(gateway.CloseRateLimited)

	// Another bot has its own allowance.
	other := createBot(t, f.owner, "second")
	tok := issueToken(t, f.owner, other.ID, map[string]any{})
	f.dialGW("Bot "+tok.Secret).identify("", 0)
}

func TestGatewayFrameRateLimit(t *testing.T) {
	f := gwFixture(t)
	g := f.dialGW("Bot " + f.token.Secret)
	g.identify("", 0)
	for i := 0; i < 130; i++ {
		if err := g.ws.WriteMessage(websocket.TextMessage, []byte(`{"op":1,"d":null}`)); err != nil {
			break
		}
	}
	g.waitClose(gateway.CloseRateLimited)
}

// Revocation takes effect at once, also for connections that are open.
func TestGatewayRevocationClosesConnections(t *testing.T) {
	f := gwFixture(t)
	connect := func(token string) *gwClient {
		g := f.dialGW("Bot " + token)
		g.identify("", bot.IntentGuildMessages)
		return g
	}

	g := connect(f.token.Secret)
	wantStatus(t, "revoke token", f.owner.delete(f.botPath+"/tokens/"+f.token.ID.String()), http.StatusNoContent)
	g.waitClose(gateway.CloseAuthFailed)

	// Only the revoked token's connections close.
	a, b := issueToken(t, f.owner, f.bot.ID, map[string]any{}), issueToken(t, f.owner, f.bot.ID, map[string]any{})
	ga, gb := connect(a.Secret), connect(b.Secret)
	wantStatus(t, "revoke a", f.owner.delete(f.botPath+"/tokens/"+a.ID.String()), http.StatusNoContent)
	ga.waitClose(gateway.CloseAuthFailed)
	postMessage(t, f.owner, f.open, map[string]any{"content": "still here"})
	gb.expect("MESSAGE_CREATE")

	// Regenerating, disabling and deleting end them as well.
	wantStatus(t, "regenerate", f.owner.post(f.botPath+"/tokens/"+b.ID.String()+"/regenerate", nil), http.StatusCreated)
	gb.waitClose(gateway.CloseAuthFailed)
	c := issueToken(t, f.owner, f.bot.ID, map[string]any{})
	gc := connect(c.Secret)
	wantStatus(t, "disable", f.owner.patch(f.botPath, map[string]any{"disabled": true}), http.StatusOK)
	gc.waitClose(gateway.CloseAuthFailed)
	wantStatus(t, "enable", f.owner.patch(f.botPath, map[string]any{"disabled": false}), http.StatusOK)
	d := issueToken(t, f.owner, f.bot.ID, map[string]any{})
	gd := connect(d.Secret)
	wantStatus(t, "delete", f.owner.delete(f.botPath), http.StatusNoContent)
	gd.waitClose(gateway.CloseAuthFailed)
}

// A token that ends without the API being asked (expiry, a database edit) ends
// its connections too, within the housekeeping interval.
func TestGatewayClosesSessionsOfExpiredTokens(t *testing.T) {
	f := gwFixture(t)
	f.a.router.Gateway.SetTimings(gateway.Timings{Heartbeat: time.Minute, Identify: 10 * time.Second, Resume: time.Minute, TokenCheck: 200 * time.Millisecond})
	g := f.dialGW("Bot " + f.token.Secret)
	g.identify("", 0)
	if _, err := f.a.db.Exec(t.Context(), `UPDATE bot_tokens SET expires_at = NOW() - INTERVAL '1 second' WHERE id = $1`, f.token.ID); err != nil {
		t.Fatal(err)
	}
	g.waitClose(gateway.CloseAuthFailed)
}

// Bots that lose their permission to see a channel are not told about the
// people in it, and members are not told about channels they cannot see.
func TestGatewayNeverLeaksAcrossScope(t *testing.T) {
	f := gwFixture(t)
	g := f.dialGW("Bot " + f.token.Secret)
	g.identify("", allBotIn)

	// Typing and presence style events are not offered at all.
	f.a.router.Hub.Broadcast("presence_update", map[string]any{"user_id": uuid.New()})
	f.a.router.Hub.BroadcastChannel(f.open, "typing", map[string]any{"user_id": uuid.New()})
	g.nothing()
}

func TestGatewayProtocolErrors(t *testing.T) {
	f := gwFixture(t)
	hdr := "Bot " + f.token.Secret
	closes := []struct {
		name string
		send func(*gwClient)
		code int
	}{
		{"resume after identify", func(g *gwClient) {
			g.send(gateway.OpIdentify, map[string]any{"intents": 0})
			g.expect("READY")
			g.send(gateway.OpResume, map[string]any{"session_id": "x", "seq": 0})
		}, gateway.CloseAlreadyIdentified},
		{"identify with a malformed d", func(g *gwClient) { g.send(gateway.OpIdentify, "not an object") }, 4002},
		{"resume with a malformed d", func(g *gwClient) { g.send(gateway.OpResume, "not an object") }, 4002},
		{"resume with a wrong token", func(g *gwClient) {
			g.send(gateway.OpResume, map[string]any{"token": bot.TokenPrefix + strings.Repeat("Q", 43), "session_id": "x", "seq": 0})
		}, gateway.CloseAuthFailed},
	}
	for _, c := range closes {
		g := f.dialGW(hdr)
		g.hello()
		c.send(g)
		t.Log(c.name)
		g.waitClose(c.code)
	}
	// Opening many gateways is limited per bot: past 20 IDENTIFY or RESUME in a minute.
	var last *gwClient
	for i := 0; i < 22; i++ {
		last = f.dialGW(hdr)
		last.hello()
		last.send(gateway.OpIdentify, map[string]any{"intents": 1 << 40})
		code := 0
		select {
		case code = <-last.closed:
		case <-time.After(within):
			t.Fatal("not closed")
		}
		if code == gateway.CloseRateLimited {
			break
		}
		if code != gateway.CloseInvalidIntents {
			t.Fatalf("closed with %d", code)
		}
		if i == 21 {
			t.Fatal("identifying was never limited")
		}
	}
	// A resume is limited by the same allowance.
	g := f.dialGW(hdr)
	g.hello()
	g.send(gateway.OpResume, map[string]any{"session_id": "x", "seq": 0})
	g.waitClose(gateway.CloseRateLimited)
}

func TestGatewayUpgradesAreLimitedPerAddress(t *testing.T) {
	f := gwFixture(t)
	var refused bool
	for i := 0; i < 40; i++ {
		ws, res, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(f.a.srv.URL, "http")+"/api/bot/v1/gateway", nil)
		if err != nil {
			if res != nil && res.StatusCode == http.StatusTooManyRequests {
				refused = true
				break
			}
			t.Fatal(err)
		}
		ws.Close()
	}
	if !refused {
		t.Fatal("gateway upgrades are not limited")
	}
}

func TestGatewayRefusesABrokenUpgrade(t *testing.T) {
	f := gwFixture(t)
	req, _ := http.NewRequest(http.MethodGet, f.a.srv.URL+"/api/bot/v1/gateway", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", res.StatusCode)
	}
}
