//go:build integration

package server

import (
	"context"
	"fmt"
	"net"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/media"
	"github.com/msch128/mnema-talk/internal/testutil"
	"github.com/msch128/mnema-talk/internal/testutil/racetest"
)

// Lane 7 of the defect hunt: an operation of several steps happens entirely
// or not at all, whichever step fails. For every k the k-th statement a
// request sends is cancelled (a real context cancellation at pgx's tracing
// hook, as in pgfault), and the row count of every table is compared with the
// state before and with the state after the operation succeeded. A mix of
// the two is a partial write.

// requestMark tags the contexts of the HTTP connections, so statements of the
// request (and of work it starts with its context) are counted and the
// server's own loops are not.
type requestMark struct{}

// stepFault cancels the k-th counted statement while armed.
type stepFault struct {
	mu     sync.Mutex
	armed  bool
	target int
	n      int
	fired  string
	seen   []string
}

func (f *stepFault) arm(target int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.armed, f.target, f.n, f.fired, f.seen = true, target, 0, "", nil
}

func (f *stepFault) disarm() (fired string, seen []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.armed = false
	return f.fired, f.seen
}

func (f *stepFault) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if ctx.Value(requestMark{}) == nil {
		return ctx
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.armed {
		return ctx
	}
	f.n++
	sql := strings.Join(strings.Fields(d.SQL), " ")
	f.seen = append(f.seen, sql)
	if f.n != f.target {
		return ctx
	}
	f.fired = sql
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	return ctx
}

func (*stepFault) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// newCrashApp is newApp whose router runs on a pool with the fault attached.
// One pool with the fault attached serves every world of a sweep, so the
// sweep does not open and close hundreds of connections.
var (
	crashFault    = &stepFault{}
	crashPoolOnce sync.Once
	crashPool     *db.Pool
	crashPoolErr  error
)

func faultyPool(t *testing.T, source *db.Pool) *db.Pool {
	t.Helper()
	crashPoolOnce.Do(func() {
		config := source.Config().Copy()
		config.ConnConfig.Tracer = crashFault
		config.MinConns = 0
		var p *pgxpool.Pool
		p, crashPoolErr = pgxpool.NewWithConfig(context.Background(), config)
		crashPool = &db.Pool{Pool: p}
	})
	if crashPoolErr != nil {
		t.Fatal(crashPoolErr)
	}
	return crashPool
}

func newCrashApp(t *testing.T) *app {
	t.Helper()
	pool := testutil.DB(t)
	testutil.Reset(t, pool)
	faulty := faultyPool(t, pool)
	var err error
	a := &app{t: t, db: pool, store: media.NewMemoryStore(), events: &events.Recorder{}}
	a.srv = httptest.NewUnstartedServer(nil)
	a.srv.Config.ConnContext = func(ctx context.Context, _ net.Conn) context.Context {
		return context.WithValue(ctx, requestMark{}, true)
	}
	cfg := crashConfig(t, "http://"+a.srv.Listener.Addr().String())
	a.router, err = NewRouter(Deps{Config: cfg, DB: faulty, Store: a.store, Events: a.events})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.router.Close)
	a.srv.Config.Handler = a.router
	a.srv.Start()
	t.Cleanup(a.srv.Close)
	return a
}

