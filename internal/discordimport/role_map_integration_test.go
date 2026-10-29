//go:build integration

package discordimport

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

// roleAt inserts an existing role at the given position.
func (w *world) roleAt(name string, position int, p perm.Set) uuid.UUID {
	w.t.Helper()
	var id uuid.UUID
	w.must(w.p.QueryRow(context.Background(), `INSERT INTO roles (name, position, permissions) VALUES ($1, $2, $3) RETURNING id`, name, position, int64(p)).Scan(&id))
	w.svc.Invalidate()
	return id
}

func (w *world) previewMapped(sub perm.Subject, m map[string]string) (*Plan, error) {
	return w.im.Preview(context.Background(), sub, "https://discord.new/AbCdEf123456", m)
}

func (w *world) count(query string, args ...any) int {
	w.t.Helper()
	var n int
	w.must(w.p.QueryRow(context.Background(), query, args...).Scan(&n))
	return n
}

func TestMappedRoleCreatesNoDuplicateAndKeepsItsPermissions(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	mods := w.roleAt("Mods", 3, perm.ViewChannel)
	roles := w.count(`SELECT COUNT(*) FROM roles`)

	m := map[string]string{"12": mods.String()} // Discord "Moderator" -> Mnema "Mods"
	plan, err := w.previewMapped(w.admin, m)
	w.must(err)
	if plan.Counts.Roles != 2 || len(plan.MappedRoles) != 1 {
		t.Fatalf("plan roles = %d, mapped = %v", plan.Counts.Roles, plan.MappedRoles)
	}
	req := applyReq(plan)
	req.RoleMap = m
	if _, err := w.im.Apply(ctx, w.admin, "adm", req); err != nil {
		t.Fatal(err)
	}

	if got := w.count(`SELECT COUNT(*) FROM roles`); got != roles+2 {
		t.Fatalf("roles = %d, want %d (Member and Admin only)", got, roles+2)
	}
	if n := w.count(`SELECT COUNT(*) FROM roles WHERE name IN ('Moderator', 'Moderator (2)')`); n != 0 {
		t.Fatalf("a Moderator role was created anyway")
	}
	if p := w.count(`SELECT permissions FROM roles WHERE id = $1`, mods); perm.Set(p) != perm.ViewChannel {
		t.Fatalf("the mapped role's permissions changed to %d", p)
	}
	// The staff channel's Moderator overwrite now belongs to the existing role.
	if n := w.count(`
		SELECT COUNT(*) FROM permission_overwrites o JOIN channels c ON c.id = o.channel_id
		WHERE c.name = 'staff' AND o.role_id = $1 AND o.allow = $2`, mods, int64(perm.ViewChannel)); n != 1 {
		t.Fatalf("%d overwrites for the mapped role on #staff, want 1", n)
	}
	// Undo removes what the import created and leaves the mapped role alone.
	list, err := w.im.List(ctx)
	w.must(err)
	if _, err := w.im.Undo(ctx, w.admin, list[0].ID, UndoRequest{}); err != nil {
		t.Fatal(err)
	}
	if n := w.count(`SELECT COUNT(*) FROM roles WHERE id = $1`, mods); n != 1 {
		t.Fatal("undo deleted the mapped role")
	}
}

func TestSameNameRoleIsSuggestedButNeverMappedAutomatically(t *testing.T) {
	w := newWorld(t)
	member := w.roleAt("member", 3, 0)
	plan := w.preview(w.admin)
	if plan.Counts.Roles != 3 {
		t.Fatalf("roles = %d: the suggestion must not map by itself", plan.Counts.Roles)
	}
	for _, r := range plan.Roles {
		if r.SourceName == "Member" && (r.SuggestedRoleID == nil || *r.SuggestedRoleID != member) {
			t.Fatalf("Member suggestion = %v, want %v", r.SuggestedRoleID, member)
		}
	}
}

func TestMappingOntoProtectedOrHigherRolesIsRefused(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	var managed uuid.UUID
	var botUser uuid.UUID
	w.must(w.p.QueryRow(ctx, `INSERT INTO users (username, display_name, password_hash, role, is_bot) VALUES ('bot', 'bot', 'x', 'user', TRUE) RETURNING id`).Scan(&botUser))
	w.must(w.p.QueryRow(ctx, `SELECT id FROM roles WHERE managed AND bot_user_id IS NULL`).Scan(&managed)) // the shared Bot role
	heldByBot := w.roleAt("Integrations", 4, 0)
	_, err := w.p.Exec(ctx, `INSERT INTO bots (user_id, name) VALUES ($1, 'bot')`, botUser)
	w.must(err)
	_, err = w.p.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2)`, botUser, heldByBot)
	w.must(err)
	var above, everyone uuid.UUID
	w.must(w.p.QueryRow(ctx, `SELECT id FROM roles WHERE name = 'Helpers'`).Scan(&above)) // above the admin's Admins role
	w.must(w.p.QueryRow(ctx, `SELECT id FROM roles WHERE is_everyone`).Scan(&everyone))
	w.svc.Invalidate()
	before := w.tables()

	for name, c := range map[string]struct {
		sub    perm.Subject
		target uuid.UUID
		status int
	}{
		"managed Bot role":              {w.admin, managed, http.StatusForbidden},
		"managed Bot role by the Owner": {w.owner, managed, http.StatusForbidden},
		"role above the importer":       {w.admin, above, http.StatusForbidden},
		"role held by a bot":            {w.admin, heldByBot, http.StatusForbidden},
		"@everyone":                     {w.admin, everyone, http.StatusBadRequest},
		"unknown role":                  {w.admin, uuid.New(), http.StatusBadRequest},
	} {
		m := map[string]string{"13": c.target.String()}
		_, err := w.previewMapped(c.sub, m)
		wantStatus(t, name+" (preview)", err, c.status)
		plan := w.preview(c.sub)
		req := applyReq(plan)
		req.RoleMap = m
		_, err = w.im.Apply(ctx, c.sub, "x", req)
		wantStatus(t, name+" (apply)", err, c.status)
	}
	// The Owner may map onto a role a bot holds and onto roles above the admin.
	if _, err := w.previewMapped(w.owner, map[string]string{"13": heldByBot.String(), "12": above.String()}); err != nil {
		t.Fatalf("owner: %v", err)
	}
	for tbl, n := range before {
		if got := w.tables()[tbl]; got != n {
			t.Errorf("%s: %d rows before, %d after refused imports", tbl, n, got)
		}
	}
}

func TestApplyNeedsTheMappingThePreviewWasMadeWith(t *testing.T) {
	w := newWorld(t)
	mods := w.roleAt("Mods", 3, 0)
	m := map[string]string{"12": mods.String()}
	plan, err := w.previewMapped(w.admin, m)
	w.must(err)
	_, err = w.im.Apply(context.Background(), w.admin, "adm", applyReq(plan)) // role_map left out
	wantStatus(t, "apply without the preview's mapping", err, http.StatusConflict)

	plain := w.preview(w.admin)
	req := applyReq(plain)
	req.RoleMap = m
	_, err = w.im.Apply(context.Background(), w.admin, "adm", req) // mapping the preview did not have
	wantStatus(t, "apply with another mapping", err, http.StatusConflict)
}
