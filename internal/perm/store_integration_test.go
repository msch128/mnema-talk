//go:build integration

package perm_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/testutil"
)

func TestMain(m *testing.M) { os.Exit(testutil.Main(m)) }

type world struct {
	p        *db.Pool
	svc      *perm.Service
	owner    perm.Subject
	member   perm.Subject
	other    perm.Subject
	channel  uuid.UUID
	category uuid.UUID
	everyone uuid.UUID
}

func newWorld(t *testing.T) *world {
	t.Helper()
	p := testutil.DB(t)
	testutil.Reset(t, p)
	w := &world{p: p, svc: perm.NewService(p)}
	user := func(name, role string) perm.Subject {
		var id uuid.UUID
		if err := p.QueryRow(context.Background(), `INSERT INTO users (username, display_name, password_hash, role) VALUES ($1, $1, 'x', $2) RETURNING id`, name, role).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return perm.Subject{ID: id, Owner: role == "admin"}
	}
	w.owner, w.member, w.other = user("owner", "admin"), user("member", "user"), user("other", "user")
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(p.QueryRow(context.Background(), `INSERT INTO categories (name) VALUES ('c') RETURNING id`).Scan(&w.category))
	must(p.QueryRow(context.Background(), `INSERT INTO channels (name, type) VALUES ('ch', 'text') RETURNING id`).Scan(&w.channel))
	must(p.QueryRow(context.Background(), `SELECT id FROM roles WHERE is_everyone`).Scan(&w.everyone))
	return w
}

func apiCode(err error) int {
	if api, ok := httpx.AsAPIError(err); ok {
		return api.Status
	}
	return 0
}

func wantAPI(t *testing.T, what string, err error, status int) {
	t.Helper()
	if apiCode(err) != status {
		t.Fatalf("%s: got %v, want status %d", what, err, status)
	}
}

func TestMigrationSeedsTheDocumentedDefaults(t *testing.T) {
	w := newWorld(t)
	roles, err := w.svc.Roles(context.Background())
	if err != nil || len(roles) != 2 || !roles[1].Everyone || roles[1].Permissions != perm.DefaultEveryone || roles[1].Position != 0 ||
		!roles[0].Managed || roles[0].Name != "Bot" || roles[0].Position != 1 || roles[0].Permissions != 0 || !roles[0].Hoist {
		t.Fatalf("roles after migration: %+v %v", roles, err)
	}
}

