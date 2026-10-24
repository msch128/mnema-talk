//go:build integration

package server

import (
	"net/http"
	"testing"
)

// The import routes admit the Owner and members with the Administrator
// permission, and nobody else; a rejected request leaves no audit entry.
func TestDiscordImportRoutesAreAdministratorOnly(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	manager := a.register(owner, "manager")
	admin := a.register(owner, "deputy")

	// Manage roles and manage channels do not open the import.
	var role struct{ ID string }
	owner.post("/api/admin/roles", map[string]any{"name": "Managers", "permissions": 1<<11 | 1<<12}).decode(t, &role)
	owner.put("/api/admin/users/"+manager.user.ID.String()+"/roles", map[string]any{"role_ids": []string{role.ID}})
	owner.post("/api/admin/roles", map[string]any{"name": "Deputies", "permissions": 1}).decode(t, &role)
	owner.put("/api/admin/users/"+admin.user.ID.String()+"/roles", map[string]any{"role_ids": []string{role.ID}})

	for _, route := range []struct{ method, path string }{
		{"POST", "/api/admin/import/discord/preview"},
		{"POST", "/api/admin/import/discord/apply"},
		{"GET", "/api/admin/import/discord/imports"},
		{"GET", "/api/admin/import/discord/imports/00000000-0000-4000-8000-000000000001"},
		{"POST", "/api/admin/import/discord/imports/00000000-0000-4000-8000-000000000001/undo"},
	} {
		if res := a.anon().do(route.method, route.path, map[string]any{}, nil); res.status != http.StatusUnauthorized {
			t.Errorf("anonymous %s %s: %d", route.method, route.path, res.status)
		}
		if res := manager.do(route.method, route.path, map[string]any{}, nil); res.status != http.StatusForbidden {
			t.Errorf("manager %s %s: %d", route.method, route.path, res.status)
		}
	}

	// An Administrator gets past the guard: a link that is no template link
	// is a plain 400 and never leaves the server (no outbound request).
	res := admin.post("/api/admin/import/discord/preview", map[string]any{"template": "https://evil.example/template/AbCdEf123456"})
	if res.status != http.StatusBadRequest {
		t.Fatalf("administrator, foreign host: %d %s", res.status, res.body)
	}
	if res := admin.get("/api/admin/import/discord/imports"); res.status != http.StatusOK {
		t.Fatalf("administrator lists imports: %d %s", res.status, res.body)
	}
	var n int
	if err := a.db.QueryRow(t.Context(), `SELECT COUNT(*) FROM audit_log WHERE action LIKE 'import.%' OR action LIKE 'admin.%'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("audit entries for previews and listings: %d, %v", n, err)
	}
	// The feature is announced for web clients.
	var health struct{ Features []string }
	a.anon().get("/api/health").decode(t, &health)
	found := false
	for _, f := range health.Features {
		found = found || f == "import.discord"
	}
	if !found {
		t.Fatalf("features = %v", health.Features)
	}
}
