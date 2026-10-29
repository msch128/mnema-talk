//go:build integration

package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/bot"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/perm"
)

// botClient talks to the bot API like a script: a token in the Authorization
// header, no cookies, no Origin.
type botClient struct {
	t     *testing.T
	a     *app
	token string
	user  uuid.UUID
}

func (b *botClient) req(method, path string, body any, hdr map[string]string) response {
	b.t.Helper()
	var rd io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rd = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, b.a.srv.URL+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if b.token != "" {
		req.Header.Set("Authorization", "Bot "+b.token)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return response{status: res.StatusCode, body: raw, header: res.Header}
}

func (b *botClient) get(p string) response          { return b.req(http.MethodGet, p, nil, nil) }
func (b *botClient) post(p string, v any) response  { return b.req(http.MethodPost, p, v, nil) }
func (b *botClient) patch(p string, v any) response { return b.req(http.MethodPatch, p, v, nil) }
func (b *botClient) put(p string) response          { return b.req(http.MethodPut, p, nil, nil) }
func (b *botClient) delete(p string) response       { return b.req(http.MethodDelete, p, nil, nil) }
func botPath(ch uuid.UUID, rest string) string {
	return bot.BasePath + "/channels/" + ch.String() + rest
}
func (b *botClient) msgs(ch uuid.UUID) response { return b.get(botPath(ch, "/messages")) }
func (b *botClient) say(ch uuid.UUID, text string) response {
	return b.post(botPath(ch, "/messages"), map[string]any{"content": text})
}

type discordErr struct {
	Message    string  `json:"message"`
	Code       int     `json:"code"`
	RetryAfter float64 `json:"retry_after"`
}

func decodeDiscord(t *testing.T, r response) discordErr {
	t.Helper()
	var e discordErr
	r.decode(t, &e)
	return e
}

// botFixture has an Owner, two channels and a bot that may read, write, react
// and attach files in the first one only.
type botFixture struct {
	a       *app
	owner   *client
	open    uuid.UUID
	other   uuid.UUID
	bot     bot.Bot
	token   bot.NewTokenResponse
	client  *botClient
	roleID  uuid.UUID
	botPath string
}

func newBotFixture(t *testing.T, withHub bool) *botFixture {
	t.Helper()
	a := newApp(t, withHub)
	owner := a.seedAdmin()
	f := &botFixture{a: a, owner: owner}
	f.open = a.createChannel(owner, "open", "text")
	f.other = a.createChannel(owner, "other", "text")
	f.roleID = createRole(t, owner, "Helper bot", perm.SendMessages|perm.AddReactions|perm.AttachFiles)
	f.bot = createBot(t, owner, "helper")
	f.botPath = "/api/admin/bots/" + f.bot.ID.String()
	wantStatus(t, "bot role", owner.put(f.botPath+"/roles", map[string]any{"role_ids": []uuid.UUID{f.roleID}}), http.StatusOK)
	wantStatus(t, "scope", putOverwrites(owner, "channels", f.open, overwrite("role", f.roleID, perm.ViewChannel, 0)), http.StatusNoContent)
	f.token = issueToken(t, owner, f.bot.ID, map[string]any{"name": "test"})
	f.client = &botClient{t: t, a: a, token: f.token.Secret, user: f.bot.UserID}
	return f
}

func TestBotAPITokenAuth(t *testing.T) {
	f := newBotFixture(t, false)
	anon := &botClient{t: t, a: f.a}
	me := bot.BasePath + "/users/@me"

	res := f.client.get(me)
	wantStatus(t, "valid token", res, http.StatusOK)
	var self bot.Self
	res.decode(t, &self)
	if self.ID != f.bot.UserID || self.BotID != f.bot.ID || !self.Bot || self.Name != "helper" {
		t.Fatalf("me: %s", res.body)
	}

	// Discord-shaped 401 for every failure, nothing tells the reasons apart.
	for name, c := range map[string]*botClient{
		"no header":   anon,
		"wrong token": {t: t, a: f.a, token: bot.TokenPrefix + strings.Repeat("A", 43)},
		"garbage":     {t: t, a: f.a, token: "x"},
	} {
		res := c.get(me)
		wantStatus(t, name, res, http.StatusUnauthorized)
		if e := decodeDiscord(t, res); e.Code != 0 || e.Message != "401: Unauthorized" {
			t.Fatalf("%s: %s", name, res.body)
		}
	}
	for _, scheme := range []string{"Bearer " + f.token.Secret, "Basic " + f.token.Secret, f.token.Secret, "Bot"} {
		wantStatus(t, scheme[:3], anon.req(http.MethodGet, me, nil, map[string]string{"Authorization": scheme}), http.StatusUnauthorized)
	}
	wantStatus(t, "lowercase scheme", anon.req(http.MethodGet, me, nil, map[string]string{"Authorization": "bot " + f.token.Secret}), http.StatusOK)

	// A token is no session: the cookie routes and the admin routes refuse it.
	for _, p := range []string{"/api/channels", "/api/members", "/api/admin/bots", "/api/auth/me"} {
		wantStatus(t, p, f.client.get(p), http.StatusUnauthorized)
	}
	// And a session cookie is no bot token.
	cookieOnly := &botClient{t: t, a: f.a}
	var cookies []string
	for _, c := range f.owner.http.Jar.Cookies(mustURL(f.a.srv.URL)) {
		cookies = append(cookies, c.String())
	}
	wantStatus(t, "cookie", cookieOnly.req(http.MethodGet, me, nil, map[string]string{"Cookie": strings.Join(cookies, "; ")}), http.StatusUnauthorized)

	// Unknown routes answer in the bot shape.
	res = f.client.get(bot.BasePath + "/nope")
	wantStatus(t, "unknown route", res, http.StatusNotFound)
	if e := decodeDiscord(t, res); e.Message != "404: Not Found" {
		t.Fatalf("404: %s", res.body)
	}
	wantStatus(t, "method", f.client.req(http.MethodDelete, me, nil, nil), http.StatusMethodNotAllowed)

	// last used is recorded.
	var used *time.Time
	if err := f.a.db.QueryRow(t.Context(), `SELECT last_used_at FROM bot_tokens WHERE id = $1`, f.token.ID).Scan(&used); err != nil || used == nil {
		t.Fatalf("last used: %v %v", used, err)
	}
	var active *time.Time
	if err := f.a.db.QueryRow(t.Context(), `SELECT last_active_at FROM bots WHERE id = $1`, f.bot.ID).Scan(&active); err != nil || active == nil {
		t.Fatalf("last active: %v %v", active, err)
	}
}

func TestBotAPIRevocationIsImmediate(t *testing.T) {
	f := newBotFixture(t, false)
	me := bot.BasePath + "/users/@me"
	wantStatus(t, "before", f.client.get(me), http.StatusOK)

	wantStatus(t, "revoke", f.owner.delete(f.botPath+"/tokens/"+f.token.ID.String()), http.StatusNoContent)
	wantStatus(t, "after revoke", f.client.get(me), http.StatusUnauthorized)

	// Expiry, disabling and deleting end access at once as well.
	exp := issueToken(t, f.owner, f.bot.ID, map[string]any{"expires_at": time.Now().Add(time.Hour).Format(time.RFC3339)})
	c := &botClient{t: t, a: f.a, token: exp.Secret}
	wantStatus(t, "expiring token", c.get(me), http.StatusOK)
	if _, err := f.a.db.Exec(t.Context(), `UPDATE bot_tokens SET expires_at = NOW() - INTERVAL '1 second' WHERE id = $1`, exp.ID); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, "expired", c.get(me), http.StatusUnauthorized)

	tok := issueToken(t, f.owner, f.bot.ID, map[string]any{})
	c = &botClient{t: t, a: f.a, token: tok.Secret}
	wantStatus(t, "fresh", c.get(me), http.StatusOK)
	wantStatus(t, "disable", f.owner.patch(f.botPath, map[string]any{"disabled": true}), http.StatusOK)
	wantStatus(t, "disabled", c.get(me), http.StatusUnauthorized)
	wantStatus(t, "enable", f.owner.patch(f.botPath, map[string]any{"disabled": false}), http.StatusOK)
	wantStatus(t, "enabled again", c.get(me), http.StatusOK)
	wantStatus(t, "delete", f.owner.delete(f.botPath), http.StatusNoContent)
	wantStatus(t, "deleted", c.get(me), http.StatusUnauthorized)
}

