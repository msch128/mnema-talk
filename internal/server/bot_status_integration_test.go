//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/bot"
	"github.com/msch128/mnema-talk/internal/gateway"
)

type seenPresence struct {
	Status   string `json:"status"`
	Activity *struct {
		Type int    `json:"type"`
		Name string `json:"name"`
	} `json:"activity"`
}

// presenceOf waits for the next presence_update about the user.
func presenceOf(t *testing.T, w *wsConn, user uuid.UUID, d time.Duration) (seenPresence, bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		raw, ok := w.expect("presence_update", time.Until(deadline))
		if !ok {
			break
		}
		var p struct {
			UserID uuid.UUID `json:"user_id"`
			seenPresence
		}
		_ = json.Unmarshal(raw, &p)
		if p.UserID == user {
			return p.seenPresence, true
		}
	}
	return seenPresence{}, false
}

func wantPresence(t *testing.T, w *wsConn, user uuid.UUID, status string) seenPresence {
	t.Helper()
	p, ok := presenceOf(t, w, user, 5*time.Second)
	if !ok || p.Status != status {
		t.Fatalf("want presence %q, got %+v (%v)", status, p, ok)
	}
	return p
}

func TestBotSetsItsStatusAndActivity(t *testing.T) {
	f := gwFixture(t)
	watcher, _, err := f.owner.dialWS(f.a.origin())
	if err != nil {
		t.Fatal(err)
	}
	watcher.expect("presence_snapshot", soon)

	g := f.dialGW("Bot " + f.token.Secret)
	g.identify("", 0)
	wantPresence(t, watcher, f.bot.UserID, "online")

	set := func(status string, activities ...map[string]any) {
		t.Helper()
		g.send(gateway.OpPresenceUpdate, map[string]any{"since": nil, "status": status, "afk": false, "activities": activities})
	}
	// Discord's idle shows as away; the activity is sanitized (control and
	// bidi characters out, spaces collapsed, length limited).
	set("idle", map[string]any{"name": "  chess\n‮  with\tfriends ", "type": 0})
	if p := wantPresence(t, watcher, f.bot.UserID, "away"); p.Activity == nil || p.Activity.Name != "chess with friends" || p.Activity.Type != 0 {
		t.Fatalf("activity: %+v", p.Activity)
	}
	// A custom status carries its text in state; only the first activity counts.
	set("dnd", map[string]any{"name": "Custom Status", "state": strings.Repeat("x", 200), "type": 4}, map[string]any{"name": "ignored", "type": 0})
	if p := wantPresence(t, watcher, f.bot.UserID, "dnd"); p.Activity == nil || p.Activity.Type != 4 || len([]rune(p.Activity.Name)) != gateway.MaxActivityLen {
		t.Fatalf("custom activity: %+v", p.Activity)
	}

	// Someone who connects now sees the status in the snapshot and the activity next to it.
	late, _, err := f.owner.dialWS(f.a.origin())
	if err != nil {
		t.Fatal(err)
	}
	snap, ok := late.expect("presence_snapshot", soon)
	var now map[uuid.UUID]string
	if !ok || json.Unmarshal(snap, &now) != nil || now[f.bot.UserID] != "dnd" {
		t.Fatalf("snapshot: %s", snap)
	}
	if p := wantPresence(t, late, f.bot.UserID, "dnd"); p.Activity == nil || p.Activity.Type != 4 {
		t.Fatalf("activity for a late joiner: %+v", p.Activity)
	}

	// Invisible reads as offline and hides the activity.
	set("invisible", map[string]any{"name": "secret", "type": 0})
	if p := wantPresence(t, watcher, f.bot.UserID, "offline"); p.Activity != nil {
		t.Fatalf("an invisible bot shows %+v", p.Activity)
	}
	// Back to online, no activity.
	set("online")
	if p := wantPresence(t, watcher, f.bot.UserID, "online"); p.Activity != nil {
		t.Fatalf("activity: %+v", p.Activity)
	}

	// The status ends with the last connection: a bot that comes back is online.
	set("dnd")
	wantPresence(t, watcher, f.bot.UserID, "dnd")
	g.ws.Close()
	wantPresence(t, watcher, f.bot.UserID, "offline")
	g2 := f.dialGW("Bot " + f.token.Secret)
	g2.identify("", 0)
	wantPresence(t, watcher, f.bot.UserID, "online")

	// IDENTIFY may carry the starting presence.
	g2.ws.Close()
	wantPresence(t, watcher, f.bot.UserID, "offline")
	g3 := f.dialGW("Bot " + f.token.Secret)
	g3.hello()
	g3.send(gateway.OpIdentify, map[string]any{"intents": 0, "presence": map[string]any{"status": "idle", "activities": []map[string]any{{"name": "booting", "type": 3}}}})
	g3.expect("READY")
	if p := wantPresence(t, watcher, f.bot.UserID, "away"); p.Activity == nil || p.Activity.Name != "booting" || p.Activity.Type != 3 {
		t.Fatalf("identify presence: %+v", p.Activity)
	}
}