func TestStoreEnforcesRulesWithoutTheHTTPLayer(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	name := "Role"

	// A plain member holds no management permission at all.
	_, err := w.svc.CreateRole(ctx, w.member, perm.RoleInput{Name: &name})
	wantAPI(t, "create as member", err, 403)
	_, err = w.svc.UpdateRole(ctx, w.member, w.everyone, perm.RoleInput{})
	wantAPI(t, "update as member", err, 403)
	wantAPI(t, "delete as member", w.svc.DeleteRole(ctx, w.member, uuid.New()), 403)
	wantAPI(t, "order as member", w.svc.ReorderRoles(ctx, w.member, nil), 403)
	wantAPI(t, "assign as member", w.svc.SetMemberRoles(ctx, w.member, w.other.ID, nil), 403)
	wantAPI(t, "overwrites as member", w.svc.SetOverwrites(ctx, w.member, perm.TargetChannel, w.channel, nil), 403)

	// Role input is validated.
	long := strings.Repeat("x", 33)
	bad := -1
	huge := 1 << 30
	unknown := perm.Set(1 << 40)
	for _, c := range []struct {
		what string
		in   perm.RoleInput
	}{
		{"missing name", perm.RoleInput{}},
		{"long name", perm.RoleInput{Name: &long}},
		{"negative color", perm.RoleInput{Name: &name, Color: &bad}},
		{"huge color", perm.RoleInput{Name: &name, Color: &huge}},
		{"unknown bits", perm.RoleInput{Name: &name, Permissions: &unknown}},
	} {
		_, err := w.svc.CreateRole(ctx, w.owner, c.in)
		wantAPI(t, c.what, err, 400)
	}
	hoist, mention := true, true
	role, err := w.svc.CreateRole(ctx, w.owner, perm.RoleInput{Name: &name, Hoist: &hoist, Mentionable: &mention})
	if err != nil || !role.Hoist || !role.Mentionable || role.Position != 2 {
		t.Fatalf("create: %+v %v", role, err)
	}
	for _, c := range []struct {
		what string
		in   perm.RoleInput
	}{
		{"update with a blank name", perm.RoleInput{Name: &long}},
		{"update with a bad color", perm.RoleInput{Color: &bad}},
		{"update with unknown bits", perm.RoleInput{Permissions: &unknown}},
	} {
		_, err := w.svc.UpdateRole(ctx, w.owner, role.ID, c.in)
		wantAPI(t, c.what, err, 400)
	}
	off := false
	updated, err := w.svc.UpdateRole(ctx, w.owner, role.ID, perm.RoleInput{Mentionable: &off, Hoist: &off})
	if err != nil || updated.Mentionable || updated.Hoist {
		t.Fatalf("flags: %+v %v", updated, err)
	}

	// There is a ceiling on the number of roles.
	if _, err := w.p.Exec(ctx, `INSERT INTO roles (name, position) SELECT 'bulk ' || g, g + 1 FROM generate_series(1, 260) g`); err != nil {
		t.Fatal(err)
	}
	w.svc.Invalidate()
	_, err = w.svc.CreateRole(ctx, w.owner, perm.RoleInput{Name: &name})
	wantAPI(t, "too many roles", err, 409)
}

func TestStoreOverwrites(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)

	// Unknown targets.
	_, err := w.svc.Overwrites(ctx, "server", w.channel)
	wantAPI(t, "unknown kind", err, 400)
	wantAPI(t, "unknown kind on write", w.svc.SetOverwrites(ctx, w.owner, "server", w.channel, nil), 400)
	_, err = w.svc.Overwrites(ctx, perm.TargetChannel, uuid.New())
	wantAPI(t, "unknown channel", err, 404)
	_, err = w.svc.Overwrites(ctx, perm.TargetCategory, uuid.New())
	wantAPI(t, "unknown category", err, 404)
	wantAPI(t, "unknown channel on write", w.svc.SetOverwrites(ctx, w.owner, perm.TargetChannel, uuid.New(), nil), 404)
	wantAPI(t, "unknown category on write", w.svc.SetOverwrites(ctx, w.owner, perm.TargetCategory, uuid.New(), nil), 404)
	tooMany := make([]perm.Overwrite, 201)
	wantAPI(t, "too many overwrites", w.svc.SetOverwrites(ctx, w.owner, perm.TargetChannel, w.channel, tooMany), 400)

	// Entries for roles and members, empty ones are dropped, both are listed.
	set := []perm.Overwrite{
		{Type: "role", ID: w.everyone, Deny: perm.SendMessages},
		{Type: "member", ID: w.member.ID, Allow: perm.Speak},
		{Type: "member", ID: w.other.ID},
	}
	if err := w.svc.SetOverwrites(ctx, w.owner, perm.TargetChannel, w.channel, set); err != nil {
		t.Fatal(err)
	}
	got, err := w.svc.Overwrites(ctx, perm.TargetChannel, w.channel)
	if err != nil || len(got) != 2 {
		t.Fatalf("listed: %+v %v", got, err)
	}
	kinds := map[string]perm.Overwrite{}
	for _, o := range got {
		kinds[o.Type] = o
	}
	if kinds["role"].Deny != perm.SendMessages || kinds["member"].Allow != perm.Speak || kinds["member"].ID != w.member.ID {
		t.Fatalf("round trip: %+v", got)
	}
	if p, err := w.svc.Channel(ctx, w.member, w.channel); err != nil || p.Has(perm.SendMessages) || !p.Has(perm.Speak) {
		t.Fatalf("effective: %b %v", p, err)
	}

	// A manager who cannot see a hidden category cannot edit or read it blindly.
	manage, name := perm.ManageChannels, "Mod"
	mod, err := w.svc.CreateRole(ctx, w.owner, perm.RoleInput{Name: &name, Permissions: &manage})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.svc.SetMemberRoles(ctx, w.owner, w.member.ID, []uuid.UUID{mod.ID}); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.SetOverwrites(ctx, w.owner, perm.TargetCategory, w.category, []perm.Overwrite{{Type: "role", ID: w.everyone, Deny: perm.ViewChannel}}); err != nil {
		t.Fatal(err)
	}
	wantAPI(t, "hidden category", w.svc.SetOverwrites(ctx, w.member, perm.TargetCategory, w.category, nil), 404)
	wantAPI(t, "hidden channel", w.svc.SetOverwrites(ctx, w.owner, perm.TargetChannel, w.channel, []perm.Overwrite{{Type: "role", ID: w.everyone, Deny: perm.ViewChannel}}), 0)
	wantAPI(t, "hidden channel for the manager", w.svc.SetOverwrites(ctx, w.member, perm.TargetChannel, w.channel, nil), 404)
}

