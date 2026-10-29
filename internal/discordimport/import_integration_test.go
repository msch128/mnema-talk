//go:build integration

package discordimport

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/testutil"
)

func TestMain(m *testing.M) { os.Exit(testutil.Main(m)) }

type world struct {
	t     *testing.T
	p     *db.Pool
	svc   *perm.Service
	im    *Importer
	owner perm.Subject
	admin perm.Subject // holds the Administrator permission through a role
	mod   perm.Subject // may manage roles and channels, but is no administrator
	// existing layout
	catA, catB, chA1, chA2, chU1 uuid.UUID
}

func newWorld(t *testing.T) *world {
	t.Helper()
	p := testutil.DB(t)
	testutil.Reset(t, p)
	srv, _ := serveTemplate(t, sampleTemplate)
	w := &world{t: t, p: p, svc: perm.NewService(p)}
	w.im = &Importer{DB: p, Perms: w.svc, Audit: &audit.Log{DB: p}, Fetcher: loopbackFetcher(srv.URL)}
	user := func(name, role string) perm.Subject {
		var id uuid.UUID
		w.must(p.QueryRow(context.Background(), `INSERT INTO users (username, display_name, password_hash, role) VALUES ($1, $1, 'x', $2) RETURNING id`, name, role).Scan(&id))
		return perm.Subject{ID: id, Owner: role == "admin"}
	}
	w.owner, w.admin, w.mod = user("owner", "admin"), user("adm", "user"), user("mod", "user")
	w.role("Admins", 1, perm.Administrator, w.admin)
	w.role("Helpers", 2, perm.ManageRoles|perm.ManageChannels, w.mod)
	w.must(p.QueryRow(context.Background(), `INSERT INTO categories (name, sort_order) VALUES ('A', 0) RETURNING id`).Scan(&w.catA))
	w.must(p.QueryRow(context.Background(), `INSERT INTO categories (name, sort_order) VALUES ('B', 1) RETURNING id`).Scan(&w.catB))
	w.must(p.QueryRow(context.Background(), `INSERT INTO channels (category_id, name, type, sort_order) VALUES ($1, 'a1', 'text', 0) RETURNING id`, w.catA).Scan(&w.chA1))
	w.must(p.QueryRow(context.Background(), `INSERT INTO channels (category_id, name, type, sort_order) VALUES ($1, 'a2', 'voice', 1) RETURNING id`, w.catA).Scan(&w.chA2))
	w.must(p.QueryRow(context.Background(), `INSERT INTO channels (name, type, sort_order) VALUES ('u1', 'text', 4) RETURNING id`).Scan(&w.chU1))
	w.svc.Invalidate()
	return w
}

func (w *world) must(err error) {
	w.t.Helper()
	if err != nil {
		w.t.Fatal(err)
	}
}

// role inserts a role above the existing ones' positions given and gives it to who.
func (w *world) role(name string, position int, p perm.Set, who perm.Subject) {
	w.t.Helper()
	var id uuid.UUID
	w.must(w.p.QueryRow(context.Background(), `INSERT INTO roles (name, position, permissions) VALUES ($1, $2, $3) RETURNING id`, name, position+10, int64(p)).Scan(&id))
	_, err := w.p.Exec(context.Background(), `INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2)`, who.ID, id)
	w.must(err)
}

// state is a stable text dump of everything that exists, for before/after comparison.
func (w *world) state(query string, ids ...any) string {
	w.t.Helper()
	rows, err := w.p.Query(context.Background(), query, ids...)
	w.must(err)
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		vals, err := rows.Values()
		w.must(err)
		fmt.Fprintln(&b, vals...)
	}
	w.must(rows.Err())
	return b.String()
}

const (
	catsQ  = `SELECT id, name, sort_order FROM categories ORDER BY id`
	chansQ = `SELECT id, name, type, topic, category_id, sort_order, user_limit, slow_mode_seconds FROM channels ORDER BY id`
	owQ    = `SELECT channel_id, category_id, role_id, allow, deny FROM permission_overwrites ORDER BY id`
	rolesQ = `SELECT id, name, permissions, is_everyone FROM roles ORDER BY id`
)

