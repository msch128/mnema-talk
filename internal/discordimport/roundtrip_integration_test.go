//go:build integration

package discordimport

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/perm"
)

// The round-trip oracle of the import: an import followed by its undo leaves
// every table as it was, row for row, except the import's own record, its
// object list and the audit log, which say that both happened.

// roundTripBookkeeping are the tables an import and its undo may change.
var roundTripBookkeeping = map[string]bool{
	"discord_imports": true, "discord_import_objects": true, "audit_log": true, "schema_migrations": true,
}

// dump is every row of every application table as text, by table.
func (w *world) dump() map[string][]string {
	w.t.Helper()
	ctx := context.Background()
	rows, err := w.p.Query(ctx, `SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind = 'r' ORDER BY 1`)
	w.must(err)
	tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
	w.must(err)
	out := map[string][]string{}
	for _, tbl := range tables {
		if roundTripBookkeeping[tbl] {
			continue
		}
		r, err := w.p.Query(ctx, `SELECT t::text FROM `+pgx.Identifier{tbl}.Sanitize()+` t`)
		w.must(err)
		vals, err := pgx.CollectRows(r, pgx.RowTo[string])
		w.must(err)
		sort.Strings(vals)
		out[tbl] = vals
	}
	return out
}

// diffDumps lists, per table, the rows only in a and the rows only in b.
func diffDumps(a, b map[string][]string) []string {
	var out []string
	for tbl := range a {
		only := func(x, y []string) []string {
			in := map[string]int{}
			for _, v := range y {
				in[v]++
			}
			var d []string
			for _, v := range x {
				if in[v] > 0 {
					in[v]--
					continue
				}
				d = append(d, v)
			}
			return d
		}
		for _, v := range only(a[tbl], b[tbl]) {
			out = append(out, tbl+" before only: "+v)
		}
		for _, v := range only(b[tbl], a[tbl]) {
			out = append(out, tbl+" after only:  "+v)
		}
	}
	sort.Strings(out)
	return out
}

// closeRolePositions gives the custom roles the positions 1..n, as the role
// routes keep them (creating shifts up, deleting closes the gap). The world
// starts with gaps that no route makes.
func (w *world) closeRolePositions() {
	w.t.Helper()
	_, err := w.p.Exec(context.Background(), `UPDATE roles r SET position = n.rn
		FROM (SELECT id, ROW_NUMBER() OVER (ORDER BY position, created_at, id) AS rn FROM roles WHERE NOT is_everyone) n
		WHERE r.id = n.id`)
	w.must(err)
	w.svc.Invalidate()
}

// importedObject is one object of kind the import created (a text channel for
// kind "channel").
func (w *world) importedObject(rec Record, kind string) uuid.UUID {
	w.t.Helper()
	var id uuid.UUID
	w.must(w.p.QueryRow(context.Background(), `SELECT o.object_id FROM discord_import_objects o
		LEFT JOIN channels c ON c.id = o.object_id
		WHERE o.import_id = $1 AND o.kind = $2 AND (o.kind <> 'channel' OR c.type = 'text')
		ORDER BY o.object_id LIMIT 1`, rec.ID, kind).Scan(&id))
	return id
}

func (w *world) exec(sql string, args ...any) {
	w.t.Helper()
	_, err := w.p.Exec(context.Background(), sql, args...)
	w.must(err)
}

type roundTrip struct {
	name string
	// roleMap maps Discord role ids onto existing roles, made by the world.
	roleMap func(w *world) map[string]string
	// between changes the imported objects before the undo.
	between func(w *world, rec Record)
}

var roundTrips = []roundTrip{
	{name: "nothing in between"},
	{name: "a member holds an imported role and wrote in an imported channel", between: func(w *world, rec Record) {
		w.exec(`INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2)`, w.mod.ID, w.importedObject(rec, "role"))
		w.exec(`INSERT INTO messages (channel_id, user_id, content) VALUES ($1, $2, 'hello')`, w.importedObject(rec, "channel"), w.mod.ID)
	}},
	{name: "an existing channel got an overwrite for an imported role", between: func(w *world, rec Record) {
		w.exec(`INSERT INTO permission_overwrites (channel_id, role_id, allow, deny) VALUES ($1, $2, 1024, 0)`, w.chA1, w.importedObject(rec, "role"))
	}},
	{name: "a Discord role mapped onto an existing one", roleMap: func(w *world) map[string]string {
		return map[string]string{"12": w.roleAt("Mods", 3, perm.ViewChannel).String()}
	}},
}

func TestImportThenUndoLeavesEveryTableAsItWas(t *testing.T) {
	for _, rt := range roundTrips {
		t.Run(rt.name, func(t *testing.T) {
			w := newWorld(t)
			var m map[string]string
			if rt.roleMap != nil {
				m = rt.roleMap(w)
			}
			w.closeRolePositions()
			ctx := context.Background()
			before := w.dump()
			plan, err := w.previewMapped(w.admin, m)
			w.must(err)
			req := applyReq(plan)
			req.RoleMap = m
			rec, err := w.im.Apply(ctx, w.admin, "adm", req)
			w.must(err)
			if changed := diffDumps(before, w.dump()); len(changed) < 10 {
				t.Fatalf("the import changed only %d rows: the oracle would prove nothing", len(changed))
			}
			if rt.between != nil {
				rt.between(w, *rec)
			}
			res, err := w.im.Undo(ctx, w.admin, rec.ID, UndoRequest{AcceptMessageLoss: true})
			w.must(err)
			if res.KeptRoles != 0 || len(res.KeptCategories) != 0 {
				t.Fatalf("the undo kept something: %+v", res)
			}
			if d := diffDumps(before, w.dump()); len(d) > 0 {
				t.Errorf("after import and undo %d rows differ:\n%s", len(d), strings.Join(d, "\n"))
			}
		})
	}
}
