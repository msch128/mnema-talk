//go:build integration

package bot

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/media"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/testutil"
	"github.com/msch128/mnema-talk/internal/testutil/pgfault"
)

func TestMain(m *testing.M) { os.Exit(testutil.Main(m)) }

var png = []byte{
	0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0x0d, 'I', 'H', 'D', 'R', 0, 0, 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0,
	0x1f, 0x15, 0xc4, 0x89, 0, 0, 0, 0x0d, 'I', 'D', 'A', 'T', 0x78, 0x9c, 0x63, 0xf8, 0xcf, 0xc0, 0xf0, 0x1f, 0, 5, 0, 1, 0xff,
	0x89, 0x99, 0x3d, 0x1d, 0, 0, 0, 0, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82,
}

type env struct {
	t     *testing.T
	p     *db.Pool
	owner *auth.User
	ch    uuid.UUID
	more  []uuid.UUID
	role  uuid.UUID
	store *media.MemoryStore
}

func newEnv(t *testing.T) *env {
	t.Helper()
	p := testutil.DB(t)
	testutil.Reset(t, p)
	e := &env{t: t, p: p, store: media.NewMemoryStore()}
	var id uuid.UUID
	if err := p.QueryRow(context.Background(), `
		INSERT INTO users (username, display_name, password_hash, role) VALUES ('owner', 'owner', 'x', 'admin') RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	e.owner = &auth.User{ID: id, Role: "admin"}
	if err := p.QueryRow(context.Background(), `INSERT INTO channels (name, type) VALUES ('c', 'text') RETURNING id`).Scan(&e.ch); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"c", "d"} {
		var other uuid.UUID
		if err := p.QueryRow(context.Background(), `INSERT INTO channels (name, type) VALUES ($1, 'text') RETURNING id`, name).Scan(&other); err != nil {
			t.Fatal(err)
		}
		e.more = append(e.more, other)
	}
	if err := p.QueryRow(context.Background(), `INSERT INTO roles (name, position, permissions) VALUES ('bots', 1, $1) RETURNING id`, int64(perm.SendMessages)).Scan(&e.role); err != nil {
		t.Fatal(err)
	}
	return e
}

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

func (e *env) handler(pool *db.Pool) *Handler {
	m := &media.Handler{DB: pool, Store: e.store, Bucket: "b", MaxUploadBytes: 1 << 20, Events: &events.Recorder{}}
	return &Handler{DB: pool, Perms: perm.NewService(pool), Media: m,
		OnRevoke: func(uuid.UUID, *uuid.UUID) {}}
}

func (e *env) serve(h *Handler, method, path string, body io.Reader, hdr map[string]string) *httptest.ResponseRecorder {
	return e.serveWith(h, true, method, path, body, hdr)
}

// serveWith routes to the handlers behind Manage bots (guarded), or straight
// to them, so that their own permission lookups are reached by a failure.
func (e *env) serveWith(h *Handler, guarded bool, method, path string, body io.Reader, hdr map[string]string) *httptest.ResponseRecorder {
	r := chi.NewRouter()
	r.Route("/admin", func(a chi.Router) {
		a.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), e.owner)))
			})
		})
		if guarded {
			h.MountAdmin(a)
			h.MountAdminUploads(a)
			return
		}
		h.mountUnguarded(a)
	})
	req := httptest.NewRequest(method, path, body)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func jsonBody(s string) (io.Reader, map[string]string) {
	return strings.NewReader(s), map[string]string{"Content-Type": "application/json"}
}

func avatarBody() (io.Reader, map[string]string) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="avatar"; filename="a.png"`)
	h.Set("Content-Type", "image/png")
	part, _ := mw.CreatePart(h)
	_, _ = part.Write(png)
	_ = mw.Close()
	return &buf, map[string]string{"Content-Type": mw.FormDataContentType()}
}

// fixtureBot is a bot with a role, a token and a command.
type fixtureBot struct {
	bot   *Bot
	token Token
	cmd   Command
}

func (e *env) newBot() fixtureBot {
	e.t.Helper()
	ctx := context.Background()
	if _, err := e.p.Exec(ctx, `DELETE FROM users WHERE is_bot`); err != nil {
		e.t.Fatal(err)
	}
	b, err := Create(ctx, e.p, "helper", 0, e.owner.ID)
	if err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.p.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2)`, b.UserID, e.role); err != nil {
		e.t.Fatal(err)
	}
	for _, ch := range append([]uuid.UUID{e.ch}, e.more...) {
		if _, err := e.p.Exec(ctx, `INSERT INTO permission_overwrites (channel_id, role_id, allow) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, ch, e.role, int64(perm.ViewChannel)); err != nil {
			e.t.Fatal(err)
		}
	}
	tok, _, err := IssueToken(ctx, e.p, b.ID, "t", nil, uuid.Nil)
	if err != nil {
		e.t.Fatal(err)
	}
	cmd, err := UpsertCommand(ctx, e.p, b.ID, CommandInput{Name: "ping", Description: "pong", Options: []Option{}})
	if err != nil {
		e.t.Fatal(err)
	}
	return fixtureBot{bot: b, token: tok, cmd: cmd}
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