func TestBotPresenceUpdatesAreValidatedAndLimited(t *testing.T) {
	f := gwFixture(t)
	watcher, _, err := f.owner.dialWS(f.a.origin())
	if err != nil {
		t.Fatal(err)
	}
	watcher.expect("presence_snapshot", soon)

	// Before IDENTIFY there is nobody to set a status for.
	early := f.dialGW("Bot " + f.token.Secret)
	early.hello()
	early.send(gateway.OpPresenceUpdate, map[string]any{"status": "dnd"})
	early.waitClose(gateway.CloseNotAuthenticated)

	// A bad status, a bad activity type and streaming (needs a URL) close with 4002.
	for name, body := range map[string]map[string]any{
		"status":    {"status": "busy"},
		"no status": {"activities": []map[string]any{}},
		"type":      {"status": "online", "activities": []map[string]any{{"name": "x", "type": 9}}},
		"streaming": {"status": "online", "activities": []map[string]any{{"name": "x", "type": 1}}},
	} {
		g := f.dialGW("Bot " + f.token.Secret)
		g.identify("", 0)
		g.send(gateway.OpPresenceUpdate, body)
		g.waitClose(4002)
		_ = name
	}
	// Let the presence changes of the connections above settle.
	for {
		if _, ok := watcher.expect("presence_update", 500*time.Millisecond); !ok {
			break
		}
	}

	// Five updates a minute per bot; the rest are dropped without a word.
	g := f.dialGW("Bot " + f.token.Secret)
	g.identify("", 0)
	for _, s := range []string{"dnd", "idle", "dnd", "idle", "dnd"} {
		g.send(gateway.OpPresenceUpdate, map[string]any{"status": s})
	}
	deadline := time.Now().Add(3 * time.Second)
	var last string
	for time.Now().Before(deadline) && last != "dnd" {
		if p, ok := presenceOf(t, watcher, f.bot.UserID, time.Second); ok {
			last = p.Status
		}
	}
	g.send(gateway.OpPresenceUpdate, map[string]any{"status": "invisible"})
	if p, ok := presenceOf(t, watcher, f.bot.UserID, quiet); ok && p.Status == "offline" {
		t.Fatal("the sixth update in a minute was applied")
	}
	// The connection stays open.
	g.send(gateway.OpHeartbeat, nil)
	if fr, ok := g.next(within); !ok || fr.Op != gateway.OpHeartbeatAck {
		t.Fatalf("connection after a dropped update: %+v %v", fr, ok)
	}
}