func (w *world) tables() map[string]int {
	w.t.Helper()
	out := map[string]int{}
	for _, tbl := range []string{"roles", "categories", "channels", "permission_overwrites", "discord_imports", "discord_import_objects", "audit_log"} {
		var n int
		w.must(w.p.QueryRow(context.Background(), `SELECT COUNT(*) FROM `+tbl).Scan(&n))
		out[tbl] = n
	}
	return out
}

func (w *world) preview(sub perm.Subject) *Plan {
	w.t.Helper()
	plan, err := w.im.Preview(context.Background(), sub, "https://discord.new/AbCdEf123456", nil)
	w.must(err)
	return plan
}

func applyReq(plan *Plan) ApplyRequest {
	return ApplyRequest{Template: "AbCdEf123456", PlanHash: plan.Hash, ConfirmAdditive: true, ConfirmRoles: true, ConfirmText: ConfirmWord}
}

func wantStatus(t *testing.T, what string, err error, status int) {
	t.Helper()
	api, ok := httpx.AsAPIError(err)
	if !ok || api.Status != status {
		t.Fatalf("%s: got %v, want status %d", what, err, status)
	}
}

func TestPreviewWritesNothing(t *testing.T) {
	w := newWorld(t)
	before := w.tables()
	plan := w.preview(w.admin)
	if plan.Counts.Roles != 3 || plan.Counts.Channels != 7 || plan.Counts.Categories != 2 {
		t.Fatalf("counts = %+v", plan.Counts)
	}
	w.preview(w.owner)
	after := w.tables()
	for tbl, n := range before {
		if after[tbl] != n {
			t.Errorf("%s: %d rows before the preview, %d after", tbl, n, after[tbl])
		}
	}
}

func TestOnlyOwnerAndAdministratorsImport(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	_, err := w.im.Preview(ctx, w.mod, "AbCdEf123456", nil)
	wantStatus(t, "manage-roles member previews", err, http.StatusForbidden)
	plan := w.preview(w.owner)
	_, err = w.im.Apply(ctx, w.mod, "mod", applyReq(plan))
	wantStatus(t, "manage-roles member applies", err, http.StatusForbidden)
	_, err = w.im.Undo(ctx, w.mod, uuid.New(), UndoRequest{})
	wantStatus(t, "manage-roles member undoes", err, http.StatusForbidden)
	if n := w.tables()["discord_imports"]; n != 0 {
		t.Fatalf("%d imports recorded", n)
	}
	if _, err := w.im.Apply(ctx, w.owner, "owner", applyReq(plan)); err != nil {
		t.Fatalf("owner: %v", err)
	}
}

func TestApplyNeedsAllThreeConfirmationsAndTheExactPreview(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	plan := w.preview(w.admin)
	before := w.tables()

	cases := map[string]func(*ApplyRequest){
		"step 1 missing": func(r *ApplyRequest) { r.ConfirmAdditive = false },
		"step 2 missing": func(r *ApplyRequest) { r.ConfirmRoles = false },
		"no text":        func(r *ApplyRequest) { r.ConfirmText = "" },
		"wrong text":     func(r *ApplyRequest) { r.ConfirmText = "yes" },
		"lowercase word": func(r *ApplyRequest) { r.ConfirmText = "import" },
	}
	for name, mutate := range cases {
		req := applyReq(plan)
		mutate(&req)
		_, err := w.im.Apply(ctx, w.admin, "adm", req)
		wantStatus(t, name, err, http.StatusBadRequest)
	}
	stale := applyReq(plan)
	stale.PlanHash = "deadbeef"
	_, err := w.im.Apply(ctx, w.admin, "adm", stale)
	wantStatus(t, "stale preview", err, http.StatusConflict)
	if after := w.tables(); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Fatalf("a refused import wrote something: %v -> %v", before, after)
	}

	// The community name is accepted in place of IMPORT.
	_, err = w.p.Exec(ctx, `UPDATE instance_settings SET community_name = 'Home Base'`)
	w.must(err)
	plan = w.preview(w.admin)
	req := applyReq(plan)
	req.ConfirmText = "home base"
	if _, err := w.im.Apply(ctx, w.admin, "adm", req); err != nil {
		t.Fatalf("server name as confirmation: %v", err)
	}
}

