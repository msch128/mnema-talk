//go:build integration

package server

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/webhook"
)

// hook is a created webhook with its secret execute URL path.
type hook struct {
	webhook.CreatedWebhook
	path string
}

func makeHook(t *testing.T, c *client, channel uuid.UUID, name string) hook {
	t.Helper()
	res := c.post("/api/admin/channels/"+channel.String()+"/webhooks", map[string]any{"name": name})
	if res.status != http.StatusCreated {
		t.Fatalf("create webhook: %d %s", res.status, res.body)
	}
	var h hook
	res.decode(t, &h.CreatedWebhook)
	h.path = "/api/webhooks/" + h.ID.String() + "/" + h.Token
	return h
}

// exec posts to a webhook the way a script does: no cookie, no Origin.
func (a *app) exec(path string, body any, hdr map[string]string) response {
	a.t.Helper()
	h := map[string]string{"Origin": ""}
	for k, v := range hdr {
		h[k] = v
	}
	return a.anon().do(http.MethodPost, path, body, h)
}

func lastMessage(t *testing.T, a *app, c *client, channel uuid.UUID) chat.Message {
	t.Helper()
	res := c.get("/api/channels/" + channel.String() + "/messages?limit=50")
	if res.status != http.StatusOK {
		t.Fatalf("messages: %d %s", res.status, res.body)
	}
	var msgs []chat.Message
	res.decode(t, &msgs)
	if len(msgs) == 0 {
		t.Fatal("no messages")
	}
	return msgs[len(msgs)-1]
}

func TestWebhookPostsAsTheWebhookNotAMember(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "alerts", "text")
	h := makeHook(t, admin, ch, "CI Bot")
	if h.Token == "" || !strings.HasSuffix(h.URL, h.path) {
		t.Fatalf("created webhook %+v", h)
	}

	res := a.exec(h.path+"?wait=true", map[string]any{
		"content":  "build **passed**",
		"username": "Pipeline",
		"embeds": []map[string]any{{
			"title": "Run 42", "description": "all green", "url": "https://example.com/run/42", "color": 65280,
			"timestamp": "2026-01-02T03:04:05+01:00",
			"fields":    []map[string]any{{"name": "tests", "value": "812", "inline": true}},
			"footer":    map[string]any{"text": "ci", "icon_url": "https://example.com/i.png"},
			"image":     map[string]any{"url": "https://example.com/i.png"},
			"author":    map[string]any{"name": "robot", "url": "https://example.com"},
		}},
	}, nil)
	if res.status != http.StatusOK {
		t.Fatalf("execute: %d %s", res.status, res.body)
	}
	var msg chat.Message
	res.decode(t, &msg)
	if !msg.IsBot || msg.DisplayName != "Pipeline" || msg.Content != "build **passed**" {
		t.Fatalf("message %+v", msg)
	}
	if len(msg.Embeds) != 1 || msg.Embeds[0].Timestamp != "2026-01-02T02:04:05Z" || msg.Embeds[0].Fields[0].Value != "812" || msg.Embeds[0].Footer.Text != "ci" {
		t.Fatalf("embeds %+v", msg.Embeds)
	}
	if msg.UserID == admin.user.ID {
		t.Fatal("webhook message is authored by the creator")
	}
	if got := a.events.Snapshot(); len(got) == 0 || got[len(got)-1].Type != "message_create" || got[len(got)-1].ChannelID != ch {
		t.Fatalf("events %+v", got)
	}

	// No username: the webhook's name. 204 without wait.
	if res := a.exec(h.path, map[string]any{"content": "plain"}, nil); res.status != http.StatusNoContent {
		t.Fatalf("no wait: %d %s", res.status, res.body)
	}
	if m := lastMessage(t, a, admin, ch); m.DisplayName != "CI Bot" || !m.IsBot {
		t.Fatalf("history %+v", m)
	}

	// The bot account is neither a member nor an admin-list entry.
	var members []struct{ Username string }
	admin.get("/api/members").decode(t, &members)
	for _, m := range members {
		if strings.HasPrefix(m.Username, "wh-") {
			t.Fatalf("bot listed as member: %v", members)
		}
	}
	var users []struct{ Username string }
	admin.get("/api/admin/users").decode(t, &users)
	for _, u := range users {
		if strings.HasPrefix(u.Username, "wh-") {
			t.Fatalf("bot listed in admin users: %v", users)
		}
	}
	// ... and cannot sign in.
	var bot string
	if err := a.db.QueryRow(t.Context(), `SELECT username FROM users WHERE is_bot`).Scan(&bot); err != nil {
		t.Fatal(err)
	}
	if res := a.anon().post("/api/auth/login", map[string]string{"username": bot, "password": "!"}); res.status == http.StatusOK {
		t.Fatal("bot signed in")
	}
}

