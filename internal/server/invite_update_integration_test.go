//go:build integration

package server

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/perm"
)

func createInvite(t *testing.T, c *client, body map[string]any) auth.Invite {
	t.Helper()
	res := c.post("/api/admin/invites", body)
	if res.status != http.StatusCreated {
		t.Fatalf("create invite: %d %s", res.status, res.body)
	}
	var inv auth.Invite
	res.decode(t, &inv)
	return inv
}

func TestInviteUpdateChangesAndRemovesLimits(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	inv := createInvite(t, admin, map[string]any{"max_uses": 5, "expires_in_hours": 2})
	path := "/api/admin/invites/" + inv.ID.String()

	// One use is consumed, so the limit has a floor.
	reg := a.anon().post("/api/auth/register", map[string]string{"username": "first", "password": "member-password-123", "invite_code": inv.Code})
	if reg.status != http.StatusCreated {
		t.Fatalf("register: %d %s", reg.status, reg.body)
	}

	later := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Second)
	res := admin.patch(path, map[string]any{"expires_at": later, "max_uses": 20})
	if res.status != http.StatusOK {
		t.Fatalf("update: %d %s", res.status, res.body)
	}
	var got auth.Invite
	res.decode(t, &got)
	if got.MaxUses == nil || *got.MaxUses != 20 || got.ExpiresAt == nil || !got.ExpiresAt.Equal(later) || got.UsesCount != 1 || got.Code != inv.Code {
		t.Fatalf("after update: %+v", got)
	}

	// A field that is absent is kept.
	res = admin.patch(path, map[string]any{"max_uses": 7})
	res.decode(t, &got)
	if res.status != http.StatusOK || got.ExpiresAt == nil || !got.ExpiresAt.Equal(later) || *got.MaxUses != 7 {
		t.Fatalf("partial update: %d %+v", res.status, got)
	}

	// null (and 0 for the limit) removes the limits.
	res = admin.patch(path, map[string]any{"expires_at": nil, "max_uses": nil})
	got = auth.Invite{}
	res.decode(t, &got)
	if res.status != http.StatusOK || got.ExpiresAt != nil || got.MaxUses != nil {
		t.Fatalf("null update: %d %+v", res.status, got)
	}
	admin.patch(path, map[string]any{"max_uses": 3})
	res = admin.patch(path, map[string]any{"max_uses": 0})
	got = auth.Invite{}
	res.decode(t, &got)
	if res.status != http.StatusOK || got.MaxUses != nil {
		t.Fatalf("zero update: %d %+v", res.status, got)
	}

	// The list shows what was stored.
	var list []auth.Invite
	admin.get("/api/admin/invites").decode(t, &list)
	if len(list) != 1 || list[0].MaxUses != nil || list[0].ExpiresAt != nil {
		t.Fatalf("list: %+v", list)
	}
}

func TestInviteUpdateReopensAnExhaustedInvite(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	inv := createInvite(t, admin, map[string]any{"max_uses": 1})
	register := func(name string) int {
		return a.anon().post("/api/auth/register", map[string]string{"username": name, "password": "member-password-123", "invite_code": inv.Code}).status
	}
	if got := register("one"); got != http.StatusCreated {
		t.Fatalf("first: %d", got)
	}
	if got := register("two"); got == http.StatusCreated {
		t.Fatal("a used up invite registered a second account")
	}
	if res := admin.patch("/api/admin/invites/"+inv.ID.String(), map[string]any{"max_uses": 2}); res.status != http.StatusOK {
		t.Fatalf("raise limit: %d %s", res.status, res.body)
	}
	if got := register("two"); got != http.StatusCreated {
		t.Fatalf("after raising the limit: %d", got)
	}
}