// A bot sees and does only what its roles and the overwrites allow.
func TestBotAPIScope(t *testing.T) {
	f := newBotFixture(t, false)
	secret := f.a.createChannel(f.owner, "staff-only", "text")
	staff := createRole(t, f.owner, "Staff", 0)
	wantStatus(t, "private", putOverwrites(f.owner, "channels", secret,
		overwrite("role", everyoneID(t, f.owner), 0, perm.ViewChannel), overwrite("role", staff, perm.ViewChannel, 0)), http.StatusNoContent)
	postMessage(t, f.owner, f.other, map[string]any{"content": "other channel"})
	postMessage(t, f.owner, secret, map[string]any{"content": "classified"})
	hidden := postMessage(t, f.owner, f.other, map[string]any{"content": "not for the bot"})

	// Only the allowed channel is listed.
	res := f.client.get(bot.BasePath + "/channels")
	wantStatus(t, "channels", res, http.StatusOK)
	var tree chat.ChannelHierarchy
	res.decode(t, &tree)
	var seen []uuid.UUID
	for _, c := range tree.Uncategorized {
		seen = append(seen, c.ID)
	}
	for _, cat := range tree.Categories {
		for _, c := range cat.Channels {
			seen = append(seen, c.ID)
		}
	}
	if len(seen) != 1 || seen[0] != f.open {
		t.Fatalf("channels: %s", res.body)
	}

	// Everything about the other channels looks like they do not exist.
	for _, ch := range []uuid.UUID{f.other, secret} {
		for name, r := range map[string]response{
			"read":  f.client.msgs(ch),
			"say":   f.client.say(ch, "hi"),
			"get":   f.client.get(botPath(ch, "/messages/"+hidden.String())),
			"react": f.client.put(botPath(ch, "/messages/"+hidden.String()+"/reactions/"+url.PathEscape("👍")+"/@me")),
			"edit":  f.client.patch(botPath(ch, "/messages/"+hidden.String()), map[string]any{"content": "x"}),
			"del":   f.client.delete(botPath(ch, "/messages/"+hidden.String())),
		} {
			if r.status != http.StatusNotFound {
				t.Fatalf("%s in a hidden channel: %d %s", name, r.status, r.body)
			}
		}
	}
	// A message of a hidden channel is not reachable through the allowed one either.
	wantStatus(t, "cross-channel get", f.client.get(botPath(f.open, "/messages/"+hidden.String())), http.StatusNotFound)

	// The bot has no admin power: member routes are closed without the intent.
	res = f.client.get(bot.BasePath + "/members")
	wantStatus(t, "members without intent", res, http.StatusForbidden)
	if e := decodeDiscord(t, res); e.Code != 50013 {
		t.Fatalf("members: %s", res.body)
	}
}

