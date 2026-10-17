//go:build integration

package server

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/webhook"
)

var png1x1 = []byte{
	0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0x0d, 'I', 'H', 'D', 'R', 0, 0, 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0,
	0x1f, 0x15, 0xc4, 0x89, 0, 0, 0, 0x0d, 'I', 'D', 'A', 'T', 0x78, 0x9c, 0x63, 0xf8, 0xcf, 0xc0, 0xf0, 0x1f, 0, 5, 0, 1, 0xff,
	0x89, 0x99, 0x3d, 0x1d, 0, 0, 0, 0, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82,
}

func multipartBody(t *testing.T, payload any, files map[string][]byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if payload != nil {
		raw, _ := json.Marshal(payload)
		if err := mw.WriteField("payload_json", string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	i := 0
	for name, data := range files {
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", `form-data; name="files[`+string(rune('0'+i))+`]"; filename="`+name+`"`)
		h.Set("Content-Type", "image/png")
		part, _ := mw.CreatePart(h)
		_, _ = part.Write(data)
		i++
	}
	_ = mw.Close()
	return &buf, mw.FormDataContentType()
}

func TestWebhookMultipartWithFiles(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "alerts", "text")
	h := makeHook(t, admin, ch, "bot")
	a.router.Webhooks.SetLimits(1000, 1000)

	body, ct := multipartBody(t, map[string]any{"content": "chart"}, map[string][]byte{"c.png": png1x1})
	res := a.exec(h.path+"?wait=true", body, map[string]string{"Content-Type": ct})
	if res.status != http.StatusOK {
		t.Fatalf("multipart: %d %s", res.status, res.body)
	}
	var msg chat.Message
	res.decode(t, &msg)
	if len(msg.Attachments) != 1 || msg.Attachments[0].MimeType != "image/png" || !msg.IsBot {
		t.Fatalf("message %+v", msg)
	}
	// Members can open it; it is in the channel's media.
	if got := admin.get(msg.Attachments[0].URL); got.status != 200 {
		t.Fatalf("attachment: %d", got.status)
	}

	// Form fields without payload_json, files only.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("content", "plain form")
	_ = mw.WriteField("username", "Formy")
	_ = mw.Close()
	res = a.exec(h.path+"?wait=true", &buf, map[string]string{"Content-Type": mw.FormDataContentType()})
	var m2 chat.Message
	res.decode(t, &m2)
	if res.status != 200 || m2.DisplayName != "Formy" || m2.Content != "plain form" {
		t.Fatalf("form fields: %d %s", res.status, res.body)
	}

	// A disguised file is refused and nothing is stored.
	before := a.store.Len()
	var bad bytes.Buffer
	bw := multipart.NewWriter(&bad)
	hd := textproto.MIMEHeader{}
	hd.Set("Content-Disposition", `form-data; name="files[0]"; filename="x.png"`)
	hd.Set("Content-Type", "image/png")
	part, _ := bw.CreatePart(hd)
	_, _ = part.Write([]byte("<html><script>alert(1)</script></html>"))
	_ = bw.Close()
	res = a.exec(h.path, &bad, map[string]string{"Content-Type": bw.FormDataContentType()})
	if res.status != http.StatusUnsupportedMediaType || a.store.Len() != before {
		t.Fatalf("disguised file: %d %s (objects %d -> %d)", res.status, res.body, before, a.store.Len())
	}

	// A bad payload after a good file discards the file.
	body, ct = multipartBody(t, nil, map[string][]byte{"c.png": png1x1})
	body.Reset()
	mw = multipart.NewWriter(body)
	hd = textproto.MIMEHeader{}
	hd.Set("Content-Disposition", `form-data; name="files[0]"; filename="c.png"`)
	hd.Set("Content-Type", "image/png")
	part, _ = mw.CreatePart(hd)
	_, _ = part.Write(png1x1)
	_ = mw.WriteField("payload_json", `{"content": 5}`)
	_ = mw.Close()
	res = a.exec(h.path, body, map[string]string{"Content-Type": mw.FormDataContentType()})
	if res.status != 400 || a.store.Len() != before {
		t.Fatalf("bad payload_json: %d %s (objects %d -> %d)", res.status, res.body, before, a.store.Len())
	}
}

func TestWebhookMentionsAndThreads(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	ch := a.createChannel(admin, "alerts", "text")
	h := makeHook(t, admin, ch, "bot")
	a.router.Webhooks.SetLimits(1000, 1000)

	mentions := func(body map[string]any) []uuid.UUID {
		t.Helper()
		res := a.exec(h.path+"?wait=true", body, nil)
		var m chat.Message
		res.decode(t, &m)
		if res.status != 200 {
			t.Fatalf("execute: %d %s", res.status, res.body)
		}
		return m.Mentions
	}
	// Default: named members are notified, @all is plain text.
	if got := mentions(map[string]any{"content": "hi @alice"}); len(got) != 1 || got[0] != alice.user.ID {
		t.Fatalf("named mention: %v", got)
	}
	if got := mentions(map[string]any{"content": "@all"}); len(got) != 0 {
		t.Fatalf("@all by default: %v", got)
	}
	// parse: [] means nobody; users list means exactly those.
	if got := mentions(map[string]any{"content": "@alice", "allowed_mentions": map[string]any{"parse": []string{}}}); len(got) != 0 {
		t.Fatalf("parse []: %v", got)
	}
	if got := mentions(map[string]any{"content": "@alice", "allowed_mentions": map[string]any{"users": []string{alice.user.ID.String(), "junk"}}}); len(got) != 1 {
		t.Fatalf("users list: %v", got)
	}
	// "everyone" works for a creator who may mention everyone (the Owner).
	if got := mentions(map[string]any{"content": "@all", "allowed_mentions": map[string]any{"parse": []string{"everyone"}}}); len(got) != 2 {
		t.Fatalf("@all allowed: %v", got)
	}
	// ... but not when the creator may not.
	mod := a.register(admin, "mod")
	role := createRole(t, admin, "hooks", perm.ManageWebhooks|perm.ViewChannel)
	assignRoles(t, admin, mod, role)
	ownerOnly := putOverwrites(admin, "channels", ch, overwrite("role", everyoneID(t, admin), 0, perm.MentionEveryone))
	if ownerOnly.status != http.StatusNoContent {
		t.Fatalf("overwrite: %d %s", ownerOnly.status, ownerOnly.body)
	}
	mh := makeHook(t, mod, ch, "modbot")
	res := a.exec(mh.path+"?wait=true", map[string]any{"content": "@all", "allowed_mentions": map[string]any{"parse": []string{"everyone"}}}, nil)
	var m chat.Message
	res.decode(t, &m)
	if res.status != 200 || len(m.Mentions) != 0 {
		t.Fatalf("creator without Mention everyone: %d %s", res.status, res.body)
	}

	// Threads: thread_id is a root message of the channel.
	root := a.send(admin, ch, "root")
	res = a.exec(h.path+"?wait=true&thread_id="+root.String(), map[string]any{"content": "in thread"}, nil)
	var reply chat.Message
	res.decode(t, &reply)
	if res.status != 200 || reply.ParentID == nil || *reply.ParentID != root {
		t.Fatalf("thread: %d %s", res.status, res.body)
	}
	other := a.createChannel(admin, "other", "text")
	foreign := a.send(admin, other, "elsewhere")
	for _, id := range []string{foreign.String(), "junk", uuid.NewString()} {
		res = a.exec(h.path+"?thread_id="+id, map[string]any{"content": "x"}, nil)
		var e webhook.DiscordError
		res.decode(t, &e)
		if res.status != 404 || e.Code != 10003 {
			t.Fatalf("thread %s: %d %s", id, res.status, res.body)
		}
	}
}

func TestWebhookRateLimits(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "alerts", "text")
	h := makeHook(t, admin, ch, "bot")
	h2 := makeHook(t, admin, ch, "bot2")

	var limited response
	for i := 0; i < 8; i++ {
		if res := a.exec(h.path, map[string]any{"content": "x"}, nil); res.status == 429 {
			limited = res
			break
		}
	}
	if limited.status != 429 {
		t.Fatal("per-webhook limit never hit")
	}
	var rl webhook.DiscordRateLimit
	limited.decode(t, &rl)
	if rl.RetryAfter <= 0 || rl.Global || limited.header.Get("Retry-After") == "" {
		t.Fatalf("429 body %s headers %v", limited.body, limited.header)
	}
	// Another webhook has its own budget ...
	if res := a.exec(h2.path, map[string]any{"content": "x"}, nil); res.status != 204 {
		t.Fatalf("second webhook: %d %s", res.status, res.body)
	}
	// ... but the channel's budget is shared.
	a.router.Webhooks.SetLimits(1000, 3)
	got429 := false
	for i := 0; i < 6 && !got429; i++ {
		got429 = a.exec(h2.path, map[string]any{"content": "x"}, nil).status == 429
	}
	if !got429 {
		t.Fatal("per-channel limit never hit")
	}
}