// faultTracer cancels the shared context when the nth statement matching
// match starts (or ends, with end): a client dropping the request at that
// stage while every statement still runs through pgx and PostgreSQL.
type faultTracer struct {
	cancel context.CancelFunc
	match  func(string) bool
	nth    int
	// prepare cancels once the statement has been prepared and before its
	// rows stream (a failure while reading rows) instead of before it runs.
	prepare bool

	mu   sync.Mutex
	seen int
}

type faultKey struct{}

func (f *faultTracer) hit(sql string) bool {
	if !f.match(sql) {
		return false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen++
	return f.seen == f.nth
}

func (f *faultTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if !f.prepare && f.hit(data.SQL) {
		f.cancel()
	}
	return ctx
}

func (f *faultTracer) TracePrepareStart(ctx context.Context, _ *pgx.Conn, data pgx.TracePrepareStartData) context.Context {
	if f.prepare && f.hit(data.SQL) {
		return context.WithValue(ctx, faultKey{}, true)
	}
	return ctx
}

func (f *faultTracer) TracePrepareEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TracePrepareEndData) {
	if f.prepare && ctx.Value(faultKey{}) == true {
		f.cancel()
	}
}

func (f *faultTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func faultService(t *testing.T, source *db.Pool, tracer *faultTracer) (*perm.Service, context.Context) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	tracer.cancel = cancel
	cfg := source.Config().Copy()
	cfg.ConnConfig.Tracer = tracer
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return perm.NewService(&db.Pool{Pool: pool}), ctx
}

func contains(sub string) func(string) bool {
	return func(sql string) bool { return strings.Contains(sql, sub) }
}

