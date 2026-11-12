//go:build integration

package server

import (
	"bytes"
	"context"
	"net/http"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/msch128/mnema-talk/internal/db"
)

// The schema oracle of the personal data paths. Every column that refers to a
// person (a foreign key to users, or a column whose name says it holds a
// person's id or name) must be named by an erasure statement or be listed in
// erasureKeeps with the reason it stays; every table that holds such a column
// must be read while an export is built or be listed in exportOmits; every
// column of users must be read by the export or be listed in profileOmits. The
// lists are the decisions: a new table or column fails until someone makes one.
// A known gap (a finding of the 2026-11 defect hunt) is listed apart: its check
// is inverted, so it fails the day the gap is closed and the entry must go.

// personColumn is one column that refers to a person.
type personColumn struct{ table, column string }

func (c personColumn) String() string { return c.table + "." + c.column }

// personColumns reads the schema: every foreign key to users, plus columns
// without one whose name says they hold a person's id or name.
func (a *app) personColumns() []personColumn {
	a.t.Helper()
	rows, err := a.db.Query(context.Background(), `
		SELECT c.relname, att.attname FROM pg_attribute att
		JOIN pg_class c ON c.oid = att.attrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind = 'r' AND att.attnum > 0 AND NOT att.attisdropped
		AND (EXISTS (SELECT 1 FROM pg_constraint k WHERE k.contype = 'f' AND k.conrelid = att.attrelid
				AND att.attnum = ANY (k.conkey) AND k.confrelid = 'users'::regclass)
			OR (format_type(att.atttypid, NULL) IN ('uuid', 'text')
				AND att.attname ~ '(^|_)(user|author|actor|editor|creator|owner|subject|reporter|blocker|blocked|uploader|leader|target)(_id)?$')
			OR att.attname ~ '_by$|_by_name$|^(username|email|actor_name|target_name|author_name)$')
		ORDER BY 1, 2`)
	if err != nil {
		a.t.Fatal(err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (personColumn, error) {
		var c personColumn
		return c, r.Scan(&c.table, &c.column)
	})
	if err != nil {
		a.t.Fatal(err)
	}
	return out
}

// tableColumns lists the columns of one table.
func (a *app) tableColumns(table string) []string {
	a.t.Helper()
	rows, err := a.db.Query(context.Background(), `SELECT attname FROM pg_attribute
		WHERE attrelid = $1::regclass AND attnum > 0 AND NOT attisdropped ORDER BY attnum`, table)
	if err != nil {
		a.t.Fatal(err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		a.t.Fatal(err)
	}
	return out
}

// verdicts checks that every item is covered, listed with a reason, or a known
// gap, and that no list entry is stale. It returns how many were covered.
type verdicts struct {
	what    string
	reasons map[string]string
	gaps    map[string]string
}

func (v verdicts) check(t *testing.T, items []string, covered func(string) bool) int {
	t.Helper()
	seen := map[string]bool{}
	n := 0
	for _, it := range items {
		seen[it] = true
		_, reason := v.reasons[it]
		gap, known := v.gaps[it]
		ok := covered(it)
		switch {
		case ok && known:
			t.Errorf("%s: %s is covered now; %s is fixed, remove it from the known gaps", v.what, it, gap)
		case ok && reason:
			t.Errorf("%s: %s is covered and also listed with a reason: drop the entry", v.what, it)
		case ok:
			n++
		case !reason && !known:
			t.Errorf("%s: %s is not covered and no reason is listed", v.what, it)
		}
	}
	for _, list := range []map[string]string{v.reasons, v.gaps} {
		for k := range list {
			if !seen[k] {
				t.Errorf("%s: the list names %s, which the schema no longer has", v.what, k)
			}
		}
	}
	return n
}

// erases reports whether a statement of the plan removes or rewrites c: it
// deletes rows of c's table, or updates that table naming the column.
func erases(plan []string, c personColumn) bool {
	del := regexp.MustCompile(`(?i)\bDELETE\s+FROM\s+` + c.table + `\b`)
	upd := regexp.MustCompile(`(?i)\bUPDATE\s+` + c.table + `\b`)
	col := regexp.MustCompile(`\b` + c.column + `\b`)
	for _, sql := range plan {
		if del.MatchString(sql) || (upd.MatchString(sql) && col.MatchString(sql)) {
			return true
		}
	}
	return false
}

func TestEveryColumnAboutAPersonIsErasedOrKeptForAReason(t *testing.T) {
	a := newApp(t, false)
	cols := a.personColumns()
	if len(cols) < 60 {
		t.Fatalf("the schema read found only %d columns: the query is broken", len(cols))
	}
	var plan []string
	for _, st := range personalDataHandler().ErasePlan() {
		plan = append(plan, st.SQL)
	}
	byName := map[string]personColumn{}
	names := make([]string, len(cols))
	for i, c := range cols {
		byName[c.String()], names[i] = c, c.String()
	}
	v := verdicts{what: "erasure", reasons: erasureKeeps, gaps: erasureGaps}
	if n := v.check(t, names, func(s string) bool { return erases(plan, byName[s]) }); n < 50 {
		t.Fatalf("only %d columns matched an erasure statement: the matcher is broken", n)
	}
}

// exportTrace records the statements an export runs: those sent from inside
// the goroutine that writes the archive, and no other (sweepers share the pool).
type exportTrace struct {
	mu  sync.Mutex
	sql []string
}

func (e *exportTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	buf := make([]byte, 64<<10)
	if bytes.Contains(buf[:runtime.Stack(buf, false)], []byte("account.(*API).writeArchive")) {
		e.mu.Lock()
		e.sql = append(e.sql, d.SQL)
		e.mu.Unlock()
	}
	return ctx
}

func (*exportTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (e *exportTrace) statements() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.sql...)
}

// tracedApp is newApp whose pool records the export's statements.
func tracedApp(t *testing.T) (*app, *exportTrace) {
	t.Helper()
	trace := &exportTrace{}
	a := newAppWithDeps(t, false, nil, func(d *Deps) {
		config := d.DB.Config().Copy()
		config.ConnConfig.Tracer = trace
		config.MinConns = 0
		p, err := pgxpool.NewWithConfig(context.Background(), config)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(p.Close)
		d.DB = &db.Pool{Pool: p}
	})
	return a, trace
}

var readTable = regexp.MustCompile(`(?i)\b(?:FROM|JOIN)\s+([a-z_]+)\b`)

// tracedExport builds alice's export and returns the statements it ran and
// the tables they read.
func tracedExport(t *testing.T) (*app, []string, map[string]bool) {
	a, trace := tracedApp(t)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	if res := a.requestExport(alice, memberPassword, ""); res.status != http.StatusAccepted {
		t.Fatalf("request: %d %s", res.status, res.body)
	}
	if st := a.waitForExport(alice); st.State != "ready" {
		t.Fatalf("export: %+v", st)
	}
	stmts := trace.statements()
	read := map[string]bool{}
	for _, s := range stmts {
		for _, m := range readTable.FindAllStringSubmatch(s, -1) {
			read[strings.ToLower(m[1])] = true
		}
	}
	if len(stmts) < 20 || !read["messages"] || !read["users"] {
		t.Fatalf("the trace saw %d statements over %v: it does not see the export", len(stmts), tableNames(read))
	}
	return a, stmts, read
}

func TestEveryTableAboutAPersonIsExportedOrOmittedForAReason(t *testing.T) {
	a, stmts, read := tracedExport(t)
	tables := map[string]bool{}
	for _, c := range a.personColumns() {
		tables[c.table] = true
	}
	v := verdicts{what: "export", reasons: exportOmits, gaps: exportGaps}
	if n := v.check(t, tableNames(tables), func(s string) bool { return read[s] }); n < 25 {
		t.Fatalf("only %d tables were read by the export: the trace is broken", n)
	}

	// The account row column by column: a statement reading users names it.
	var fromUsers []string
	usersRead := regexp.MustCompile(`(?i)\b(FROM|JOIN)\s+users\b`)
	for _, s := range stmts {
		if usersRead.MatchString(s) {
			fromUsers = append(fromUsers, s)
		}
	}
	names := func(col string) bool {
		re := regexp.MustCompile(`\b` + col + `\b`)
		for _, s := range fromUsers {
			if re.MatchString(s) {
				return true
			}
		}
		return false
	}
	p := verdicts{what: "profile export", reasons: profileOmits, gaps: profileGaps}
	if n := p.check(t, a.tableColumns("users"), names); n < 15 {
		t.Fatalf("only %d columns of users were read: the trace is broken", n)
	}
}

func tableNames(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