func assertTokenStatesAndExpiry(t *testing.T, ctx context.Context, e *env, f fixtureBot) {
	t.Helper()
	if _, err := e.p.Exec(ctx, `INSERT INTO bot_tokens (bot_id, token_hash, hint, expires_at) VALUES ($1, $2, 'oldx', NOW() - INTERVAL '1 hour')`, f.bot.ID, HashToken("expired")); err != nil {
		t.Fatal(err)
	}
	toks, err := Tokens(ctx, e.p, f.bot.ID)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]bool{}
	for _, tk := range toks {
		states[tk.State] = true
	}
	if !states["expired"] || !states["active"] {
		t.Fatalf("token states: %v", states)
	}
	now := time.Now()
	if CheckExpiry(now.Add(-time.Second), now) == nil {
		t.Fatal("CheckExpiry accepted past expiry")
	}
	if CheckExpiry(now.Add(MaxTokenLifetime+time.Hour), now) == nil {
		t.Fatal("CheckExpiry accepted excessive expiry")
	}
	if CheckExpiry(now.Add(time.Hour), now) != nil {
		t.Fatal("CheckExpiry rejected valid 1-hour expiry")
	}
}

func assertCancelledContextFailsStatements(t *testing.T, ctx context.Context, e *env, f fixtureBot) {
	t.Helper()
	dead, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := Create(dead, e.p, "x", 0, e.owner.ID); err == nil {
		t.Fatal("create")
	}
	if _, err := List(dead, e.p); err == nil {
		t.Fatal("list")
	}
	if _, err := Tokens(dead, e.p, f.bot.ID); err == nil {
		t.Fatal("tokens")
	}
	if err := RevokeToken(dead, e.p, f.bot.ID, f.token.ID); err == nil {
		t.Fatal("revoke")
	}
}

func TestBotStoreEdges(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := Get(ctx, e.p, uuid.New()); err == nil {
		t.Fatal("unknown bot")
	}
	WithRoles([]Bot{{UserID: uuid.New()}}, map[uuid.UUID][]uuid.UUID{})
	f := e.newBot()
	bots, _ := List(ctx, e.p)
	WithRoles(bots, map[uuid.UUID][]uuid.UUID{f.bot.UserID: {e.role}})
	if len(bots[0].RoleIDs) != 1 {
		t.Fatal("roles not filled")
	}
	assertTokenStatesAndExpiry(t, ctx, e, f)
	if err := Update(ctx, e.p, uuid.New(), nil, nil, nil); err == nil {
		t.Fatal("update of an unknown bot")
	}
	if _, err := UpsertCommand(ctx, e.p, uuid.New(), CommandInput{Name: "x", Description: "x"}); err == nil {
		t.Fatal("command for an unknown bot")
	}
	if err := Delete(ctx, e.p, uuid.New()); err == nil {
		t.Fatal("delete of an unknown bot")
	}
	if _, _, err := IssueToken(ctx, e.p, uuid.New(), "x", nil, uuid.Nil); err == nil {
		t.Fatal("token for an unknown bot")
	}
	if err := RevokeToken(ctx, e.p, f.bot.ID, uuid.New()); err == nil {
		t.Fatal("unknown token")
	}
	assertCancelledContextFailsStatements(t, ctx, e, f)
}

func TestManagementRefusals(t *testing.T) {
	e := newEnv(t)
	f := e.newBot()
	h := e.handler(e.p)
	path := "/admin/bots/" + f.bot.ID.String()
	call := func(method, p, body string) int {
		r, hdr := jsonBody(body)
		return e.serve(h, method, p, r, hdr).Code
	}
	for what, c := range map[string]struct {
		method, path, body string
		want               int
	}{
		"bad json":           {"POST", path + "/tokens", `{`, 400},
		"name too long":      {"POST", path + "/tokens", `{"name":"` + strings.Repeat("x", 40) + `"}`, 400},
		"expiry in the past": {"POST", path + "/tokens", `{"expires_at":"2001-01-01T00:00:00Z"}`, 400},
		"unknown token":      {"POST", path + "/tokens/" + uuid.NewString() + "/regenerate", ``, 404},
		"bad token id":       {"DELETE", path + "/tokens/x", ``, 400},
		"bad command id":     {"PATCH", path + "/commands/x", `{"disabled":true}`, 400},
		"bad command body":   {"PATCH", path + "/commands/" + f.cmd.ID.String(), `{`, 400},
		"unknown command":    {"DELETE", path + "/commands/" + uuid.NewString(), ``, 404},
		"bad delete command": {"DELETE", path + "/commands/x", ``, 400},
		"bad regenerate id":  {"POST", path + "/tokens/x/regenerate", ``, 400},
		"unknown bot":        {"GET", "/admin/bots/" + uuid.NewString() + "/access", ``, 404},
		"bad roles body":     {"PUT", path + "/roles", `{`, 400},
		"unknown role":       {"PUT", path + "/roles", `{"role_ids":["` + uuid.NewString() + `"]}`, 400},
		"bad update":         {"PATCH", path, `{"name":" "}`, 400},
		"bad intents":        {"PATCH", path, `{"allowed_intents":-1}`, 400},
		"bad create":         {"POST", "/admin/bots", `{"name":""}`, 400},
		"bad create intents": {"POST", "/admin/bots", `{"name":"x","allowed_intents":2048}`, 400},
		"bad create body":    {"POST", "/admin/bots", `{`, 400},
		"bad update body":    {"PATCH", path, `{`, 400},
	} {
		if got := call(c.method, c.path, c.body); got != c.want {
			t.Errorf("%s: %d, want %d", what, got, c.want)
		}
	}
	h.Media = nil
	body, hdr := avatarBody()
	if w := e.serve(h, "POST", path+"/avatar", body, hdr); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("avatar without storage: %d", w.Code)
	}
	// Too many bots.
	for i := 0; i < MaxBots; i++ {
		if _, err := Create(context.Background(), e.p, "b", 0, e.owner.ID); err != nil {
			break
		}
	}
	if w := e.serve(h, "POST", "/admin/bots", strings.NewReader(`{"name":"one more"}`), map[string]string{"Content-Type": "application/json"}); w.Code != http.StatusConflict {
		t.Fatalf("over the bot limit: %d", w.Code)
	}
}
