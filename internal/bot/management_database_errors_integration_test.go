//go:build integration

package bot

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/testutil/pgfault"
)

// faultPool opens a pool whose statements fail from the skip-th on... exactly
// the (skip+1)-th statement fails.
func (e *env) faultPool(tr *pgfault.Tracer) *db.Pool {
	cfg := e.p.Pool.Config()
	cfg.MaxConns, cfg.MinConns, cfg.MinIdleConns = 1, 0, 0
	cfg.ConnConfig.Tracer = tr
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		e.t.Fatal(err)
	}
	return &db.Pool{Pool: pool}
}

type route struct {
	name   string
	method string
	path   func(fixtureBot) string
	body   func() (io.Reader, map[string]string)
	// ok is the status of the undisturbed call.
	ok int
}

func adminRoutes(e *env) []route {
	base := func(f fixtureBot) string { return "/admin/bots/" + f.bot.ID.String() }
	none := func() (io.Reader, map[string]string) { return nil, nil }
	js := func(s string) func() (io.Reader, map[string]string) {
		return func() (io.Reader, map[string]string) { return jsonBody(s) }
	}
	later := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	return []route{
		{"list", "GET", func(fixtureBot) string { return "/admin/bots" }, none, 200},
		{"create", "POST", func(fixtureBot) string { return "/admin/bots" }, js(`{"name":"new","allowed_intents":1}`), 201},
		{"get", "GET", base, none, 200},
		{"rename", "PATCH", base, js(`{"name":"renamed"}`), 200},
		{"intents", "PATCH", base, js(`{"allowed_intents":512}`), 200},
		{"disable", "PATCH", base, js(`{"disabled":true}`), 200},
		{"delete", "DELETE", base, none, 204},
		{"avatar", "POST", func(f fixtureBot) string { return base(f) + "/avatar" }, avatarBody, 200},
		{"roles", "PUT", func(f fixtureBot) string { return base(f) + "/roles" }, func() (io.Reader, map[string]string) { return jsonBody(fmt.Sprintf(`{"role_ids":["%s"]}`, e.role)) }, 200},
		{"roles cleared", "PUT", func(f fixtureBot) string { return base(f) + "/roles" }, js(`{"role_ids":[]}`), 200},
		{"access", "GET", func(f fixtureBot) string { return base(f) + "/access" }, none, 200},
		{"commands", "GET", func(f fixtureBot) string { return base(f) + "/commands" }, none, 200},
		{"disable command", "PATCH", func(f fixtureBot) string { return base(f) + "/commands/" + f.cmd.ID.String() }, js(`{"disabled":true}`), 200},
		{"delete command", "DELETE", func(f fixtureBot) string { return base(f) + "/commands/" + f.cmd.ID.String() }, none, 204},
		{"tokens", "GET", func(f fixtureBot) string { return base(f) + "/tokens" }, none, 200},
		{"issue", "POST", func(f fixtureBot) string { return base(f) + "/tokens" }, js(`{"name":"x","expires_at":"` + later + `"}`), 201},
		{"regenerate", "POST", func(f fixtureBot) string { return base(f) + "/tokens/" + f.token.ID.String() + "/regenerate" }, none, 201},
		{"revoke", "DELETE", func(f fixtureBot) string { return base(f) + "/tokens/" + f.token.ID.String() }, none, 204},
	}
}

func testRouteStatementFailures(t *testing.T, e *env, rt route, guarded bool) {
	t.Helper()
	for n := 0; ; n++ {
		if n > 60 {
			t.Fatal("more than 60 statements")
		}
		f := e.newBot()
		tr := &pgfault.Tracer{Skip: n}
		pool := e.faultPool(tr)
		h := e.handler(pool)
		body, hdr := rt.body()
		w := e.serveWith(h, guarded, rt.method, rt.path(f), body, hdr)
		pool.Close()
		if !tr.Hit() {
			if w.Code != rt.ok {
				t.Fatalf("no statement failed, yet status %d, want %d: %s", w.Code, rt.ok, w.Body)
			}
			break
		}
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("guarded=%v statement %d failed: status %d, want 500: %s", guarded, n+1, w.Code, w.Body)
		}
	}
}

// Every database statement of the management API can fail; the answer is a
// sanitized 500, never a half answer. (A statement is failed by number: the
// first, the second, ... until the call needs no more.)
func TestManagementDatabaseFailures(t *testing.T) {
	e := newEnv(t)
	for _, rt := range adminRoutes(e) {
		t.Run(rt.name, func(t *testing.T) {
			f := e.newBot()
			body, hdr := rt.body()
			if w := e.serve(e.handler(e.p), rt.method, rt.path(f), body, hdr); w.Code != rt.ok {
				t.Fatalf("undisturbed: %d, want %d: %s", w.Code, rt.ok, w.Body)
			}
			for _, guarded := range []bool{true, false} {
				testRouteStatementFailures(t, e, rt, guarded)
			}
		})
	}
}