func TestWebhookBodyLimits(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "alerts", "text")
	h := makeHook(t, admin, ch, "bot")
	a.router.Webhooks.SetLimits(1000, 1000)

	huge := strings.NewReader(`{"content":"` + strings.Repeat("a", 2<<20) + `"}`)
	res := a.exec(h.path, huge, map[string]string{"Content-Type": "application/json"})
	var e webhook.DiscordError
	res.decode(t, &e)
	if res.status != 413 || e.Code != 40005 {
		t.Fatalf("huge json: %d %s", res.status, res.body)
	}
	// Too many files.
	files := map[string][]byte{}
	for i := 0; i < 11; i++ {
		files[strings.Repeat("f", i+1)+".png"] = png1x1
	}
	body, ct := multipartBody(t, nil, files)
	if res := a.exec(h.path, body, map[string]string{"Content-Type": ct}); res.status != 400 {
		t.Fatalf("11 files: %d %s", res.status, res.body)
	}
}

func TestWebhookManagementPermissions(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice") // no ManageWebhooks
	ch := a.createChannel(admin, "alerts", "text")
	list := "/api/admin/channels/" + ch.String() + "/webhooks"

	if res := alice.get(list); res.status != 403 {
		t.Fatalf("member list: %d", res.status)
	}
	if res := alice.post(list, map[string]any{"name": "x"}); res.status != 403 {
		t.Fatalf("member create: %d", res.status)
	}
	h := makeHook(t, admin, ch, "owner hook")

	// A role with Manage webhooks manages them in channels it can view.
	// New roles are created at the bottom: the one made first ranks higher.
	high := createRole(t, admin, "higher", perm.ManageWebhooks|perm.ManageRoles)
	hooks := createRole(t, admin, "hooks", perm.ManageWebhooks)
	bob := a.register(admin, "bob")
	carol := a.register(admin, "carol")
	assignRoles(t, admin, bob, hooks)
	assignRoles(t, admin, carol, high)

	var got []webhook.Webhook
	res := bob.get(list)
	res.decode(t, &got)
	if res.status != 200 || len(got) != 1 || got[0].Name != "owner hook" {
		t.Fatalf("bob list: %d %s", res.status, res.body)
	}
	if strings.Contains(string(res.body), h.Token) {
		t.Fatal("list leaks the token")
	}
	// A webhook belongs to its channel: whoever may manage the channel's
	// webhooks may manage it, whoever made it and whatever role they have.
	if res := bob.patch("/api/admin/webhooks/"+h.ID.String(), map[string]any{"name": "renamed by bob"}); res.status != 200 {
		t.Fatalf("member renames the Owner's webhook: %d %s", res.status, res.body)
	}
	if res := carol.post("/api/admin/webhooks/"+h.ID.String()+"/token", nil); res.status != 200 {
		t.Fatalf("member regenerates the Owner's webhook: %d %s", res.status, res.body)
	}
	ch2 := carol.post(list, map[string]any{"name": "carol hook"})
	if ch2.status != 201 {
		t.Fatalf("carol create: %d %s", ch2.status, ch2.body)
	}
	var carolHook webhook.CreatedWebhook
	ch2.decode(t, &carolHook)
	if res := bob.patch("/api/admin/webhooks/"+carolHook.ID.String(), map[string]any{"name": "x"}); res.status != 200 {
		t.Fatalf("lower role edits a higher member's hook: %d %s", res.status, res.body)
	}
	if res := bob.delete("/api/admin/webhooks/" + carolHook.ID.String()); res.status != 204 {
		t.Fatalf("lower role deletes a higher member's hook: %d", res.status)
	}
	if res := bob.delete("/api/admin/webhooks/" + h.ID.String()); res.status != 204 {
		t.Fatalf("member deletes the Owner's hook: %d", res.status)
	}
	// A channel the member cannot view does not exist for them.
	private := a.createChannel(admin, "private", "text")
	if res := putOverwrites(admin, "channels", private, overwrite("role", everyoneID(t, admin), 0, perm.ViewChannel)); res.status != 204 {
		t.Fatalf("private: %d %s", res.status, res.body)
	}
	ph := makeHook(t, admin, private, "hidden")
	if res := bob.get("/api/admin/channels/" + private.String() + "/webhooks"); res.status != 404 {
		t.Fatalf("hidden list: %d", res.status)
	}
	if res := bob.delete("/api/admin/webhooks/" + ph.ID.String()); res.status != 404 {
		t.Fatalf("hidden webhook: %d", res.status)
	}
	// Validation and limits.
	if res := admin.post(list, map[string]any{"name": "  "}); res.status != 400 {
		t.Fatalf("empty name: %d", res.status)
	}
	if res := admin.patch("/api/admin/webhooks/"+uuid.NewString(), map[string]any{"name": "x"}); res.status != 404 {
		t.Fatalf("unknown webhook: %d", res.status)
	}
	if res := admin.patch("/api/admin/webhooks/nope", map[string]any{"name": "x"}); res.status != 400 {
		t.Fatalf("bad id: %d", res.status)
	}
	for i := 0; i < 20; i++ {
		if res := admin.post("/api/admin/channels/"+private.String()+"/webhooks", map[string]any{"name": "n"}); res.status == 409 {
			return
		}
	}
	t.Fatal("no per-channel webhook cap")
}