func TestBotAPIMessages(t *testing.T) {
	f := newBotFixture(t, false)
	f.a.router.Bots.SetLimits(1000, 1000)
	c := f.client

	res := c.post(botPath(f.open, "/messages"), map[string]any{
		"content": "build **passed**", "tts": false, "components": []any{},
		"embeds": []map[string]any{{"title": "Run 42", "color": 65280, "fields": []map[string]any{{"name": "tests", "value": "812", "inline": true}}}},
	})
	wantStatus(t, "post", res, http.StatusCreated)
	var m chat.Message
	res.decode(t, &m)
	if m.UserID != f.bot.UserID || !m.IsBot || m.DisplayName != "helper" || len(m.Embeds) != 1 || m.Embeds[0].Title != "Run 42" {
		t.Fatalf("message: %s", res.body)
	}

	// Validation in Discord's shape.
	for name, body := range map[string]map[string]any{
		"empty":      {"content": ""},
		"bad embeds": {"embeds": []map[string]any{{"url": "javascript:alert(1)"}}},
		"too long":   {"content": strings.Repeat("x", chat.MaxMessageLen+1)},
	} {
		res := c.post(botPath(f.open, "/messages"), body)
		wantStatus(t, name, res, http.StatusBadRequest)
		if e := decodeDiscord(t, res); e.Code != 50035 && e.Code != 50006 {
			t.Fatalf("%s: %s", name, res.body)
		}
	}
	wantStatus(t, "not json", c.req(http.MethodPost, botPath(f.open, "/messages"), nil, nil), http.StatusBadRequest)

	// Own messages: read, edit, delete. Others' cannot be edited or deleted without Manage messages.
	wantStatus(t, "get", c.get(botPath(f.open, "/messages/"+m.ID.String())), http.StatusOK)
	res = c.patch(botPath(f.open, "/messages/"+m.ID.String()), map[string]any{"content": "edited"})
	wantStatus(t, "edit own", res, http.StatusOK)
	var edited chat.Message
	res.decode(t, &edited)
	if edited.Content != "edited" || !edited.IsEdited {
		t.Fatalf("edited: %s", res.body)
	}
	member := postMessage(t, f.owner, f.open, map[string]any{"content": "a member's message"})
	wantStatus(t, "edit foreign", c.patch(botPath(f.open, "/messages/"+member.String()), map[string]any{"content": "x"}), http.StatusForbidden)
	wantStatus(t, "delete foreign", c.delete(botPath(f.open, "/messages/"+member.String())), http.StatusForbidden)
	res = c.msgs(f.open)
	var page []chat.Message
	res.decode(t, &page)
	if len(page) != 2 {
		t.Fatalf("history: %s", res.body)
	}

	// Replies and threads.
	res = c.post(botPath(f.open, "/messages"), map[string]any{"content": "reply", "message_reference": map[string]any{"message_id": member}})
	wantStatus(t, "reply", res, http.StatusCreated)
	var reply chat.Message
	res.decode(t, &reply)
	if reply.ReplyToID == nil || *reply.ReplyToID != member {
		t.Fatalf("reply: %s", res.body)
	}
	wantStatus(t, "thread", c.post(botPath(f.open, "/messages"), map[string]any{"content": "in thread", "parent_id": member}), http.StatusCreated)
	wantStatus(t, "reply to a message elsewhere", c.post(botPath(f.open, "/messages"), map[string]any{"content": "x", "message_reference": map[string]any{"message_id": uuid.New()}}), http.StatusBadRequest)

	wantStatus(t, "delete own", c.delete(botPath(f.open, "/messages/"+m.ID.String())), http.StatusNoContent)
	wantStatus(t, "gone", c.get(botPath(f.open, "/messages/"+m.ID.String())), http.StatusNotFound)
}