func TestImportIsStrictlyAdditive(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	oldCats, oldChans := w.state(catsQ), w.state(chansQ)
	oldOW := w.state(owQ)
	var everyonePerms int64
	w.must(w.p.QueryRow(ctx, `SELECT permissions FROM roles WHERE is_everyone`).Scan(&everyonePerms))
	oldRoleOrder := w.state(`SELECT name FROM roles WHERE NOT is_everyone ORDER BY position`)
	oldRoles := w.state(`SELECT id, name, permissions, color, hoist, mentionable, managed FROM roles ORDER BY id`)

	plan := w.preview(w.admin)
	rec, err := w.im.Apply(ctx, w.admin, "adm", applyReq(plan))
	if err != nil {
		t.Fatal(err)
	}
	if rec.Counts != plan.Counts {
		t.Fatalf("recorded %+v, planned %+v", rec.Counts, plan.Counts)
	}

	// Everything that existed is byte-identical: same names, order, placement.
	for _, tc := range []struct{ name, query, old string }{
		{"categories", `SELECT id, name, sort_order FROM categories WHERE id = ANY($1) ORDER BY id`, oldCats},
		{"channels", `SELECT id, name, type, topic, category_id, sort_order, user_limit, slow_mode_seconds FROM channels WHERE id = ANY($1) ORDER BY id`, oldChans},
	} {
		ids := []uuid.UUID{w.catA, w.catB, w.chA1, w.chA2, w.chU1}
		if got := w.state(tc.query, ids); got != tc.old {
			t.Errorf("%s changed:\nbefore:\n%s\nafter:\n%s", tc.name, tc.old, got)
		}
	}
	if got := w.state(`SELECT id, name, permissions, color, hoist, mentionable, managed FROM roles WHERE id NOT IN (SELECT object_id FROM discord_import_objects) ORDER BY id`); got != oldRoles {
		t.Errorf("existing roles changed:\n%s\n%s", oldRoles, got)
	}
	if got := w.state(`SELECT name FROM roles WHERE id NOT IN (SELECT object_id FROM discord_import_objects) AND NOT is_everyone ORDER BY position`); got != oldRoleOrder {
		t.Errorf("order of existing roles changed:\n%s\n%s", oldRoleOrder, got)
	}
	if got := w.state(owQ + ``); !strings.HasPrefix(got, oldOW) && oldOW != "" {
		t.Errorf("existing overwrites changed")
	}
	var after int64
	w.must(w.p.QueryRow(ctx, `SELECT permissions FROM roles WHERE is_everyone`).Scan(&after))
	if after != everyonePerms {
		t.Fatalf("@everyone changed: %d -> %d", everyonePerms, after)
	}

	// Appended at the end: new categories after A and B, new uncategorized after u1.
	var minNewCat, minNewUncat int
	w.must(w.p.QueryRow(ctx, `SELECT MIN(sort_order) FROM categories WHERE id IN (SELECT object_id FROM discord_import_objects WHERE kind = 'category')`).Scan(&minNewCat))
	w.must(w.p.QueryRow(ctx, `SELECT MIN(sort_order) FROM channels WHERE category_id IS NULL AND id IN (SELECT object_id FROM discord_import_objects WHERE kind = 'channel')`).Scan(&minNewUncat))
	if minNewCat != 2 || minNewUncat != 5 {
		t.Fatalf("new categories start at %d (want 2), new uncategorized channels at %d (want 5)", minNewCat, minNewUncat)
	}
	// Imported channels sit in imported categories only.
	var strays int
	w.must(w.p.QueryRow(ctx, `SELECT COUNT(*) FROM channels WHERE id IN (SELECT object_id FROM discord_import_objects WHERE kind = 'channel')
		AND category_id IS NOT NULL AND category_id IN (SELECT id FROM categories WHERE id IN ($1, $2))`, w.catA, w.catB).Scan(&strays))
	if strays != 0 {
		t.Fatalf("%d imported channels landed in an existing category", strays)
	}
}

