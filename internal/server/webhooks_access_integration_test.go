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

// A webhook belongs to its channel, like in Discord: it posts as itself, whoever
// made it. Only @all and @here depend on the maker, through a flag stored when
// they made or last changed it.
func TestWebhookBelongsToItsChannel(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "alerts", "text")
	a.router.Webhooks.SetLimits(1000, 1000)

	// Manage webhooks and View channel are all it takes to make one.
	role := createRole(t, admin, "hooks", perm.ManageWebhooks)
	mod := a.register(admin, "mod")
	assignRoles(t, admin, mod, role)
	everyone := everyoneID(t, admin)
	if res := putOverwrites(admin, "channels", ch, overwrite("role", everyone, 0, perm.SendMessages|perm.AttachFiles|perm.MentionEveryone)); res.status != 204 {
		t.Fatalf("overwrite: %d %s", res.status, res.body)
	}
	h := makeHook(t, mod, ch, "modbot")
	// It posts although its maker may not post here, files included.
	if res := a.exec(h.path, map[string]any{"content": "ok"}, nil); res.status != http.StatusNoContent {
		t.Fatalf("post: %d %s", res.status, res.body)
	}
	body, ct := multipartBody(t, map[string]any{"content": "chart"}, map[string][]byte{"c.png": png1x1})
	if res := a.exec(h.path, body, map[string]string{"Content-Type": ct}); res.status != http.StatusNoContent {
		t.Fatalf("file post: %d %s", res.status, res.body)
	}
	// Nor does it matter if the maker is disabled or loses the role.
	assignRoles(t, admin, mod)
	if res := admin.post("/api/admin/users/"+mod.user.ID.String()+"/disable", nil); res.status >= 300 {
		t.Fatalf("disable: %d", res.status)
	}
	if res := a.exec(h.path, map[string]any{"content": "still ok"}, nil); res.status != http.StatusNoContent {
		t.Fatalf("post after the maker is gone: %d %s", res.status, res.body)
	}
	// But it cannot notify everyone: its maker could not.
	res := a.exec(h.path+"?wait=true", map[string]any{"content": "@all", "allowed_mentions": map[string]any{"parse": []string{"everyone"}}}, nil)
	var m chat.Message
	res.decode(t, &m)
	if res.status != 200 || len(m.Mentions) != 0 {
		t.Fatalf("mass mention by a webhook whose maker could not: %d %s", res.status, res.body)
	}
	// When somebody who may changes it, it can.
	if r := admin.patch("/api/admin/webhooks/"+h.ID.String(), map[string]any{"name": "by owner"}); r.status != 200 {
		t.Fatalf("rename: %d", r.status)
	}
	res = a.exec(h.path+"?wait=true", map[string]any{"content": "@all", "allowed_mentions": map[string]any{"parse": []string{"everyone"}}}, nil)
	res.decode(t, &m)
	if res.status != 200 || len(m.Mentions) == 0 {
		t.Fatalf("mass mention after the Owner changed it: %d %s", res.status, res.body)
	}
	// And without "everyone" in allowed_mentions it still does not.
	res = a.exec(h.path+"?wait=true", map[string]any{"content": "@all"}, nil)
	var plain chat.Message
	res.decode(t, &plain)
	if len(plain.Mentions) != 0 {
		t.Fatalf("@all by default: %v", plain.Mentions)
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