func TestWebhookAuthentication(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "alerts", "text")
	h := makeHook(t, admin, ch, "bot")
	body := map[string]any{"content": "hi"}

	cases := []struct {
		name   string
		path   string
		status int
		code   int
	}{
		{"wrong token", "/api/webhooks/" + h.ID.String() + "/nope", 401, 50027},
		{"unknown id", "/api/webhooks/" + uuid.NewString() + "/" + h.Token, 404, 10015},
		{"bad id", "/api/webhooks/not-a-uuid/" + h.Token, 404, 10015},
	}
	for _, tc := range cases {
		res := a.exec(tc.path, body, nil)
		var e webhook.DiscordError
		res.decode(t, &e)
		if res.status != tc.status || e.Code != tc.code {
			t.Errorf("%s: %d %s", tc.name, res.status, res.body)
		}
	}
	// A session cookie does not authenticate or substitute the token, and a
	// foreign Origin does not matter: the URL is the credential.
	res := admin.do(http.MethodPost, "/api/webhooks/"+h.ID.String()+"/nope", body, map[string]string{"Origin": "https://evil.example"})
	if res.status != 401 {
		t.Fatalf("cookie must not help: %d %s", res.status, res.body)
	}
	if res := a.exec(h.path, body, map[string]string{"Origin": "https://elsewhere.example"}); res.status != 204 {
		t.Fatalf("foreign origin with token: %d %s", res.status, res.body)
	}

	// Regenerating the token kills the old URL.
	var fresh webhook.CreatedWebhook
	rr := admin.post("/api/admin/webhooks/"+h.ID.String()+"/token", nil)
	rr.decode(t, &fresh)
	if rr.status != 200 || fresh.Token == h.Token {
		t.Fatalf("regenerate: %d %s", rr.status, rr.body)
	}
	if res := a.exec(h.path, body, nil); res.status != 401 {
		t.Fatalf("old token: %d", res.status)
	}
	if res := a.exec("/api/webhooks/"+h.ID.String()+"/"+fresh.Token, body, nil); res.status != 204 {
		t.Fatalf("new token: %d %s", res.status, res.body)
	}
	// Only the hash is stored.
	var stored int
	if err := a.db.QueryRow(t.Context(), `SELECT COUNT(*) FROM webhooks WHERE token_hash = convert_to($1, 'UTF8')`, fresh.Token).Scan(&stored); err != nil || stored != 0 {
		t.Fatalf("plain token stored: %d %v", stored, err)
	}

	// Throttle brute force: failures per address are limited.
	last := 0
	for i := 0; i < 40; i++ {
		last = a.exec("/api/webhooks/"+h.ID.String()+"/guess"+fmt.Sprint(i), body, nil).status
	}
	if last != 429 {
		t.Fatalf("failed attempts are not limited: %d", last)
	}
}

func TestWebhookPayloadValidation(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "alerts", "text")
	h := makeHook(t, admin, ch, "bot")
	a.router.Webhooks.SetLimits(1000, 1000)

	long := strings.Repeat("x", chat.MaxMessageLen+1)
	cases := []struct {
		name string
		body any
		hdr  map[string]string
		want int
		code int
	}{
		{"empty", map[string]any{}, nil, 400, 50006},
		{"only whitespace", map[string]any{"content": "  "}, nil, 400, 50006},
		{"too long", map[string]any{"content": long}, nil, 400, 50035},
		{"bad username", map[string]any{"content": "x", "username": "a\nb"}, nil, 400, 50035},
		{"bad embed url", map[string]any{"embeds": []map[string]any{{"title": "t", "url": "javascript:alert(1)"}}}, nil, 400, 50035},
		{"bad embed image", map[string]any{"embeds": []map[string]any{{"image": map[string]any{"url": "ftp://x/y"}}}}, nil, 400, 50035},
		{"bad color", map[string]any{"embeds": []map[string]any{{"title": "t", "color": 1 << 30}}}, nil, 400, 50035},
		{"bad timestamp", map[string]any{"embeds": []map[string]any{{"title": "t", "timestamp": "yesterday"}}}, nil, 400, 50035},
		{"field without value", map[string]any{"embeds": []map[string]any{{"fields": []map[string]any{{"name": "n", "value": ""}}}}}, nil, 400, 50035},
		{"too many embeds", map[string]any{"embeds": make([]map[string]any, 11)}, nil, 400, 50035},
		{"empty embeds only", map[string]any{"embeds": []map[string]any{{}}}, nil, 400, 50006},
		{"not json", "text", nil, 400, 50035},
		{"mentions conflict", map[string]any{"content": "x", "allowed_mentions": map[string]any{"parse": []string{"users"}, "users": []string{uuid.NewString()}}}, nil, 400, 50035},
		{"wrong content type", "x", map[string]string{"Content-Type": "text/plain"}, 400, 50035},
	}
	for _, tc := range cases {
		var body any = tc.body
		if s, ok := tc.body.(string); ok {
			body = strings.NewReader(s)
		}
		res := a.exec(h.path, body, tc.hdr)
		var e webhook.DiscordError
		res.decode(t, &e)
		if res.status != tc.want || e.Code != tc.code {
			t.Errorf("%s: %d %s", tc.name, res.status, res.body)
		}
	}
}