func TestImportedRolesAreBelowEveryExistingRoleAndNeverAdministrator(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	plan := w.preview(w.admin)
	if !plan.AdministratorDropped {
		t.Fatal("the preview must say the administrator bit is dropped")
	}
	if _, err := w.im.Apply(ctx, w.admin, "adm", applyReq(plan)); err != nil {
		t.Fatal(err)
	}
	var maxNew, minOld, callerTop int
	w.must(w.p.QueryRow(ctx, `SELECT MAX(position) FROM roles WHERE id IN (SELECT object_id FROM discord_import_objects)`).Scan(&maxNew))
	w.must(w.p.QueryRow(ctx, `SELECT MIN(position) FROM roles WHERE NOT is_everyone AND id NOT IN (SELECT object_id FROM discord_import_objects)`).Scan(&minOld))
	w.must(w.p.QueryRow(ctx, `SELECT MAX(r.position) FROM roles r JOIN user_roles ur ON ur.role_id = r.id WHERE ur.user_id = $1`, w.admin.ID).Scan(&callerTop))
	if maxNew >= minOld || maxNew >= callerTop {
		t.Fatalf("imported roles reach position %d; existing roles start at %d, the importing administrator's highest is %d", maxNew, minOld, callerTop)
	}
	rows, err := w.p.Query(ctx, `SELECT name, permissions FROM roles WHERE id IN (SELECT object_id FROM discord_import_objects)`)
	w.must(err)
	defer rows.Close()
	got := map[string]perm.Set{}
	for rows.Next() {
		var n string
		var p int64
		w.must(rows.Scan(&n, &p))
		got[n] = perm.Set(p)
	}
	if got["Admin"].Has(perm.Administrator) || got["Admin"] != perm.ManageChannels {
		t.Errorf("Admin role = %v, want manage channels without administrator", keys(got["Admin"]))
	}
	if want := perm.KickMembers | perm.BanMembers | perm.ManageMessages; got["Moderator"] != want {
		t.Errorf("Moderator = %v, want %v", keys(got["Moderator"]), keys(want))
	}
	if want := perm.ViewChannel | perm.SendMessages; got["Member"] != want {
		t.Errorf("Member = %v, want %v", keys(got["Member"]), keys(want))
	}
	for name, p := range got {
		if p.Has(perm.Administrator) || p.HasAny(perm.OwnerGrantOnly) {
			t.Errorf("%s holds %v", name, keys(p))
		}
	}
}

func TestOverwritesLandOnTheNewChannelsAndRoles(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	plan := w.preview(w.admin)
	if _, err := w.im.Apply(ctx, w.admin, "adm", applyReq(plan)); err != nil {
		t.Fatal(err)
	}
	rows, err := w.p.Query(ctx, `
		SELECT r.name, r.is_everyone, o.allow, o.deny FROM permission_overwrites o
		JOIN roles r ON r.id = o.role_id JOIN channels c ON c.id = o.channel_id WHERE c.name = 'staff' ORDER BY r.is_everyone DESC`)
	w.must(err)
	defer rows.Close()
	var seen []string
	for rows.Next() {
		var name string
		var every bool
		var allow, deny int64
		w.must(rows.Scan(&name, &every, &allow, &deny))
		seen = append(seen, fmt.Sprintf("%s/%v/%v/%v", name, every, perm.Set(allow).Has(perm.ViewChannel), perm.Set(deny).Has(perm.ViewChannel)))
	}
	if got := strings.Join(seen, " "); got != "@everyone/true/false/true Moderator/false/true/false" {
		t.Fatalf("staff overwrites = %q", got)
	}
	// Existing channels got no overwrite.
	var n int
	w.must(w.p.QueryRow(ctx, `SELECT COUNT(*) FROM permission_overwrites WHERE channel_id = ANY($1)`, []uuid.UUID{w.chA1, w.chA2, w.chU1}).Scan(&n))
	if n != 0 {
		t.Fatalf("%d overwrites on existing channels", n)
	}
}