// A timed-out bot is silenced everywhere a bot can speak: REST, reactions,
// uploads and the responses to slash commands.
func TestTimedOutBotCannotSpeak(t *testing.T) {
	f := gwFixture(t)
	f.a.router.Bots.SetLimits(1000, 1000)
	max := f.a.register(f.owner, "max")
	wantStatus(t, "register", f.client.post(bot.BasePath+"/commands", pingCommand), http.StatusOK)
	cmd := channelCommands(t, max, f.open)[0]
	g := f.dialGW("Bot " + f.token.Secret)
	g.identify("", bot.IntentGuildMessages)

	msg := postMessage(t, f.owner, f.open, map[string]any{"content": "hello"})
	wantStatus(t, "speaks", f.client.say(f.open, "before"), http.StatusCreated)
	res := max.post("/api/channels/"+f.open.String()+"/interactions", map[string]any{"command_id": cmd.ID, "options": map[string]any{"text": "x"}})
	wantStatus(t, "run before", res, http.StatusAccepted)
	_, d := g.expect("MESSAGE_CREATE") // the "hello" above
	_, d = g.expect("MESSAGE_CREATE")  // the bot's own "before"
	_, d = g.expect("INTERACTION_CREATE")
	var inv struct {
		ID    uuid.UUID `json:"id"`
		Token string    `json:"token"`
	}
	_ = json.Unmarshal(d, &inv)

	wantStatus(t, "timeout the bot", f.owner.post("/api/admin/users/"+f.bot.UserID.String()+"/timeout", map[string]any{"duration_seconds": 600, "reason": "spam"}), http.StatusOK)

	wantStatus(t, "post", f.client.say(f.open, "after"), http.StatusForbidden)
	wantStatus(t, "react", f.client.put(botPath(f.open, "/messages/"+msg.String()+"/reactions/%F0%9F%91%8D/@me")), http.StatusForbidden)
	reply := func(path string, body any) response {
		return f.client.post(bot.BasePath+"/interactions/"+inv.ID.String()+"/"+inv.Token+path, body)
	}
	wantStatus(t, "slash command response", reply("/callback", map[string]any{"type": 4, "data": map[string]any{"content": "pong"}}), http.StatusForbidden)
	// Reading still works, and the bot keeps its gateway connection.
	wantStatus(t, "read", f.client.msgs(f.open), http.StatusOK)
	postMessage(t, f.owner, f.open, map[string]any{"content": "still readable"})
	g.expect("MESSAGE_CREATE")

	// Lifting the timeout gives the voice back.
	wantStatus(t, "untimeout", f.owner.delete("/api/admin/users/"+f.bot.UserID.String()+"/timeout"), http.StatusNoContent)
	wantStatus(t, "speaks again", f.client.say(f.open, "back"), http.StatusCreated)
	wantStatus(t, "response", reply("/followups", map[string]any{"content": "pong"}), http.StatusCreated)
}

// A banned bot is out: its token is refused, its gateway sessions close, and
// lifting the ban brings it back (a ban is separate from the bot's own switch).
func TestBannedBotIsRefusedAndDisconnected(t *testing.T) {
	f := gwFixture(t)
	g := f.dialGW("Bot " + f.token.Secret)
	g.identify("", 0)
	banned := f.dialGW("Bot " + f.token.Secret)
	banned.identify("", 0)
	path := "/api/admin/users/" + f.bot.UserID.String() + "/ban"

	wantStatus(t, "ban", f.owner.post(path, map[string]any{"reason": "rogue"}), http.StatusNoContent)
	g.waitClose(gateway.CloseAuthFailed)
	banned.waitClose(gateway.CloseAuthFailed)
	wantStatus(t, "rest", f.client.get(bot.BasePath+"/users/@me"), http.StatusUnauthorized)
	wantStatus(t, "post", f.client.say(f.open, "x"), http.StatusUnauthorized)

	// It cannot connect or resume while banned.
	again := f.dialGW("")
	again.hello()
	again.send(gateway.OpIdentify, map[string]any{"token": f.token.Secret, "intents": 0})
	again.waitClose(gateway.CloseAuthFailed)
	upgrade := f.dialGW("Bot " + f.token.Secret)
	upgrade.waitClose(gateway.CloseAuthFailed)

	wantStatus(t, "unban", f.owner.delete(path), http.StatusNoContent)
	wantStatus(t, "rest again", f.client.get(bot.BasePath+"/users/@me"), http.StatusOK)
	back := f.dialGW("Bot " + f.token.Secret)
	back.identify("", 0)
	// A running ban ends nothing else: an expiring one lifts itself.
	wantStatus(t, "short ban", f.owner.post(path, map[string]any{"reason": "", "duration_seconds": 60}), http.StatusNoContent)
	back.waitClose(gateway.CloseAuthFailed)
	if _, err := f.a.db.Exec(t.Context(), `UPDATE users SET banned_until = NOW() - INTERVAL '1 second' WHERE id = $1`, f.bot.UserID); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, "after expiry", f.client.get(bot.BasePath+"/users/@me"), http.StatusOK)
}
