//go:build integration

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/perm"
)

func auditEntries(t *testing.T, c *client, query string) audit.Page {
	t.Helper()
	res := c.get("/api/admin/audit-log?" + query)
	if res.status != http.StatusOK {
		t.Fatalf("audit log: %d %s", res.status, res.body)
	}
	var page audit.Page
	res.decode(t, &page)
	return page
}

func TestAuditLogRecordsAdminActionsWithoutSecrets(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()

	role := createRole(t, admin, "Mods", perm.SendMessages)
	if res := admin.patch("/api/admin/roles/"+role.String(), map[string]any{"name": "Moderators", "color": 255}); res.status != http.StatusOK {
		t.Fatalf("patch role: %d %s", res.status, res.body)
	}
	inv := admin.post("/api/admin/invites", map[string]any{"code": "super-secret-invite"})
	if inv.status != http.StatusCreated {
		t.Fatal(inv.status, inv.body)
	}
	ch := a.createChannel(admin, "general", "text")
	hook := admin.post("/api/admin/channels/"+ch.String()+"/webhooks", map[string]any{"name": "ci"})
	if hook.status != http.StatusCreated {
		t.Fatalf("webhook: %d %s", hook.status, hook.body)
	}
	out := admin.post("/api/admin/channels/"+ch.String()+"/outgoing-webhooks", map[string]any{
		"name": "out", "url": "https://example.com/hook?key=SECRETKEY", "events": []string{"message.created"},
	})
	member := a.register(admin, "alice")
	if res := admin.post("/api/admin/users/"+member.user.ID.String()+"/disable", nil); res.status != http.StatusNoContent {
		t.Fatalf("disable: %d %s", res.status, res.body)
	}
	// A failed request leaves nothing behind.
	admin.post("/api/admin/users/"+uuid.NewString()+"/enable", nil)

	page := auditEntries(t, admin, "limit=200")
	byAction := map[string]audit.Item{}
	var all strings.Builder
	for _, e := range page.Entries {
		byAction[e.Action] = e
		all.WriteString(string(mustJSON(e)))
	}
	for _, want := range []string{"role.create", "role.update", "invite.create", "channel.create", "webhook.create", "user.disable"} {
		if _, ok := byAction[want]; !ok {
			t.Errorf("no %s entry; have %v", want, keys(byAction))
		}
	}
	if out.status == http.StatusCreated {
		if _, ok := byAction["outgoing_webhook.create"]; !ok {
			t.Error("no outgoing webhook entry")
		}
	}
	upd := byAction["role.update"]
	if upd.ActorName != "cool_guy" || upd.ActorID == nil || upd.TargetID != role.String() || upd.RequestID == "" {
		t.Errorf("update entry %+v", upd)
	}
	changes, _ := upd.Details["changes"].(map[string]any)
	if _, ok := changes["name"]; !ok {
		t.Errorf("no name change in %v", upd.Details)
	}
	if before, _ := upd.Details["before"].(map[string]any); before["name"] != "Mods" {
		t.Errorf("before = %v", upd.Details["before"])
	}
	if d := byAction["user.disable"]; d.TargetName != "alice" {
		t.Errorf("disable target %q", d.TargetName)
	}
	for _, secret := range []string{"super-secret-invite", "SECRETKEY"} {
		if strings.Contains(all.String(), secret) {
			t.Errorf("secret %q reached the audit log", secret)
		}
	}
	if strings.Contains(all.String(), `"user.enable"`) {
		t.Error("a failed request was audited")
	}
	if strings.Contains(all.String(), "token") && !strings.Contains(all.String(), "[redacted]") {
		t.Error("token field neither absent nor redacted")
	}
}