func TestApplyAuditsOnceWithCountsAndTemplate(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	plan := w.preview(w.admin)
	if n := w.tables()["audit_log"]; n != 0 {
		t.Fatalf("a preview wrote %d audit entries", n)
	}
	if _, err := w.im.Apply(ctx, w.admin, "adm", applyReq(plan)); err != nil {
		t.Fatal(err)
	}
	var n int
	var details []byte
	w.must(w.p.QueryRow(ctx, `SELECT COUNT(*) FROM audit_log WHERE action = 'import.discord'`).Scan(&n))
	w.must(w.p.QueryRow(ctx, `SELECT details FROM audit_log WHERE action = 'import.discord'`).Scan(&details))
	var d map[string]any
	w.must(json.Unmarshal(details, &d))
	if n != 1 || d["template"] != "AbCdEf123456" || d["categories"] != 2.0 || d["channels"] != 7.0 || d["roles"] != 3.0 {
		t.Fatalf("audit entries %d, details %v", n, d)
	}
	if got := w.tables()["audit_log"]; got != 1 {
		t.Fatalf("%d audit entries for one import", got)
	}
}

func TestUndoRemovesExactlyWhatWasCreated(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	plan := w.preview(w.admin)
	rec, err := w.im.Apply(ctx, w.admin, "adm", applyReq(plan))
	if err != nil {
		t.Fatal(err)
	}
	beforeOthers := map[string]string{
		"cats":  w.state(`SELECT id, name, sort_order FROM categories WHERE id = ANY($1) ORDER BY id`, []uuid.UUID{w.catA, w.catB}),
		"chans": w.state(`SELECT id, name, category_id, sort_order FROM channels WHERE id = ANY($1) ORDER BY id`, []uuid.UUID{w.chA1, w.chA2, w.chU1}),
		"roles": w.state(`SELECT name FROM roles WHERE id NOT IN (SELECT object_id FROM discord_import_objects) ORDER BY position`),
	}

	// Manual changes since: one created role and one channel deleted by hand.
	_, err = w.p.Exec(ctx, `DELETE FROM roles WHERE name = 'Moderator'`)
	w.must(err)
	_, err = w.p.Exec(ctx, `DELETE FROM channels WHERE name = 'news'`)
	w.must(err)

	d, err := w.im.Detail(ctx, rec.ID)
	w.must(err)
	if d.Remaining != (Counts{Categories: 2, Channels: 6, Roles: 2}) || len(d.ChannelsWithMessages) != 0 {
		t.Fatalf("detail = %+v", d)
	}
	res, err := w.im.Undo(ctx, w.admin, rec.ID, UndoRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != (Counts{Categories: 2, Channels: 6, Roles: 2}) || res.AlreadyGone != (Counts{Channels: 1, Roles: 1}) {
		t.Fatalf("result = %+v", res)
	}
	var left int
	w.must(w.p.QueryRow(ctx, `SELECT COUNT(*) FROM categories`).Scan(&left))
	if left != 2 {
		t.Fatalf("%d categories left, want the 2 existing ones", left)
	}
	if got := w.state(`SELECT id, name, sort_order FROM categories ORDER BY id`); got != beforeOthers["cats"] {
		t.Errorf("categories after undo:\n%s", got)
	}
	if got := w.state(`SELECT id, name, category_id, sort_order FROM channels ORDER BY id`); got != beforeOthers["chans"] {
		t.Errorf("channels after undo:\n%s", got)
	}
	if got := w.state(`SELECT name FROM roles ORDER BY position`); got != beforeOthers["roles"] {
		t.Errorf("roles after undo:\n%s\nwant:\n%s", got, beforeOthers["roles"])
	}
	var min, max int
	w.must(w.p.QueryRow(ctx, `SELECT MIN(position), MAX(position) FROM roles WHERE NOT is_everyone`).Scan(&min, &max))
	var count int
	w.must(w.p.QueryRow(ctx, `SELECT COUNT(*) FROM roles WHERE NOT is_everyone`).Scan(&count))
	if min != 1 || max != count {
		t.Fatalf("role positions %d..%d for %d roles: not closed up", min, max, count)
	}

	_, err = w.im.Undo(ctx, w.admin, rec.ID, UndoRequest{})
	wantStatus(t, "second undo", err, http.StatusConflict)
	var audits int
	w.must(w.p.QueryRow(ctx, `SELECT COUNT(*) FROM audit_log WHERE action = 'import.discord_undo'`).Scan(&audits))
	if audits != 1 {
		t.Fatalf("%d undo audit entries, want 1", audits)
	}
}

func TestUndoNeedsExtraConfirmationForChannelsWithMessages(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	plan := w.preview(w.admin)
	rec, err := w.im.Apply(ctx, w.admin, "adm", applyReq(plan))
	w.must(err)
	_, err = w.p.Exec(ctx, `INSERT INTO messages (channel_id, user_id, content) SELECT c.id, $1, 'hello' FROM channels c WHERE c.name = 'rules'`, w.admin.ID)
	w.must(err)

	d, err := w.im.Detail(ctx, rec.ID)
	w.must(err)
	if len(d.ChannelsWithMessages) != 1 || d.ChannelsWithMessages[0].Name != "rules" || d.ChannelsWithMessages[0].Messages != 1 {
		t.Fatalf("detail = %+v", d.ChannelsWithMessages)
	}
	before := w.tables()
	_, err = w.im.Undo(ctx, w.admin, rec.ID, UndoRequest{})
	wantStatus(t, "undo without the extra confirmation", err, http.StatusConflict)
	if after := w.tables(); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Fatalf("a refused undo changed data: %v -> %v", before, after)
	}
	if _, err := w.im.Undo(ctx, w.admin, rec.ID, UndoRequest{AcceptMessageLoss: true}); err != nil {
		t.Fatal(err)
	}
	var msgs int
	w.must(w.p.QueryRow(ctx, `SELECT COUNT(*) FROM messages`).Scan(&msgs))
	if msgs != 0 {
		t.Fatalf("%d messages left", msgs)
	}
}