func TestBotAPIReactions(t *testing.T) {
	f := newBotFixture(t, false)
	msg := postMessage(t, f.owner, f.open, map[string]any{"content": "react to me"})
	path := botPath(f.open, "/messages/"+msg.String()+"/reactions/"+url.PathEscape("👍")+"/@me")

	wantStatus(t, "add", f.client.put(path), http.StatusNoContent)
	wantStatus(t, "add again", f.client.put(path), http.StatusNoContent)
	count := func() int {
		var m chat.Message
		f.owner.get("/api/channels/"+f.open.String()+"/messages").decode(t, &[]chat.Message{})
		res := f.client.get(botPath(f.open, "/messages/"+msg.String()))
		res.decode(t, &m)
		n := 0
		for _, r := range m.Reactions {
			n += r.Count
		}
		return n
	}
	if n := count(); n != 1 {
		t.Fatalf("reactions after adding twice: %d", n)
	}
	wantStatus(t, "remove", f.client.delete(path), http.StatusNoContent)
	wantStatus(t, "remove again", f.client.delete(path), http.StatusNoContent)
	if n := count(); n != 0 {
		t.Fatalf("reactions after removing: %d", n)
	}
	wantStatus(t, "bad emoji", f.client.put(botPath(f.open, "/messages/"+msg.String()+"/reactions/%20/@me")), http.StatusBadRequest)

	// Without Add reactions the bot cannot add, only take back.
	wantStatus(t, "role without reactions", f.owner.patch("/api/admin/roles/"+f.roleID.String(), map[string]any{"permissions": perm.SendMessages}), http.StatusOK)
	wantStatus(t, "forbidden", f.client.put(path), http.StatusForbidden)
}

