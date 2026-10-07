//go:build integration

package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/testutil"
	"golang.org/x/crypto/bcrypt"
)

const expectedPasswordCost = bcrypt.MinCost

func TestMain(m *testing.M) {
	SetPasswordCostForTests(expectedPasswordCost)
	os.Exit(testutil.Main(m))
}

func authFixture(t *testing.T) (*db.Pool, *User, *User) {
	t.Helper()
	p := testutil.DB(t)
	testutil.Reset(t, p)
	ctx := context.Background()
	if err := EnsureAdminUser(ctx, p, "Herzog", testPassword, true); err != nil {
		t.Fatal(err)
	}
	admin, _, err := Login(ctx, p, "Herzog", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreateInvite(ctx, p, admin.ID, "member-invite", nil, nil); err != nil {
		t.Fatal(err)
	}
	member, err := Register(ctx, p, "member", "", testPassword, "member-invite")
	if err != nil {
		t.Fatal(err)
	}
	return p, admin, member
}

// authQueryHook uses pgx's public tracing contract to coordinate a real
// concurrent change or cancel a real database query at a specific boundary.
// It does not replace the database, rows, errors or production auth functions.
type authQueryHook struct {
	match  string
	once   sync.Once
	before func()
	cancel bool
	hit    bool
}

func (h *authQueryHook) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if strings.Contains(strings.Join(strings.Fields(d.SQL), " "), h.match) {
		h.once.Do(func() {
			h.hit = true
			if h.before != nil {
				h.before()
			}
			if h.cancel {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
		})
	}
	return ctx
}
func (*authQueryHook) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func authHookPool(t *testing.T, source *db.Pool, hook *authQueryHook) *db.Pool {
	t.Helper()
	config := source.Config().Copy()
	config.ConnConfig.Tracer = hook
	config.MinConns = 0
	p, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	t.Cleanup(func() {
		if !hook.hit {
			t.Errorf("query hook did not reach %q", hook.match)
		}
	})
	return &db.Pool{Pool: p}
}

func authRequest(t *testing.T, h httpx.HandlerFunc, user *User, body string, id uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	r = r.WithContext(WithUser(r.Context(), user))
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", id.String())
	rc.URLParams.Add("userID", id.String())
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rc))
	w := httptest.NewRecorder()
	httpx.Handle(h).ServeHTTP(w, r)
	return w
}