func TestStoreFailsWhenTheDatabaseDoes(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	name := "Role"
	// Two roles to move and one member who holds the lower one.
	low, err := w.svc.CreateRole(ctx, w.owner, perm.RoleInput{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	high, err := w.svc.CreateRole(ctx, w.owner, perm.RoleInput{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	// The member holds both: their roles are listed highest first.
	if err := w.svc.SetMemberRoles(ctx, w.owner, w.member.ID, []uuid.UUID{low.ID, high.ID}); err != nil {
		t.Fatal(err)
	}
	if held, err := w.svc.MemberRoles(ctx); err != nil || len(held[w.member.ID]) != 2 || held[w.member.ID][0] != low.ID {
		t.Fatalf("member roles: %v %v", held, err)
	}
	if err := perm.NewService(nil).RequireGlobal(ctx, w.member, perm.ManageRoles); err == nil {
		t.Fatal("an unreadable state must fail the check")
	}

	for _, c := range []struct {
		name  string
		match string
		nth   int
		rows  bool // fail while the rows stream instead of when the statement starts
		op    func(*perm.Service, context.Context) error
	}{
		{"lock", "pg_advisory_xact_lock", 1, false, func(s *perm.Service, c context.Context) error {
			_, err := s.CreateRole(c, w.owner, perm.RoleInput{Name: &name})
			return err
		}},
		{"read in a change", "FROM roles", 1, false, func(s *perm.Service, c context.Context) error {
			_, err := s.CreateRole(c, w.owner, perm.RoleInput{Name: &name})
			return err
		}},
		{"read rows", "FROM user_roles", 1, true, func(s *perm.Service, c context.Context) error {
			_, err := s.CreateRole(c, w.owner, perm.RoleInput{Name: &name})
			return err
		}},
		{"shift roles", "position = position + 1", 1, false, func(s *perm.Service, c context.Context) error {
			_, err := s.CreateRole(c, w.owner, perm.RoleInput{Name: &name})
			return err
		}},
		{"insert role", "INSERT INTO roles", 1, false, func(s *perm.Service, c context.Context) error {
			_, err := s.CreateRole(c, w.owner, perm.RoleInput{Name: &name})
			return err
		}},
		{"update role", "UPDATE roles SET name", 1, false, func(s *perm.Service, c context.Context) error {
			_, err := s.UpdateRole(c, w.owner, low.ID, perm.RoleInput{Name: &name})
			return err
		}},
		{"holders", "SELECT user_id FROM user_roles WHERE role_id", 1, false, func(s *perm.Service, c context.Context) error {
			_, err := s.UpdateRole(c, w.owner, low.ID, perm.RoleInput{Name: &name})
			return err
		}},
		{"holders rows", "SELECT user_id FROM user_roles WHERE role_id", 1, true, func(s *perm.Service, c context.Context) error {
			_, err := s.UpdateRole(c, w.owner, low.ID, perm.RoleInput{Name: &name})
			return err
		}},
		{"delete role", "DELETE FROM roles", 1, false, func(s *perm.Service, c context.Context) error { return s.DeleteRole(c, w.owner, high.ID) }},
		{"close the gap", "position = position - 1", 1, false, func(s *perm.Service, c context.Context) error { return s.DeleteRole(c, w.owner, high.ID) }},
		{"holders before delete", "SELECT user_id FROM user_roles WHERE role_id", 1, false, func(s *perm.Service, c context.Context) error { return s.DeleteRole(c, w.owner, high.ID) }},
		{"move role", "UPDATE roles SET position = v.pos", 1, false, func(s *perm.Service, c context.Context) error {
			return s.ReorderRoles(c, w.owner, []uuid.UUID{high.ID, low.ID})
		}},
		{"load user", "SELECT role, is_bot FROM users WHERE id", 1, false, func(s *perm.Service, c context.Context) error {
			return s.SetMemberRoles(c, w.owner, w.other.ID, []uuid.UUID{low.ID})
		}},
		{"assign role", "INSERT INTO user_roles", 1, false, func(s *perm.Service, c context.Context) error {
			return s.SetMemberRoles(c, w.owner, w.other.ID, []uuid.UUID{low.ID})
		}},
		{"remove role", "DELETE FROM user_roles", 1, false, func(s *perm.Service, c context.Context) error { return s.SetMemberRoles(c, w.owner, w.member.ID, nil) }},
		{"snapshot", "FROM roles", 1, false, func(s *perm.Service, c context.Context) error { _, err := s.Roles(c); return err }},
		{"snapshot for members", "FROM roles", 1, false, func(s *perm.Service, c context.Context) error { _, err := s.MemberRoles(c); return err }},
		{"snapshot for overwrites", "FROM roles", 1, false, func(s *perm.Service, c context.Context) error {
			_, err := s.Overwrites(c, perm.TargetChannel, w.channel)
			return err
		}},
		{"reload for an unknown channel", "FROM roles", 2, false, func(s *perm.Service, c context.Context) error {
			_, err := s.Overwrites(c, perm.TargetChannel, uuid.New())
			return err
		}},
		{"list overwrites", "FROM permission_overwrites WHERE", 1, false, func(s *perm.Service, c context.Context) error {
			_, err := s.Overwrites(c, perm.TargetChannel, w.channel)
			return err
		}},
		{"list overwrites rows", "FROM permission_overwrites WHERE", 1, true, func(s *perm.Service, c context.Context) error {
			_, err := s.Overwrites(c, perm.TargetChannel, w.channel)
			return err
		}},
		{"load member", "SELECT role FROM users WHERE id", 1, false, func(s *perm.Service, c context.Context) error {
			return s.SetOverwrites(c, w.owner, perm.TargetChannel, w.channel, []perm.Overwrite{{Type: "member", ID: w.other.ID, Deny: perm.Speak}})
		}},
		{"clear overwrites", "DELETE FROM permission_overwrites", 1, false, func(s *perm.Service, c context.Context) error {
			return s.SetOverwrites(c, w.owner, perm.TargetChannel, w.channel, nil)
		}},
		{"insert overwrite", "INSERT INTO permission_overwrites", 1, false, func(s *perm.Service, c context.Context) error {
			return s.SetOverwrites(c, w.owner, perm.TargetChannel, w.channel, []perm.Overwrite{{Type: "role", ID: w.everyone, Deny: perm.Speak}})
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			svc, cctx := faultService(t, w.p, &faultTracer{match: contains(c.match), nth: c.nth, prepare: c.rows})
			if c.name == "reload for an unknown channel" {
				// A first load must succeed to get as far as the reload.
				if _, err := svc.Roles(cctx); err != nil {
					t.Fatal(err)
				}
			}
			err := c.op(svc, cctx)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("got %v, want the cancellation to surface", err)
			}
		})
	}
}

// With bots around, every change by a non-Owner is checked by outcome: the
// permission state is read again after it, and a failure of that read fails
// the change. A bot's roles cannot be set through a member's account.
func TestBotInvariantReadsTheStateAfterAChange(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	var botUser uuid.UUID
	if err := w.p.QueryRow(ctx, `INSERT INTO users (username, display_name, password_hash, role, is_bot) VALUES ('bot-x', 'x', '!', 'user', TRUE) RETURNING id`).Scan(&botUser); err != nil {
		t.Fatal(err)
	}
	if _, err := w.p.Exec(ctx, `INSERT INTO bots (user_id, name) VALUES ($1, 'x')`, botUser); err != nil {
		t.Fatal(err)
	}
	managers := perm.ManageRoles
	name := "Managers"
	role, err := w.svc.CreateRole(ctx, w.owner, perm.RoleInput{Name: &name, Permissions: &managers})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.svc.SetMemberRoles(ctx, w.owner, w.member.ID, []uuid.UUID{role.ID}); err != nil {
		t.Fatal(err)
	}

	other := "Other"
	if _, err := w.svc.CreateRole(ctx, w.member, perm.RoleInput{Name: &other}); err != nil {
		t.Fatalf("an ordinary change by a non-Owner: %v", err)
	}
	svc, cctx := faultService(t, w.p, &faultTracer{match: contains("FROM roles"), nth: 2})
	third := "Third"
	if _, err := svc.CreateRole(cctx, w.member, perm.RoleInput{Name: &third}); !errors.Is(err, context.Canceled) {
		t.Fatalf("a failing read after the change must fail it, got %v", err)
	}

	wantAPI(t, "bot roles on a member", w.svc.SetBotRoles(ctx, w.owner, w.member.ID, nil), 404)
	wantAPI(t, "member roles on a bot", w.svc.SetMemberRoles(ctx, w.owner, botUser, nil), 403)
}