func TestUndoKeepsACategoryThatHoldsOtherChannels(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	plan := w.preview(w.admin)
	rec, err := w.im.Apply(ctx, w.admin, "adm", applyReq(plan))
	w.must(err)
	var info uuid.UUID
	w.must(w.p.QueryRow(ctx, `SELECT id FROM categories WHERE name = 'Info'`).Scan(&info))
	_, err = w.p.Exec(ctx, `UPDATE channels SET category_id = $1 WHERE id = $2`, info, w.chU1)
	w.must(err)

	res, err := w.im.Undo(ctx, w.admin, rec.ID, UndoRequest{})
	w.must(err)
	if len(res.KeptCategories) != 1 || res.KeptCategories[0] != "Info" || res.Removed.Categories != 1 {
		t.Fatalf("result = %+v", res)
	}
	var cat *uuid.UUID
	w.must(w.p.QueryRow(ctx, `SELECT category_id FROM channels WHERE id = $1`, w.chU1).Scan(&cat))
	if cat == nil || *cat != info {
		t.Fatal("the foreign channel was touched")
	}
}

func TestStagesAndForumsAreCreatedWhenTheServerHasThem(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	w.im.Features = func(name string) bool { return name == "talks.stage" || name == "channels.forum" }
	plan := w.preview(w.admin)
	if _, err := w.im.Apply(ctx, w.admin, "adm", applyReq(plan)); err != nil {
		t.Fatal(err)
	}
	var stageType string
	var isStage bool
	w.must(w.p.QueryRow(ctx, `SELECT type, is_stage FROM channels WHERE name = 'Stage'`).Scan(&stageType, &isStage))
	if stageType != "voice" || !isStage {
		t.Fatalf("stage imported as type %q is_stage %v, want a voice channel that is a Stage", stageType, isStage)
	}
	var forumType string
	w.must(w.p.QueryRow(ctx, `SELECT type FROM channels WHERE name = 'ideas'`).Scan(&forumType))
	if forumType != "forum" {
		t.Fatalf("forum imported as type %q", forumType)
	}
}
