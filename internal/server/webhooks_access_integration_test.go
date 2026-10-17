//go:build integration

package server

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/perm"
)

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// A webhook is an admin tool that then stands on its own: it posts with its
// settings and nothing else, whoever made it and whatever became of them.
func TestWebhookStandsOnItsOwn(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "alerts", "text")
	a.router.Webhooks.SetLimits(1000, 1000)

	role := createRole(t, admin, "admins", perm.Administrator)
	boss := a.register(admin, "boss")
	assignRoles(t, admin, boss, role)
	// Nobody needs Send messages, Attach files or View channel for it to post.
	everyone := everyoneID(t, admin)
	if res := putOverwrites(admin, "channels", ch, overwrite("role", everyone, 0, perm.SendMessages|perm.AttachFiles|perm.MentionEveryone|perm.ViewChannel)); res.status != 204 {
		t.Fatalf("overwrite: %d %s", res.status, res.body)
	}
	var h hook
	res := boss.post("/api/admin/channels/"+ch.String()+"/webhooks", map[string]any{"name": "bossbot", "allow_everyone": true})
	res.decode(t, &h.CreatedWebhook)
	h.path = "/api/webhooks/" + h.ID.String() + "/" + h.Token
	if res.status != http.StatusCreated {
		t.Fatalf("create: %d %s", res.status, res.body)
	}
	body, ct := multipartBody(t, map[string]any{"content": "chart", "allowed_mentions": map[string]any{"parse": []string{"everyone"}}}, map[string][]byte{"c.png": png1x1})
	var m chat.Message
	res = a.exec(h.path+"?wait=true", body, map[string]string{"Content-Type": ct})
	res.decode(t, &m)
	if res.status != http.StatusOK || len(m.Attachments) != 1 {
		t.Fatalf("post with a file: %d %s", res.status, res.body)
	}
	// The maker losing the role or being disabled changes nothing.
	assignRoles(t, admin, boss)
	if res := admin.post("/api/admin/users/"+boss.user.ID.String()+"/disable", nil); res.status >= 300 {
		t.Fatalf("disable: %d", res.status)
	}
	res = a.exec(h.path+"?wait=true", map[string]any{"content": "@all", "allowed_mentions": map[string]any{"parse": []string{"everyone"}}}, nil)
	res.decode(t, &m)
	if res.status != http.StatusOK || len(m.Mentions) == 0 {
		t.Fatalf("post after the maker is gone: %d %s", res.status, res.body)
	}
	// Whoever holds the URL gets exactly the webhook's settings: the maker's
	// own rights never entered into it. Turning the setting off turns it off.
	if r := admin.patch("/api/admin/webhooks/"+h.ID.String(), map[string]any{"allow_everyone": false}); r.status != http.StatusOK {
		t.Fatalf("patch: %d", r.status)
	}
	res = a.exec(h.path+"?wait=true", map[string]any{"content": "@all", "allowed_mentions": map[string]any{"parse": []string{"everyone"}}}, nil)
	res.decode(t, &m)
	if len(m.Mentions) != 0 {
		t.Fatalf("mass mention with the setting off: %v", m.Mentions)
	}
}

// The secret token of an execute URL never reaches the logs.
func TestWebhookTokenIsNotLogged(t *testing.T) {
	var logs lockedBuffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })

	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "alerts", "text")
	h := makeHook(t, admin, ch, "bot")
	a.router.Webhooks.SetLimits(1000, 1000)

	a.exec(h.path, map[string]any{"content": "ok"}, nil)
	a.exec(h.path, map[string]any{}, nil) // 400
	a.exec(h.path, "not json", map[string]string{"Content-Type": "application/json"})
	a.exec("/api/webhooks/"+h.ID.String()+"/wrong-secret-value", map[string]any{"content": "x"}, nil)
	if strings.Contains(logs.String(), h.Token) || strings.Contains(logs.String(), "wrong-secret-value") {
		t.Fatalf("token in logs:\n%s", logs.String())
	}
	if !strings.Contains(logs.String(), "/api/webhooks/"+h.ID.String()+"/[redacted]") {
		t.Fatalf("expected the redacted path in the access log:\n%s", logs.String())
	}
}