func TestAuthDatabaseCancellationPropagates(t *testing.T) {
	p, admin, member := authFixture(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name, query string
		call        func(*db.Pool) error
	}{
		{"seed admin lookup", "SELECT EXISTS (SELECT 1 FROM users WHERE role = 'admin')", func(p *db.Pool) error { return EnsureAdminUser(ctx, p, "Herzog", testPassword, true) }},
		{"login", "FROM users WHERE LOWER(username)", func(p *db.Pool) error { _, _, e := Login(ctx, p, member.Username, testPassword); return e }},
		{"register invite precheck", "SELECT EXISTS (SELECT 1 FROM invites", func(p *db.Pool) error {
			_, e := Register(ctx, p, "newmember", "", testPassword, "member-invite")
			return e
		}},
		{"register locked invite lookup", "FROM invites WHERE code = $1 FOR UPDATE", func(p *db.Pool) error {
			_, e := Register(ctx, p, "newmember", "", testPassword, "member-invite")
			return e
		}},
		{"register username lookup", "SELECT EXISTS (SELECT 1 FROM users WHERE LOWER(username)", func(p *db.Pool) error {
			_, e := Register(ctx, p, "newmember", "", testPassword, "member-invite")
			return e
		}},
		{"register insert", "INSERT INTO users (username, display_name, password_hash, role)", func(p *db.Pool) error {
			_, e := Register(ctx, p, "newmember", "", testPassword, "member-invite")
			return e
		}},
		{"register invite increment", "UPDATE invites SET uses_count", func(p *db.Pool) error {
			_, e := Register(ctx, p, "newmember", "", testPassword, "member-invite")
			return e
		}},
		{"change password load", "SELECT password_hash FROM users", func(p *db.Pool) error {
			_, e := ChangePassword(ctx, p, member.ID, testPassword, "next-password-value")
			return e
		}},
		{"password confirmation load", "SELECT password_hash FROM users", func(p *db.Pool) error { return CheckUserPassword(ctx, p, member.ID, testPassword) }},
		{"list invitations", "FROM invites ORDER BY", func(p *db.Pool) error { _, e := ListInvites(ctx, p); return e }},
		{"create invitation", "INSERT INTO invites", func(p *db.Pool) error { _, e := CreateInvite(ctx, p, admin.ID, "cancelled-invite", nil, nil); return e }},
		{"delete invitation", "DELETE FROM invites", func(p *db.Pool) error { return DeleteInvite(ctx, p, uuid.New()) }},
		{"list admin users", "FROM users ORDER BY", func(p *db.Pool) error { _, e := ListUsersForAdmin(ctx, p); return e }},
		{"reset password write", "UPDATE users SET password_hash", func(p *db.Pool) error { _, e := ResetPassword(ctx, p, member.ID); return e }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hook := &authQueryHook{match: tc.query, cancel: true}
			fault := authHookPool(t, p, hook)
			if err := tc.call(fault); !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v, want context cancellation", err)
			}
		})
	}
	// A failed invite increment must roll back the newly inserted user too.
	var exists bool
	if err := p.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE username='newmember')`).Scan(&exists); err != nil || exists {
		t.Fatalf("registration rollback: exists=%v err=%v", exists, err)
	}
}

func TestAuthHandlersPropagateDatabaseCancellation(t *testing.T) {
	p, admin, member := authFixture(t)
	for _, tc := range []struct {
		name, query, body string
		handler           func(*Handler) httpx.HandlerFunc
		id                uuid.UUID
	}{
		{"logout all", "UPDATE users SET token_version", "", func(h *Handler) httpx.HandlerFunc { return h.logoutAll }, member.ID},
		{"locale", "UPDATE users SET locale", `{"locale":"en"}`, func(h *Handler) httpx.HandlerFunc { return h.setLocale }, member.ID},
		{"profile", "UPDATE users SET display_name", `{"display_name":"new"}`, func(h *Handler) httpx.HandlerFunc { return h.updateProfile }, member.ID},
		{"presence", "UPDATE users SET presence", `{"presence":"away"}`, func(h *Handler) httpx.HandlerFunc { return h.setPresence }, member.ID},
		{"status", "UPDATE users SET status_text", `{"status_text":"busy"}`, func(h *Handler) httpx.HandlerFunc { return h.setStatus }, member.ID},
		{"profile load", "FROM users WHERE id = $1", "", func(h *Handler) httpx.HandlerFunc { return h.getUser }, member.ID},
		{"profile stats", "SELECT voice_seconds", "", func(h *Handler) httpx.HandlerFunc { return h.getUser }, member.ID},
		{"invitation list", "FROM invites ORDER BY", "", func(h *Handler) httpx.HandlerFunc { return h.listInvites }, member.ID},
		{"admin list", "FROM users ORDER BY", "", func(h *Handler) httpx.HandlerFunc { return h.listUsers }, member.ID},
		{"disable", "UPDATE users SET disabled_at", "", func(h *Handler) httpx.HandlerFunc { return h.disableUser }, member.ID},
		{"enable", "UPDATE users SET disabled_at", "", func(h *Handler) httpx.HandlerFunc { return h.enableUser }, member.ID},
		{"revoke target load", "FROM users WHERE id = $1", "", func(h *Handler) httpx.HandlerFunc { return h.revokeUserSessions }, member.ID},
		{"revoke write", "UPDATE users SET token_version", "", func(h *Handler) httpx.HandlerFunc { return h.revokeUserSessions }, member.ID},
		{"admin set password", "UPDATE users SET password_hash", `{"password":"replacement-password"}`, func(h *Handler) httpx.HandlerFunc { return h.setUserPassword }, member.ID},
		{"admin reset password", "UPDATE users SET password_hash", "", func(h *Handler) httpx.HandlerFunc { return h.resetUserPassword }, member.ID},
		{"confirm password", "SELECT password_hash FROM users", "", nil, member.ID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fault := authHookPool(t, p, &authQueryHook{match: tc.query, cancel: true})
			h := NewHandler(&Sessions{DB: fault, Secret: secret, TTL: time.Hour}, &events.Recorder{})
			if tc.handler == nil {
				if wait, err := h.ConfirmPassword(context.Background(), member.ID, testPassword); wait != 0 || !errors.Is(err, context.Canceled) {
					t.Fatalf("confirmation = %v, %v", wait, err)
				}
				return
			}
			w := authRequest(t, tc.handler(h), admin, tc.body, tc.id)
			if w.Code != http.StatusInternalServerError {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if len(w.Result().Cookies()) != 0 {
				t.Fatal("failed operation changed session cookie")
			}
		})
	}
}

func TestAuthSessionFailurePaths(t *testing.T) {
	p, _, member := authFixture(t)
	s := &Sessions{DB: p, Secret: secret, TTL: time.Hour}
	for _, value := range []string{"", "not-a-token"} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(&http.Cookie{Name: s.CookieName(), Value: value})
		if _, _, err := s.AuthenticateRequest(r); err != errNoSession {
			t.Fatalf("bad cookie=%q err=%v", value, err)
		}
	}
	token, err := IssueToken(member.ID, 0, secret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: s.CookieName(), Value: token})
	fault := authHookPool(t, p, &authQueryHook{match: "FROM users WHERE id = $1 AND disabled_at IS NULL", cancel: true})
	s.DB = fault
	if _, _, err := s.AuthenticateRequest(r); !errors.Is(err, context.Canceled) {
		t.Fatalf("auth db error = %v", err)
	}
	s.DB = p
	// Use a second traced pool: the first hook intentionally fires only once.
	s.DB = authHookPool(t, p, &authQueryHook{match: "FROM users WHERE id = $1 AND disabled_at IS NULL", cancel: true})
	w := httptest.NewRecorder()
	s.RequireUser(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("failed authentication reached handler") })).ServeHTTP(w, r)
	if w.Code != 500 {
		t.Fatalf("middleware db error status=%d", w.Code)
	}
	if err := CheckUserPassword(context.Background(), p, uuid.New(), testPassword); err != ErrWrongPassword {
		t.Fatalf("absent password check = %v", err)
	}
	if err := SetDisabled(context.Background(), p, member.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := CheckUserPassword(context.Background(), p, member.ID, testPassword); err != ErrWrongPassword {
		t.Fatalf("disabled password check = %v", err)
	}
}

func TestAuthNotFoundAndConflicts(t *testing.T) {
	p, admin, member := authFixture(t)
	ctx := context.Background()
	for name, call := range map[string]func() error{
		"locale":              func() error { _, e := SetLocale(ctx, p, uuid.New(), "en"); return e },
		"presence":            func() error { _, e := SetPresence(ctx, p, uuid.New(), PresenceAway); return e },
		"status":              func() error { _, e := SetStatusText(ctx, p, uuid.New(), "hello"); return e },
		"reset":               func() error { _, e := ResetPassword(ctx, p, uuid.New()); return e },
		"set password target": func() error { return SetPassword(ctx, p, uuid.New(), testPassword) },
	} {
		t.Run(name, func(t *testing.T) {
			e, ok := httpx.AsAPIError(call())
			if !ok || e.Status != 404 {
				t.Fatalf("error=%+v want404", e)
			}
		})
	}
	if err := SetPassword(ctx, p, member.ID, "short"); err == nil {
		t.Fatal("short password accepted")
	}
	if _, err := Register(ctx, p, "newmember", "", testPassword, "unknown-code"); err != ErrInvalidInvite {
		t.Fatalf("unknown invite = %v", err)
	}
	if _, err := Register(ctx, p, "MEMBER", "", testPassword, "member-invite"); err != ErrUsernameTaken {
		t.Fatalf("case-insensitive conflict = %v", err)
	}
	if err := SetPassword(ctx, p, admin.ID, testPassword); err == nil {
		t.Fatal("admin password target accepted")
	}
}

func TestAuthRegistrationConcurrentChanges(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, query string
		change      func(*testing.T, *db.Pool)
		want        error
	}{
		{"invite removed after precheck", "FROM invites WHERE code = $1 FOR UPDATE", func(t *testing.T, p *db.Pool) {
			if _, e := p.Exec(ctx, `DELETE FROM invites WHERE code='member-invite'`); e != nil {
				t.Fatal(e)
			}
		}, ErrInvalidInvite},
		{"username inserted after check", "INSERT INTO users (username, display_name, password_hash, role)", func(t *testing.T, p *db.Pool) {
			if _, e := p.Exec(ctx, `INSERT INTO users(username,display_name,password_hash,role) VALUES('newmember','newmember','test-only-hash','user')`); e != nil {
				t.Fatal(e)
			}
		}, ErrUsernameTaken},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, _, _ := authFixture(t)
			hook := &authQueryHook{match: tc.query, before: func() { tc.change(t, p) }}
			fault := authHookPool(t, p, hook)
			u, e := Register(ctx, fault, "newmember", "", testPassword, "member-invite")
			if u != nil || e != tc.want {
				t.Fatalf("register = %+v, %v want %v", u, e, tc.want)
			}
		})
	}
}

func TestResetPasswordUserRemovedBeforeWrite(t *testing.T) {
	p, _, member := authFixture(t)
	fault := authHookPool(t, p, &authQueryHook{match: "UPDATE users SET password_hash", before: func() {
		if _, e := p.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, member.ID); e != nil {
			t.Fatal(e)
		}
	}})
	password, err := ResetPassword(context.Background(), fault, member.ID)
	api, ok := httpx.AsAPIError(err)
	if password != "" || !ok || api.Status != 404 {
		t.Fatalf("reset after deletion = %q, %v", password, err)
	}
}

func TestAdminSeedDatabaseFailuresAndGeneratedPassword(t *testing.T) {
	p, admin, _ := authFixture(t)
	ctx := context.Background()
	// Existing administrator ignores a now-invalid seed configuration.
	if err := EnsureAdminUser(ctx, p, "bad name", "short", true); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Exec(ctx, `DELETE FROM users WHERE id=$1`, admin.ID); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"SELECT username FROM users WHERE LOWER(username)", "INSERT INTO users (username, display_name, password_hash, role)"} {
		t.Run(query, func(t *testing.T) {
			fault := authHookPool(t, p, &authQueryHook{match: query, cancel: true})
			if e := EnsureAdminUser(ctx, fault, "newadmin", testPassword, true); !errors.Is(e, context.Canceled) {
				t.Fatalf("seed failure = %v", e)
			}
		})
	}
	fault := authHookPool(t, p, &authQueryHook{match: "INSERT INTO users (username, display_name, password_hash, role)", before: func() {
		if _, e := p.Exec(ctx, `INSERT INTO users(username,display_name,password_hash,role) VALUES('newadmin','newadmin','test-only-hash','user')`); e != nil {
			t.Fatal(e)
		}
	}})
	if e := EnsureAdminUser(ctx, fault, "newadmin", testPassword, true); e == nil || !strings.Contains(e.Error(), "taken by a regular user") {
		t.Fatalf("concurrent seed conflict = %v", e)
	}
	// Keep development's one-time generated password out of the test logs.
	// Its stored hash and generated-password branch are tested separately by
	// a capture logger below.
	var captured bytes.Buffer
	logger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&captured, nil)))
	t.Cleanup(func() { slog.SetDefault(logger) })
	if err := EnsureAdminUser(ctx, p, "generated-admin", "", false); err != nil {
		t.Fatal(err)
	}
	var generated string
	for _, line := range bytes.Split(bytes.TrimSpace(captured.Bytes()), []byte("\n")) {
		var record map[string]any
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatal("generated seed log was not JSON")
		}
		if value, ok := record["password"].(string); ok {
			generated = value
		}
	}
	if len(generated) != 24 {
		t.Fatal("development seed did not emit a random 24-character password")
	}
	if _, _, err := Login(ctx, p, "generated-admin", generated); err != nil {
		t.Fatal("generated password did not authenticate")
	}
	captured.Reset()
	if err := EnsureAdminUser(ctx, p, "generated-admin", "", false); err != nil {
		t.Fatal(err)
	}
	if captured.Len() != 0 {
		t.Fatal("seed emitted credentials again after administrator existed")
	}
}

func TestAdminKickRejectsMissingOrDisconnectedUser(t *testing.T) {
	p, admin, member := authFixture(t)
	h := NewHandler(&Sessions{DB: p}, &events.Recorder{})
	for _, tc := range []struct {
		id     uuid.UUID
		status int
	}{{uuid.New(), 404}, {member.ID, 400}} {
		w := authRequest(t, h.kickUser, admin, "", tc.id)
		if w.Code != tc.status {
			t.Fatalf("kick status=%d want%d", w.Code, tc.status)
		}
	}
	// Failed validation must return an API error and retain the current cookie.
	w := authRequest(t, h.changePassword, member, `{"current_password":"unused","new_password":"short"}`, member.ID)
	if w.Code != 400 || len(w.Result().Cookies()) != 0 {
		t.Fatalf("password validation status=%d cookies=%d", w.Code, len(w.Result().Cookies()))
	}
}

func TestPasswordCostHookRejectsOutOfRangeValues(t *testing.T) {
	original := PasswordCost()
	for _, cost := range []int{bcrypt.MinCost - 1, bcrypt.MaxCost + 1} {
		t.Run("invalid cost", func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("invalid cost did not panic")
				}
				if PasswordCost() != original {
					t.Error("invalid cost changed active cost")
				}
			}()
			SetPasswordCostForTests(cost)
		})
	}
}

func TestAuthListRejectsIncompatibleDatabaseColumnTypes(t *testing.T) {
	p, _, _ := authFixture(t)
	ctx := context.Background()
	// A mismatched schema can return a UUID-looking column with invalid data.
	// Shadow only this connection's search path so the fixture never alters
	// application tables or relies on invalid records bypassing constraints.
	_, err := p.Exec(ctx, `CREATE SCHEMA auth_scan_fixture;
		CREATE VIEW auth_scan_fixture.invites AS SELECT
		'invalid-uuid'::text AS id, 'testcode'::text AS code, NULL::int AS max_uses,
		0::int AS uses_count, NULL::timestamptz AS expires_at, NOW() AS created_at;
		CREATE VIEW auth_scan_fixture.users AS SELECT
		'invalid-uuid'::text AS id, 'testuser'::text AS username, 'testuser'::text AS display_name,
		''::text AS bio, 'user'::text AS role, NULL::text AS avatar_s3_key, ''::text AS status_text,
		'online'::text AS presence, ''::text AS locale, NOW() AS created_at,
		NULL::timestamptz AS disabled_at, NULL::timestamptz AS last_seen_at`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := p.Exec(ctx, `DROP SCHEMA auth_scan_fixture CASCADE`); err != nil {
			t.Error(err)
		}
	})
	config := p.Config().Copy()
	config.ConnConfig.RuntimeParams["search_path"] = "auth_scan_fixture,public"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	fault := &db.Pool{Pool: pool}
	if rows, err := ListInvites(ctx, fault); rows != nil || err == nil || !strings.Contains(err.Error(), "invalid UUID") {
		t.Fatalf("incompatible invite scan = %v, %v", rows, err)
	}
	if rows, err := ListUsersForAdmin(ctx, fault); rows != nil || err == nil || !strings.Contains(err.Error(), "invalid UUID") {
		t.Fatalf("incompatible user scan = %v, %v", rows, err)
	}
}

func TestAuthHashEntropyFailuresPreserveAccounts(t *testing.T) {
	p, admin, member := authFixture(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name      string
		available int
		call      func() error
	}{
		{"register", 0, func() error {
			_, err := Register(ctx, p, "entropy-member", "", testPassword, "member-invite")
			return err
		}},
		{"change password", 0, func() error {
			_, err := ChangePassword(ctx, p, member.ID, testPassword, "next-password-value")
			return err
		}},
		{"set password", 0, func() error { return SetPassword(ctx, p, member.ID, "next-password-value") }},
		// rand.Read gets exactly enough entropy for a 15-byte temporary
		// password; bcrypt's subsequent 16-byte salt read encounters EOF.
		{"reset password", 15, func() error { _, err := ResetPassword(ctx, p, member.ID); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			func() {
				original := rand.Reader
				defer func() { rand.Reader = original }()
				rand.Reader = bytes.NewReader(make([]byte, tc.available))
				if err := tc.call(); err == nil || !strings.Contains(err.Error(), "hash password") {
					t.Fatalf("entropy failure = %v", err)
				}
			}()
			if _, tv, err := Login(ctx, p, member.Username, testPassword); err != nil || tv != 0 {
				t.Fatalf("failed hash changed account: tv=%d err=%v", tv, err)
			}
		})
	}
	if _, err := p.Exec(ctx, `DELETE FROM users WHERE id=$1`, admin.ID); err != nil {
		t.Fatal(err)
	}
	func() {
		original := rand.Reader
		defer func() { rand.Reader = original }()
		rand.Reader = bytes.NewReader(nil)
		if err := EnsureAdminUser(ctx, p, "entropy-admin", testPassword, true); err == nil || !strings.Contains(err.Error(), "hash admin password") {
			t.Fatalf("seed entropy failure = %v", err)
		}
	}()
	var exists bool
	if err := p.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE username IN ('entropy-member','entropy-admin'))`).Scan(&exists); err != nil || exists {
		t.Fatalf("failed hash created account: exists=%v err=%v", exists, err)
	}
}
