//go:build integration

package server

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

// A member who holds only the built-in Moderator or Admin role reads the report
// queue and kicks a member below them; a custom role with the same moderation
// bits but without Manage reports and Kick members cannot do either.
func TestBuiltInModeratorAndAdminHandleReportsAndKick(t *testing.T) {
	f := newBuiltinsFixture(t)
	seedBuiltInRoles(t, f)
	custom := f.a.register(f.owner, "custom")
	assignRoles(t, f.owner, custom, createRole(t, f.owner, "almost mods", perm.ManageMessages|perm.TimeoutMembers|perm.DisconnectMembers))
	wantStatus(t, "custom role list", custom.get("/api/admin/reports"), http.StatusForbidden)
	// The command is not even offered to a role without Kick members.
	wantStatus(t, "custom role kick", f.run(custom, "kick", map[string]any{"member": f.member.user.ID}), http.StatusNotFound)

	for _, name := range []string{"Moderator", "Admin"} {
		holder := f.a.register(f.owner, "holder_"+strings.ToLower(name))
		assignRoles(t, f.owner, holder, f.roleByName(t, name))
		wantStatus(t, name+" lists reports", holder.get("/api/admin/reports"), http.StatusOK)
		msg := f.a.send(f.member, f.text, "rude "+name)
		res := f.senior.post("/api/messages/"+msg.String()+"/report", map[string]any{"category": "harassment", "note": "no"})
		wantStatus(t, "report", res, http.StatusCreated)
		var rep struct{ ID uuid.UUID }
		res.decode(t, &rep)
		wantStatus(t, name+" resolves a report", holder.post("/api/admin/reports/"+rep.ID.String()+"/resolve", map[string]any{"status": "resolved"}), http.StatusOK)
		victim := f.a.register(f.owner, "victim_"+strings.ToLower(name))
		wantStatus(t, name+" kicks a member", f.run(holder, "kick", map[string]any{"member": victim.user.ID}), http.StatusOK)
	}
}

// seedBuiltInRoles puts the two roles back that the test reset removes: the
// seed of migration 0034 and the grant of 0097, as a real database has them.
func seedBuiltInRoles(t *testing.T, f *builtinsFixture) {
	t.Helper()
	for _, file := range []string{"0034_starter_roles.sql", "0097_starter_roles_reports_kick.sql"} {
		sql, err := os.ReadFile("../db/migrations/" + file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.a.db.Exec(context.Background(), string(sql)); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
	}
}