func crashConfig(t *testing.T, url string) *config.Config {
	t.Helper()
	cfg, err := config.FromEnv(func(k string) (string, bool) {
		v, ok := map[string]string{
			"DATABASE_URL": "unused", "PUBLIC_URL": url,
			"JWT_SECRET": integrationSecret, "MEDIA_RETENTION_DAYS": "0",
		}[k]
		return v, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// tableCounts is the row count of every application table. user_sessions is
// left out: the first request of a sign-in records its session in a write of
// its own, which is not part of any operation here.
func (a *app) tableCounts() map[string]int {
	a.t.Helper()
	ctx := context.Background()
	rows, err := a.db.Query(ctx, `SELECT table_name FROM information_schema.tables
		WHERE table_schema = 'public' AND table_type = 'BASE TABLE' AND table_name NOT IN ('schema_migrations', 'user_sessions') ORDER BY 1`)
	if err != nil {
		a.t.Fatal(err)
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		a.t.Fatal(err)
	}
	out := make(map[string]int, len(names))
	for _, name := range names {
		out[name] = a.count(`SELECT COUNT(*) FROM ` + pgx.Identifier{name}.Sanitize())
	}
	return out
}

// crashOp is one operation: setup builds the world through the routes (not
// counted), run is the request under test.
type crashOp struct {
	name  string
	setup func(a *app) func() response
}

// partialTables names the tables whose counts after a failure match the
// success state while others match the state before.
func partialTables(before, failed, success map[string]int) (moved, stayed []string) {
	for name, b := range before {
		s, f := success[name], failed[name]
		switch {
		case b == s:
		case f == s:
			moved = append(moved, name)
		case f == b:
			stayed = append(stayed, name)
		default:
			moved = append(moved, fmt.Sprintf("%s(%d, before %d, success %d)", name, f, b, s))
		}
	}
	slices.Sort(moved)
	slices.Sort(stayed)
	return moved, stayed
}

// partialClass sorts a partial write: "error after commit" when the change
// stayed but the answer was a server error, "audit" when only the audit entry
// is missing after an answer of success, "other" otherwise.
func partialClass(status int, stayed []string) string {
	switch {
	case status >= 500:
		return "error after commit"
	case len(stayed) == 1 && stayed[0] == "audit_log":
		return "audit"
	default:
		return "other"
	}
}

// partialWrite is one failing statement that left a mix of states.
type partialWrite struct {
	key, text string
}

// sweepCrashPoints runs op once without a fault to learn the statements and
// the success state, then once per statement with that statement failing,
// each on a fresh database. It returns the partial writes it saw.
func sweepCrashPoints(t *testing.T, op crashOp) []partialWrite {
	t.Helper()
	a := newCrashApp(t)
	run := op.setup(a)
	crashFault.arm(0)
	if res := run(); res.status >= 300 {
		t.Fatalf("%s without a fault: %d %s", op.name, res.status, res.body)
	}
	time.Sleep(50 * time.Millisecond)
	_, statements := crashFault.disarm()
	success := a.tableCounts()
	if len(statements) == 0 {
		t.Fatalf("%s: no statement was counted, the sweep would test nothing", op.name)
	}
	var partial []partialWrite
	fired := 0
	for k := 1; k <= len(statements); k++ {
		// A subtest per k, so the router of each world closes before the
		// next one opens.
		t.Run(fmt.Sprint(k), func(t *testing.T) {
			a := newCrashApp(t)
			run := op.setup(a)
			before := a.tableCounts()
			crashFault.arm(k)
			res := run()
			time.Sleep(20 * time.Millisecond)
			hit, _ := crashFault.disarm()
			if hit == "" {
				return
			}
			fired++
			moved, stayed := partialTables(before, a.tableCounts(), success)
			// A mix of states, or the whole change behind an error answer.
			if len(moved) > 0 && (len(stayed) > 0 || res.status >= 400) {
				partial = append(partial, partialWrite{
					key: op.name + " | " + partialClass(res.status, stayed),
					text: fmt.Sprintf("%s: statement %d (%.80s) failed, answer %d: changed %v, unchanged %v",
						op.name, k, hit, res.status, moved, stayed),
				})
			}
		})
	}
	t.Logf("crash sweep %s: %d statements, %d faults fired, %d partial", op.name, len(statements), fired, len(partial))
	if fired*2 < len(statements) {
		t.Fatalf("%s: only %d of %d faults fired", op.name, fired, len(statements))
	}
	return partial
}

var crashOps = []crashOp{
	{"post a message", func(a *app) func() response {
		admin := a.seedAdmin()
		ch := a.createChannel(admin, "general", "text")
		return func() response {
			return admin.post("/api/channels/"+ch.String()+"/messages", map[string]any{"content": "hello"})
		}
	}},
	{"start a conversation", func(a *app) func() response {
		admin := a.seedAdmin()
		alice, bob := a.register(admin, "alice"), a.register(admin, "bobby")
		return func() response { return alice.startDM(bob) }
	}},
	{"register with an invite", func(a *app) func() response {
		admin := a.seedAdmin()
		inv := admin.post("/api/admin/invites", map[string]any{})
		var code struct{ Code string }
		inv.decode(a.t, &code)
		return func() response {
			return a.anon().post("/api/auth/register", map[string]string{"username": "newbie", "password": memberPassword, "invite_code": code.Code})
		}
	}},
	{"create a channel", func(a *app) func() response {
		admin := a.seedAdmin()
		return func() response {
			return admin.post("/api/admin/channels", map[string]any{"name": "lounge", "type": "text"})
		}
	}},
	{"delete a message", func(a *app) func() response {
		admin := a.seedAdmin()
		ch := a.createChannel(admin, "general", "text")
		msg := a.send(admin, ch, "bye")
		return func() response { return admin.delete("/api/channels/" + ch.String() + "/messages/" + msg.String()) }
	}},
	{"delete an account", func(a *app) func() response {
		admin := a.seedAdmin()
		alice := a.register(admin, "alice")
		ch := a.createChannel(admin, "general", "text")
		msg := a.send(alice, ch, "mine")
		alice.post("/api/messages/"+msg.String()+"/reactions", map[string]string{"emoji": "x"})
		return func() response { return a.deleteAccount(alice, memberPassword, "") }
	}},
	{"create a wiki page", func(a *app) func() response {
		admin := a.seedAdmin()
		return func() response {
			return admin.post("/api/wiki/pages", map[string]any{"title": "Rules", "content": "be kind"})
		}
	}},
	{"edit a message", func(a *app) func() response {
		admin := a.seedAdmin()
		ch := a.createChannel(admin, "general", "text")
		msg := a.send(admin, ch, "first words")
		return func() response {
			return admin.put("/api/channels/"+ch.String()+"/messages/"+msg.String(), map[string]any{"content": "second words"})
		}
	}},
	{"react to a message", func(a *app) func() response {
		admin := a.seedAdmin()
		ch := a.createChannel(admin, "general", "text")
		msg := a.send(admin, ch, "react")
		return func() response {
			return admin.post("/api/messages/"+msg.String()+"/reactions", map[string]string{"emoji": "x"})
		}
	}},
	{"pin a message", func(a *app) func() response {
		admin := a.seedAdmin()
		ch := a.createChannel(admin, "general", "text")
		msg := a.send(admin, ch, "pin")
		return func() response {
			return admin.put("/api/channels/"+ch.String()+"/messages/"+msg.String()+"/pin", nil)
		}
	}},
	{"report a message", func(a *app) func() response {
		admin := a.seedAdmin()
		alice := a.register(admin, "alice")
		ch := a.createChannel(admin, "general", "text")
		msg := a.send(admin, ch, "report me")
		return func() response {
			return alice.post("/api/messages/"+msg.String()+"/report", map[string]any{"category": "spam", "note": "n"})
		}
	}},
	{"block a member", func(a *app) func() response {
		admin := a.seedAdmin()
		alice, bob := a.register(admin, "alice"), a.register(admin, "bobby")
		alice.startDM(bob)
		return func() response { return alice.put("/api/me/blocks/"+bob.user.ID.String(), nil) }
	}},
	{"create an event", func(a *app) func() response {
		admin := a.seedAdmin()
		return func() response {
			return admin.post("/api/events", map[string]any{"title": "Raid", "description": "", "start_time": time.Now().Add(48 * time.Hour), "location": "Hall"})
		}
	}},
	{"comment on an event", func(a *app) func() response {
		admin := a.seedAdmin()
		res := admin.post("/api/events", map[string]any{"title": "Raid", "description": "", "start_time": time.Now().Add(48 * time.Hour), "location": "Hall"})
		var ev struct{ ID string }
		res.decode(a.t, &ev)
		return func() response {
			return admin.post("/api/events/"+ev.ID+"/comments", map[string]any{"content": "count me in"})
		}
	}},
	{"ban a member", func(a *app) func() response {
		admin := a.seedAdmin()
		alice := a.register(admin, "alice")
		return func() response {
			return admin.post("/api/admin/users/"+alice.user.ID.String()+"/ban", map[string]any{"reason": "x"})
		}
	}},
	{"time out a member", func(a *app) func() response {
		admin := a.seedAdmin()
		alice := a.register(admin, "alice")
		return func() response {
			return admin.post("/api/admin/users/"+alice.user.ID.String()+"/timeout", map[string]any{"duration_seconds": 600, "reason": "x"})
		}
	}},
	{"change the password", func(a *app) func() response {
		admin := a.seedAdmin()
		alice := a.register(admin, "alice")
		return func() response {
			return alice.put("/api/auth/password", map[string]string{"current_password": memberPassword, "new_password": "another-password-456"})
		}
	}},
	{"open a forum post", func(a *app) func() response {
		admin := a.seedAdmin()
		forum := a.createChannel(admin, "board", "forum")
		return func() response {
			return admin.post("/api/channels/"+forum.String()+"/posts", map[string]any{"title": "post", "content": "body"})
		}
	}},
	{"answer an event", func(a *app) func() response {
		admin := a.seedAdmin()
		res := admin.post("/api/events", map[string]any{"title": "Raid", "description": "", "start_time": time.Now().Add(48 * time.Hour), "location": "Hall"})
		var ev struct{ ID string }
		res.decode(a.t, &ev)
		return func() response {
			return admin.put("/api/events/"+ev.ID+"/rsvp", map[string]string{"response": "going"})
		}
	}},
	{"delete a channel", func(a *app) func() response {
		admin := a.seedAdmin()
		ch := a.createChannel(admin, "general", "text")
		a.send(admin, ch, "goes with it")
		admin.post("/api/admin/channels/"+ch.String()+"/webhooks", map[string]any{"name": "hook"})
		return func() response { return admin.delete("/api/admin/channels/" + ch.String()) }
	}},
	{"delete a role", func(a *app) func() response {
		admin := a.seedAdmin()
		alice := a.register(admin, "alice")
		role := createRole(a.t, admin, "helpers", 0)
		assignRoles(a.t, admin, alice, role)
		return func() response { return admin.delete("/api/admin/roles/" + role.String()) }
	}},
	{"create a webhook", func(a *app) func() response {
		admin := a.seedAdmin()
		ch := a.createChannel(admin, "general", "text")
		return func() response {
			return admin.post("/api/admin/channels/"+ch.String()+"/webhooks", map[string]any{"name": "hook"})
		}
	}},
	{"create a category", func(a *app) func() response {
		admin := a.seedAdmin()
		return func() response { return admin.post("/api/admin/categories", map[string]any{"name": "Area"}) }
	}},
	{"create an invite", func(a *app) func() response {
		admin := a.seedAdmin()
		return func() response { return admin.post("/api/admin/invites", map[string]any{}) }
	}},
	{"create a role", func(a *app) func() response {
		admin := a.seedAdmin()
		return func() response {
			return admin.post("/api/admin/roles", map[string]any{"name": "helpers", "permissions": 0})
		}
	}},
	{"assign a role", func(a *app) func() response {
		admin := a.seedAdmin()
		alice := a.register(admin, "alice")
		role := createRole(a.t, admin, "helpers", 0)
		return func() response { return setRoles(admin, alice.user.ID, role) }
	}},
}

// knownPartial is the checked-in list of partial writes, keyed by operation
// and class (partialClass). It may only shrink: an entry that no longer
// reproduces fails the test, as does a partial write that is not listed.
var knownPartial = map[string]string{
	// H-L7-04: the audit entry is written after the change committed, on its
	// own (audit.RecordOrLog, or the edit's before-and-after read): a failed
	// or killed write leaves a change that answered success with no entry.
	"register with an invite | audit": "H-L7-04",
	"create a channel | audit":        "H-L7-04",
	"delete a message | audit":        "H-L7-04",
	"delete an account | audit":       "H-L7-04",
	"create a wiki page | audit":      "H-L7-04",
	"edit a message | audit":          "H-L7-04",
	"report a message | audit":        "H-L7-04",
	"create an event | audit":         "H-L7-04",
	"create a role | audit":           "H-L7-04",
	"assign a role | audit":           "H-L7-04",
	"ban a member | audit":            "H-L7-04",
	"create an invite | audit":        "H-L7-04",
	"delete a channel | audit":        "H-L7-04",
	"delete a role | audit":           "H-L7-04",
	"create a webhook | audit":        "H-L7-04",
	"create a category | audit":       "H-L7-04",
	// H-L7-05: the result is read back after the commit; a failed read
	// answers 500 for a change that was made (a wiki page also loses its
	// audit entry).
	"create a wiki page | error after commit":   "H-L7-05",
	"post a message | error after commit":       "H-L7-05",
	"start a conversation | error after commit": "H-L7-05",
	"edit a message | error after commit":       "H-L7-05",
	"react to a message | error after commit":   "H-L7-05",
	"comment on an event | error after commit":  "H-L7-05",
	"open a forum post | error after commit":    "H-L7-05",
	"answer an event | error after commit":      "H-L7-05",
	// The webhook's token is only in the answer of its creation.
	"create a webhook | error after commit": "H-L7-05",
}

func TestEveryFailingStatementLeavesAllOrNothing(t *testing.T) {
	ops := crashOps
	if !racetest.Deep() {
		ops = ops[:3]
	}
	seen := map[string]bool{}
	ran := map[string]bool{}
	for _, op := range ops {
		t.Run(op.name, func(t *testing.T) {
			// Only operations that ran (not filtered out by -run) count.
			ran[op.name] = true
			for _, p := range sweepCrashPoints(t, op) {
				finding, ok := knownPartial[p.key]
				if !ok {
					t.Error("partial write not on the known list: " + p.text)
					continue
				}
				seen[p.key] = true
				t.Logf("known failure %s: %s", finding, p.text)
			}
		})
	}
	for key, finding := range knownPartial {
		if name, _, _ := strings.Cut(key, " | "); ran[name] && !seen[key] {
			t.Errorf("known failure %s (%s) no longer reproduces: remove it from the list", finding, key)
		}
	}
}
