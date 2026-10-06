//go:build integration

package server

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
)

func TestAdminInviteValidation(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()

	cases := []struct {
		name string
		body map[string]any
		want int
	}{
		{"code too short", map[string]any{"code": "abc"}, http.StatusBadRequest},
		{"code with spaces", map[string]any{"code": "has spaces"}, http.StatusBadRequest},
		{"code too long", map[string]any{"code": strings.Repeat("a", 65)}, http.StatusBadRequest},
		{"max_uses zero", map[string]any{"max_uses": 0}, http.StatusBadRequest},
		{"max_uses too high", map[string]any{"max_uses": 1001}, http.StatusBadRequest},
		{"expiry zero", map[string]any{"expires_in_hours": 0}, http.StatusBadRequest},
		{"expiry over a year", map[string]any{"expires_in_hours": 24*365 + 1}, http.StatusBadRequest},
		{"unknown field", map[string]any{"uses": 3}, http.StatusBadRequest},
		{"custom code", map[string]any{"code": "Team_Night-2026", "max_uses": 1000, "expires_in_hours": 24 * 365}, http.StatusCreated},
		{"duplicate custom code", map[string]any{"code": "Team_Night-2026"}, http.StatusConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := admin.post("/api/admin/invites", tc.body)
			if res.status != tc.want {
				t.Fatalf("status %d, want %d: %s", res.status, tc.want, res.body)
			}
		})
	}
}

func TestAdminListsAndDeletesInvites(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()

	res := admin.post("/api/admin/invites", map[string]any{"max_uses": 5, "expires_in_hours": 2})
	if res.status != http.StatusCreated {
		t.Fatalf("create: %d %s", res.status, res.body)
	}
	var created auth.Invite
	res.decode(t, &created)
	if len(created.Code) != 12 || created.MaxUses == nil || *created.MaxUses != 5 || created.UsesCount != 0 {
		t.Fatalf("generated invite: %+v", created)
	}
	if created.ExpiresAt == nil || time.Until(*created.ExpiresAt) < time.Hour || time.Until(*created.ExpiresAt) > 2*time.Hour {
		t.Fatalf("expiry not about two hours ahead: %v", created.ExpiresAt)
	}
	if res := admin.post("/api/admin/invites", map[string]any{"code": "second-code"}); res.status != http.StatusCreated {
		t.Fatalf("second invite: %d %s", res.status, res.body)
	}

	var list []auth.Invite
	admin.get("/api/admin/invites").decode(t, &list)
	if len(list) != 2 || list[0].Code != "second-code" || list[1].ID != created.ID {
		t.Fatalf("list not newest first: %+v", list)
	}
	if list[0].MaxUses != nil || list[0].ExpiresAt != nil {
		t.Fatalf("unlimited invite has limits: %+v", list[0])
	}

	if res := admin.delete("/api/admin/invites/" + created.ID.String()); res.status != http.StatusNoContent {
		t.Fatalf("delete: %d %s", res.status, res.body)
	}
	if res := admin.delete("/api/admin/invites/" + created.ID.String()); res.status != http.StatusNotFound {
		t.Fatalf("delete twice: %d", res.status)
	}
	if res := admin.delete("/api/admin/invites/not-a-uuid"); res.status != http.StatusBadRequest {
		t.Fatalf("delete bad id: %d", res.status)
	}
	admin.get("/api/admin/invites").decode(t, &list)
	if len(list) != 1 {
		t.Fatalf("deleted invite still listed: %+v", list)
	}

	reg := a.anon().post("/api/auth/register", map[string]string{"username": "late", "password": "member-password-123", "invite_code": created.Code})
	if reg.status != http.StatusBadRequest {
		t.Fatalf("deleted invite still registers: %d %s", reg.status, reg.body)
	}

	member := a.register(admin, "max")
	if res := member.delete("/api/admin/invites/" + uuid.NewString()); res.status != http.StatusForbidden {
		t.Fatalf("member deleting invites: %d", res.status)
	}
}

func TestExpiredInviteCannotRegister(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()

	res := admin.post("/api/admin/invites", map[string]any{"code": "soon-gone", "expires_in_hours": 1})
	if res.status != http.StatusCreated {
		t.Fatalf("create: %d %s", res.status, res.body)
	}
	if _, err := a.db.Exec(context.Background(), `UPDATE invites SET expires_at = now() - interval '1 minute' WHERE code = 'soon-gone'`); err != nil {
		t.Fatal(err)
	}
	reg := a.anon().post("/api/auth/register", map[string]string{"username": "late", "password": "member-password-123", "invite_code": "soon-gone"})
	if reg.status != http.StatusBadRequest {
		t.Fatalf("expired invite registered: %d %s", reg.status, reg.body)
	}
	var list []auth.Invite
	admin.get("/api/admin/invites").decode(t, &list)
	if len(list) != 1 || list[0].UsesCount != 0 {
		t.Fatalf("expired invite was consumed: %+v", list)
	}
}