func TestWebhookAvatarAndDeletionKeepHistory(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "alerts", "text")
	h := makeHook(t, admin, ch, "bot")
	a.router.Webhooks.SetLimits(1000, 1000)

	res := admin.upload("/api/admin/webhooks/"+h.ID.String()+"/avatar", "avatar", "a.png", "image/png", png1x1, nil)
	var w webhook.Webhook
	res.decode(t, &w)
	if res.status != 200 || w.AvatarURL == "" {
		t.Fatalf("avatar: %d %s", res.status, res.body)
	}
	// Replacing removes the previous object.
	objects := a.store.Len()
	if res := admin.upload("/api/admin/webhooks/"+h.ID.String()+"/avatar", "avatar", "b.png", "image/png", png1x1, nil); res.status != 200 || a.store.Len() != objects {
		t.Fatalf("replace avatar: %d %s", res.status, res.body)
	}
	if a.exec(h.path, map[string]any{"content": "kept"}, nil).status != 204 {
		t.Fatal("execute")
	}
	m := lastMessage(t, a, admin, ch)
	if m.AvatarURL == "" {
		t.Fatal("message lacks the webhook avatar")
	}

	if res := admin.delete("/api/admin/webhooks/" + h.ID.String()); res.status != 204 {
		t.Fatalf("delete: %d %s", res.status, res.body)
	}
	if res := a.exec(h.path, map[string]any{"content": "x"}, nil); res.status != 404 {
		t.Fatalf("deleted webhook: %d", res.status)
	}
	// The message stays, still marked as a bot's.
	if kept := lastMessage(t, a, admin, ch); kept.ID != m.ID || !kept.IsBot || kept.DisplayName != "bot" {
		t.Fatalf("history after delete: %+v", kept)
	}
	// A webhook that never posted leaves no bot account behind.
	h2 := makeHook(t, admin, ch, "unused")
	admin.upload("/api/admin/webhooks/"+h2.ID.String()+"/avatar", "avatar", "c.png", "image/png", png1x1, nil)
	objects = a.store.Len()
	admin.delete("/api/admin/webhooks/" + h2.ID.String())
	var bots int
	if err := a.db.QueryRow(t.Context(), `SELECT COUNT(*) FROM users WHERE is_bot`).Scan(&bots); err != nil || bots != 1 {
		t.Fatalf("bot accounts: %d %v", bots, err)
	}
	if a.store.Len() != objects-1 {
		t.Fatalf("avatar object not removed: %d -> %d", objects, a.store.Len())
	}
	// Deleting the channel removes the webhooks with it.
	h3 := makeHook(t, admin, ch, "gone")
	if res := admin.delete("/api/admin/channels/" + ch.String()); res.status != 204 {
		t.Fatalf("delete channel: %d %s", res.status, res.body)
	}
	if res := a.exec(h3.path, map[string]any{"content": "x"}, nil); res.status != 404 {
		t.Fatalf("webhook of deleted channel: %d", res.status)
	}
}