func TestInviteUpdateValidation(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	inv := createInvite(t, admin, map[string]any{"max_uses": 5})
	path := "/api/admin/invites/" + inv.ID.String()
	for i, name := range []string{"u1", "u2", "u3"} {
		_ = i
		if res := a.anon().post("/api/auth/register", map[string]string{"username": name, "password": "member-password-123", "invite_code": inv.Code}); res.status != http.StatusCreated {
			t.Fatalf("register %s: %d %s", name, res.status, res.body)
		}
	}
	now := time.Now()
	bad := map[string]map[string]any{
		"empty body":          {},
		"past expiry":         {"expires_at": now.Add(-time.Minute)},
		"expiry too far":      {"expires_at": now.Add(9000 * time.Hour)},
		"expiry not a time":   {"expires_at": "tomorrow"},
		"below consumed uses": {"max_uses": 2},
		"negative":            {"max_uses": -1},
		"above the bound":     {"max_uses": 1001},
		"unknown field":       {"max_uses": 9, "code": "changed-code-1234"},
	}
	for name, body := range bad {
		if res := admin.patch(path, body); res.status != http.StatusBadRequest {
			t.Errorf("%s: %d %s, want 400", name, res.status, res.body)
		}
	}
	// Equal to the consumed uses is allowed; the invite is then used up.
	if res := admin.patch(path, map[string]any{"max_uses": 3}); res.status != http.StatusOK {
		t.Errorf("max_uses == uses: %d %s", res.status, res.body)
	}
	// Nothing above changed the invite.
	var list []auth.Invite
	admin.get("/api/admin/invites").decode(t, &list)
	if len(list) != 1 || list[0].Code != inv.Code || *list[0].MaxUses != 3 {
		t.Fatalf("list: %+v", list)
	}
	if res := admin.patch("/api/admin/invites/00000000-0000-4000-8000-000000000999", map[string]any{"max_uses": 4}); res.status != http.StatusNotFound {
		t.Errorf("unknown invite: %d", res.status)
	}
	if res := admin.patch("/api/admin/invites/not-a-uuid", map[string]any{"max_uses": 4}); res.status != http.StatusBadRequest {
		t.Errorf("bad id: %d", res.status)
	}
}

func TestInviteUpdateNeedsManageInvites(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	member := a.register(admin, "bob")
	inv := createInvite(t, admin, map[string]any{"max_uses": 5})
	path := "/api/admin/invites/" + inv.ID.String()
	if res := member.patch(path, map[string]any{"max_uses": 9}); res.status != http.StatusForbidden {
		t.Fatalf("plain member: %d %s", res.status, res.body)
	}
	if res := a.anon().patch(path, map[string]any{"max_uses": 9}); res.status != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", res.status)
	}
	role := createRole(t, admin, "inviter", perm.CreateInvites)
	assignRoles(t, admin, member, role)
	// Whoever manages invites edits any invite, not only their own.
	if res := member.patch(path, map[string]any{"max_uses": 9}); res.status != http.StatusOK {
		t.Fatalf("invite manager: %d %s", res.status, res.body)
	}
	var list []auth.Invite
	admin.get("/api/admin/invites").decode(t, &list)
	for _, i := range list {
		if i.ID == inv.ID && (i.MaxUses == nil || *i.MaxUses != 9) {
			t.Fatalf("not stored: %+v", i)
		}
	}
}

func TestInviteUpdateIsAudited(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	inv := createInvite(t, admin, map[string]any{"code": "audited-invite-k7x2", "max_uses": 5})
	path := "/api/admin/invites/" + inv.ID.String()
	if res := admin.patch(path, map[string]any{"max_uses": nil}); res.status != http.StatusOK {
		t.Fatalf("update: %d %s", res.status, res.body)
	}
	// A refused change leaves no entry.
	admin.patch(path, map[string]any{"expires_at": time.Now().Add(-time.Hour)})

	page := auditEntries(t, admin, "action=invite.update")
	if len(page.Entries) != 1 {
		t.Fatalf("entries: %+v", page.Entries)
	}
	e := page.Entries[0]
	if e.TargetType != "invite" || e.TargetID != inv.ID.String() || e.ActorID == nil || *e.ActorID != admin.user.ID {
		t.Fatalf("entry %+v", e)
	}
	before, _ := e.Details["before"].(map[string]any)
	if before["max_uses"] != float64(5) {
		t.Errorf("before = %v", e.Details["before"])
	}
	if s := string(mustJSON(e)); strings.Contains(s, "audited-invite-k7x2") {
		t.Error("the invite code reached the audit log")
	}
}