func TestAuditLogFiltersAndPages(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	for i := 0; i < 5; i++ {
		createRole(t, admin, "r"+string(rune('a'+i)), 0)
	}
	first := auditEntries(t, admin, "action=role&limit=2")
	if len(first.Entries) != 2 || first.NextBefore == nil {
		t.Fatalf("first page %+v", first)
	}
	second := auditEntries(t, admin, "action=role&limit=2&before="+itoa(*first.NextBefore))
	if len(second.Entries) != 2 || second.Entries[0].ID >= first.Entries[1].ID {
		t.Fatalf("second page %+v", second)
	}
	if got := auditEntries(t, admin, "action=role.create&actor="+admin.user.ID.String()+"&target_type=role"); len(got.Entries) != 5 {
		t.Fatalf("filtered %d", len(got.Entries))
	}
	if got := auditEntries(t, admin, "action=invite"); len(got.Entries) != 0 {
		t.Fatalf("action prefix leaked %d", len(got.Entries))
	}
	if got := auditEntries(t, admin, "from=2999-01-01"); len(got.Entries) != 0 {
		t.Fatal("from filter")
	}
	if got := auditEntries(t, admin, "to=2000-01-01"); len(got.Entries) != 0 {
		t.Fatal("to filter")
	}
	for _, bad := range []string{"actor=x", "from=yesterday", "to=x", "before=0", "before=x", "action=" + strings.Repeat("a", 70)} {
		if res := admin.get("/api/admin/audit-log?" + bad); res.status != http.StatusBadRequest {
			t.Errorf("%s: %d", bad, res.status)
		}
	}
}

func TestAuditLogNeedsItsPermission(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	member := a.register(admin, "bob")
	if res := member.get("/api/admin/audit-log"); res.status != http.StatusForbidden {
		t.Fatalf("plain member: %d", res.status)
	}
	// An admin-area permission alone does not open the log.
	inv := createRole(t, admin, "inviter", perm.CreateInvites)
	assignRoles(t, admin, member, inv)
	if res := member.get("/api/admin/audit-log"); res.status != http.StatusForbidden {
		t.Fatalf("invite manager: %d %s", res.status, res.body)
	}
	viewer := createRole(t, admin, "auditor", perm.ViewAuditLog)
	assignRoles(t, admin, member, viewer)
	if res := member.get("/api/admin/audit-log"); res.status != http.StatusOK {
		t.Fatalf("auditor: %d %s", res.status, res.body)
	}
	// Reading is not an action.
	if got := auditEntries(t, admin, "action=audit"); len(got.Entries) != 0 {
		t.Fatal("reads were audited")
	}
}

func TestAuditLogIsImmutable(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	createRole(t, admin, "x", 0)
	ctx := context.Background()
	if _, err := a.db.Exec(ctx, `UPDATE audit_log SET action = 'forged'`); err == nil {
		t.Fatal("update succeeded")
	}
}

func TestAuditRetentionPrunes(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	createRole(t, admin, "x", 0)
	ctx := context.Background()
	l := &audit.Log{DB: a.db}
	if n, _ := l.Prune(ctx, 0); n != 0 {
		t.Fatal("days=0 must keep everything")
	}
	if _, err := a.db.Exec(ctx, `INSERT INTO audit_log (created_at, action) VALUES (NOW() - INTERVAL '400 days', 'old.thing')`); err != nil {
		t.Fatal(err)
	}
	if n, err := l.Prune(ctx, 30); err != nil || n != 1 {
		t.Fatalf("pruned %d, %v", n, err)
	}
	if got := auditEntries(t, admin, "action=role"); len(got.Entries) != 1 {
		t.Fatal("recent entry removed")
	}
}

// Every mutating admin route must be named in audit.Routes, otherwise it is
// only logged under a generic action.
func TestAuditCoversEveryMutatingAdminRoute(t *testing.T) {
	a := newApp(t, false)
	mux, ok := a.router.Handler.(chi.Routes)
	if !ok {
		t.Fatal("router is not a chi mux")
	}
	seen := 0
	err := chi.Walk(mux, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if !strings.HasPrefix(route, "/api/admin/") || method == http.MethodGet {
			return nil
		}
		seen++
		path := strings.ReplaceAll(strings.ReplaceAll(route, "{id}", uuid.NewString()), "/*", "")
		if _, _, found := audit.Match(method, path); !found {
			t.Errorf("%s %s is not in audit.Routes", method, route)
		}
		return nil
	})
	if err != nil || seen < 30 {
		t.Fatalf("walked %d routes: %v", seen, err)
	}
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func keys(m map[string]audit.Item) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
