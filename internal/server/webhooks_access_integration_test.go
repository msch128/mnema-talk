//go:build integration

package server

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"

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

// A webhook posts with the creator's rights, checked again at every post.
func TestWebhookFollowsItsCreatorsRights(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "alerts", "text")
	a.router.Webhooks.SetLimits(1000, 1000)

	// Manage webhooks alone is not enough to create one: it posts messages.
	ro := createRole(t, admin, "hooks-only", perm.ManageWebhooks)
	mod := a.register(admin, "mod")
	assignRoles(t, admin, mod, ro)
	everyone := everyoneID(t, admin)
	deny := func(p perm.Set) {
		t.Helper()
		if res := putOverwrites(admin, "channels", ch, overwrite("role", everyone, 0, p)); res.status != http.StatusNoContent {
			t.Fatalf("overwrite: %d %s", res.status, res.body)
		}
	}
	deny(perm.SendMessages)
	list := "/api/admin/channels/" + ch.String() + "/webhooks"
	if res := mod.post(list, map[string]any{"name": "x"}); res.status != http.StatusForbidden {
		t.Fatalf("create without Send messages: %d %s", res.status, res.body)
	}
	// ... but the Owner can, whatever the channel says.
	owners := makeHook(t, admin, ch, "owner")
	if res := a.exec(owners.path, map[string]any{"content": "ok"}, nil); res.status != 204 {
		t.Fatalf("owner's webhook: %d %s", res.status, res.body)
	}

	deny(0)
	h := makeHook(t, mod, ch, "modbot")
	if res := a.exec(h.path, map[string]any{"content": "ok"}, nil); res.status != 204 {
		t.Fatalf("allowed: %d %s", res.status, res.body)
	}
	// The creator loses Send messages: the webhook stops.
	deny(perm.SendMessages)
	res := a.exec(h.path, map[string]any{"content": "no"}, nil)
	if res.status != http.StatusForbidden || !strings.Contains(string(res.body), "50013") {
		t.Fatalf("lost Send messages: %d %s", res.status, res.body)
	}
	// Files need Attach files.
	deny(perm.AttachFiles)
	body, ct := multipartBody(t, map[string]any{"content": "chart"}, map[string][]byte{"c.png": png1x1})
	if res := a.exec(h.path, body, map[string]string{"Content-Type": ct}); res.status != http.StatusForbidden {
		t.Fatalf("file without Attach files: %d %s", res.status, res.body)
	}
	if res := a.exec(h.path, map[string]any{"content": "text only"}, nil); res.status != 204 {
		t.Fatalf("text without Attach files: %d %s", res.status, res.body)
	}
	// A disabled creator account stops it too.
	if res := admin.post("/api/admin/users/"+mod.user.ID.String()+"/disable", nil); res.status >= 300 {
		t.Fatalf("disable: %d %s", res.status, res.body)
	}
	if res := a.exec(h.path, map[string]any{"content": "no"}, nil); res.status != http.StatusForbidden {
		t.Fatalf("disabled creator: %d %s", res.status, res.body)
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