func TestBotAPIUpload(t *testing.T) {
	f := newBotFixture(t, false)
	post := func(ch uuid.UUID, name, ctype string, data []byte) response {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		_ = mw.WriteField("content", "see attached")
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", `form-data; name="file"; filename="`+name+`"`)
		h.Set("Content-Type", ctype)
		part, _ := mw.CreatePart(h)
		_, _ = part.Write(data)
		_ = mw.Close()
		req, _ := http.NewRequest(http.MethodPost, f.a.srv.URL+botPath(ch, "/upload"), &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("Authorization", "Bot "+f.token.Secret)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		return response{status: res.StatusCode, body: raw}
	}
	res := post(f.open, "a.png", "image/png", pngBytes)
	wantStatus(t, "upload", res, http.StatusCreated)
	var m chat.Message
	res.decode(t, &m)
	if len(m.Attachments) != 1 || !m.IsBot || m.Content != "see attached" {
		t.Fatalf("upload: %s", res.body)
	}
	wantStatus(t, "hidden channel", post(f.other, "a.png", "image/png", pngBytes), http.StatusNotFound)
	res = post(f.open, "evil.exe", "application/x-msdownload", []byte("MZ\x90\x00 not allowed"))
	if res.status != http.StatusUnsupportedMediaType {
		t.Fatalf("type check: %d %s", res.status, res.body)
	}
	// Attach files is a permission like any other.
	wantStatus(t, "drop attach files", f.owner.patch("/api/admin/roles/"+f.roleID.String(), map[string]any{"permissions": perm.SendMessages}), http.StatusOK)
	wantStatus(t, "no attach files", post(f.open, "a.png", "image/png", pngBytes), http.StatusForbidden)
}

func TestBotAPIMembersNeedTheIntent(t *testing.T) {
	f := newBotFixture(t, false)
	max := f.a.register(f.owner, "max")
	stranger := f.a.register(f.owner, "stranger")
	// max can see the bot's channel; the private channel's only viewer stays out of the list.
	closed := f.a.createChannel(f.owner, "closed", "text")
	private := createRole(t, f.owner, "Private", 0)
	wantStatus(t, "private", putOverwrites(f.owner, "channels", closed,
		overwrite("role", everyoneID(t, f.owner), 0, perm.ViewChannel), overwrite("role", private, perm.ViewChannel, 0)), http.StatusNoContent)
	_ = max
	_ = stranger
	wantStatus(t, "without intent", f.client.get(bot.BasePath+"/members"), http.StatusForbidden)
	wantStatus(t, "grant intent", f.owner.patch(f.botPath, map[string]any{"allowed_intents": bot.IntentGuildMembers}), http.StatusOK)

	res := f.client.get(bot.BasePath + "/members")
	wantStatus(t, "members", res, http.StatusOK)
	var list []chat.BotMember
	res.decode(t, &list)
	names := map[string]bool{}
	for _, m := range list {
		names[m.Username] = true
		if m.ID == f.bot.UserID && !m.Bot {
			t.Fatal("a bot is a member, flagged as one")
		}
	}
	if !names["Admin"] || !names["max"] || !names["stranger"] || len(list) != 4 {
		t.Fatalf("members: %s", res.body)
	}
	wantStatus(t, "one member", f.client.get(bot.BasePath+"/members/"+max.user.ID.String()), http.StatusOK)
	wantStatus(t, "unknown member", f.client.get(bot.BasePath+"/members/"+uuid.NewString()), http.StatusNotFound)
	wantStatus(t, "bad id", f.client.get(bot.BasePath+"/members/x"), http.StatusBadRequest)
}

func TestBotAPIRateLimits(t *testing.T) {
	f := newBotFixture(t, false)
	f.a.router.Bots.SetLimits(1000, 3)
	for i := 0; i < 3; i++ {
		wantStatus(t, "send", f.client.say(f.open, "hi"), http.StatusCreated)
	}
	res := f.client.say(f.open, "too many")
	wantStatus(t, "send limit", res, http.StatusTooManyRequests)
	e := decodeDiscord(t, res)
	var rl struct {
		Global bool `json:"global"`
	}
	res.decode(t, &rl)
	if e.RetryAfter <= 0 || e.Message != "You are being rate limited." || rl.Global || res.header.Get("Retry-After") == "" {
		t.Fatalf("429: %s %v", res.body, res.header)
	}
	// Other channels have their own budget; reads are not limited by the send bucket.
	wantStatus(t, "reads", f.client.msgs(f.open), http.StatusOK)

	// A second bot has separate buckets.
	other := createBot(t, f.owner, "second")
	tok := issueToken(t, f.owner, other.ID, map[string]any{})
	second := &botClient{t: t, a: f.a, token: tok.Secret}
	wantStatus(t, "second bot unaffected", second.get(bot.BasePath+"/users/@me"), http.StatusOK)

	f.a.router.Bots.SetLimits(2, 100)
	f2 := &botClient{t: t, a: f.a, token: tok.Secret}
	var limited bool
	for i := 0; i < 5; i++ {
		if r := f2.get(bot.BasePath + "/users/@me"); r.status == http.StatusTooManyRequests {
			limited = true
			var g struct {
				Global bool `json:"global"`
			}
			r.decode(t, &g)
			if !g.Global {
				t.Fatalf("the overall limit is global: %s", r.body)
			}
		}
	}
	if !limited {
		t.Fatal("the per-bot global limit never applied")
	}
}

// Wrong tokens are counted per client address.
func TestBotAPIFailedTokensAreLimited(t *testing.T) {
	f := newBotFixture(t, false)
	anon := &botClient{t: t, a: f.a, token: bot.TokenPrefix + strings.Repeat("B", 43)}
	var got429 bool
	for i := 0; i < 40; i++ {
		if anon.get(bot.BasePath+"/users/@me").status == http.StatusTooManyRequests {
			got429 = true
			break
		}
	}
	if !got429 {
		t.Fatal("guessing was never slowed down")
	}
}

// Tokens live in the Authorization header: they must not reach the logs, in
// requests, in errors or in management responses that are logged.
func TestBotTokensNeverReachTheLogs(t *testing.T) {
	var logs lockedBuffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	f := newBotFixture(t, false)
	f.client.get(bot.BasePath + "/users/@me")
	f.client.say(f.open, "hello")
	f.client.say(f.other, "hello")
	f.client.get(bot.BasePath + "/nope")
	wrong := &botClient{t: t, a: f.a, token: bot.TokenPrefix + strings.Repeat("C", 43)}
	wrong.get(bot.BasePath + "/users/@me")
	regen := f.owner.post(f.botPath+"/tokens/"+f.token.ID.String()+"/regenerate", nil)
	var fresh bot.NewTokenResponse
	regen.decode(t, &fresh)
	(&botClient{t: t, a: f.a, token: fresh.Secret}).get(bot.BasePath + "/users/@me")

	out := logs.String()
	for _, secret := range []string{f.token.Secret, fresh.Secret, wrong.token, strings.TrimPrefix(f.token.Secret, bot.TokenPrefix)} {
		if strings.Contains(out, secret) {
			t.Fatalf("a token reached the logs: %q", secret)
		}
	}
	if !strings.Contains(out, "/api/bot/v1") {
		t.Fatal("the test saw no bot requests in the log")
	}
}

func TestBotAPIRefusals(t *testing.T) {
	f := newBotFixture(t, false)
	f.a.router.Bots.SetLimits(1000, 1000)
	c := f.client

	wantStatus(t, "bad channel id", c.post(bot.BasePath+"/channels/x/messages", map[string]any{"content": "x"}), http.StatusBadRequest)
	big := map[string]any{"content": strings.Repeat("x", 2<<20)}
	res := c.post(botPath(f.open, "/messages"), big)
	wantStatus(t, "oversized body", res, http.StatusRequestEntityTooLarge)
	if e := decodeDiscord(t, res); e.Code != 40005 {
		t.Fatalf("oversized: %s", res.body)
	}
	// Without a declared length the limit is enforced while reading.
	req, _ := http.NewRequest(http.MethodPost, f.a.srv.URL+botPath(f.open, "/messages"), io.NopCloser(strings.NewReader(`{"content":"`+strings.Repeat("x", 2<<20)+`"}`)))
	req.Header.Set("Authorization", "Bot "+f.token.Secret)
	req.Header.Set("Content-Type", "application/json")
	chunked, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(chunked.Body)
	chunked.Body.Close()
	if chunked.StatusCode != http.StatusRequestEntityTooLarge || !strings.Contains(string(raw), "40005") {
		t.Fatalf("chunked oversized body: %d %s", chunked.StatusCode, raw)
	}
	wantStatus(t, "garbage body", c.req(http.MethodPost, botPath(f.open, "/messages"), nil, map[string]string{"Content-Type": "application/json"}), http.StatusBadRequest)
	// Reactions with an emoji written in the path in different ways.
	msg := postMessage(t, f.owner, f.open, map[string]any{"content": "react"})
	for _, emoji := range []string{"%F0%9F%91%8D", "thumbs", "a%2Fb", "50%25"} {
		wantStatus(t, "emoji "+emoji, c.put(botPath(f.open, "/messages/"+msg.String()+"/reactions/"+emoji+"/@me")), http.StatusNoContent)
	}
}
