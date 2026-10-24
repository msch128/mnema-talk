//go:build integration

package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
)

type sessionEnv struct {
	t      *testing.T
	p      *db.Pool
	s      *Sessions
	router http.Handler
	admin  *User
	member *User
}

func newSessionEnv(t *testing.T) *sessionEnv {
	t.Helper()
	p, admin, member := authFixture(t)
	s := &Sessions{DB: p, Secret: strings.Repeat("k", 32), TTL: 24 * time.Hour}
	h := NewHandler(s, nil)
	router := chi.NewRouter()
	router.Group(func(r chi.Router) {
		r.Use(s.RequireUser)
		h.MountAuthenticated(r)
	})
	return &sessionEnv{t: t, p: p, s: s, router: router, admin: admin, member: member}
}

// signIn issues a session for the user and makes its first request, which
// puts it on the session list, like a browser does after signing in.
func (e *sessionEnv) signIn(u *User, userAgent string) string {
	e.t.Helper()
	token, err := IssueToken(u.ID, 0, e.s.Secret, e.s.TTL)
	if err != nil {
		e.t.Fatal(err)
	}
	if rec := e.do(token, userAgent, http.MethodGet, "/auth/me"); rec.Code != http.StatusOK {
		e.t.Fatalf("first request: %d %s", rec.Code, rec.Body)
	}
	return token
}

func (e *sessionEnv) do(token, userAgent, method, path string) *httptest.ResponseRecorder {
	e.t.Helper()
	r := httptest.NewRequest(method, path, nil)
	r.AddCookie(&http.Cookie{Name: e.s.CookieName(), Value: token})
	r.Header.Set("User-Agent", userAgent)
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, r)
	return rec
}

func (e *sessionEnv) list(token string) []MemberSession {
	e.t.Helper()
	rec := e.do(token, firefoxLinux, http.MethodGet, "/auth/sessions")
	if rec.Code != http.StatusOK {
		e.t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	var out MemberSessionList
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		e.t.Fatal(err)
	}
	return out.Sessions
}

func (e *sessionEnv) alive(token string) bool {
	_, err := e.s.Authenticate(requestWithToken(e.s, token))
	return err == nil
}

func (e *sessionEnv) jti(token string) string {
	c, err := ParseToken(token, e.s.Secret)
	if err != nil {
		e.t.Fatal(err)
	}
	return c.ID
}

func (e *sessionEnv) idOf(token string) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	if err := e.p.QueryRow(context.Background(), `SELECT id FROM user_sessions WHERE jti = $1`, e.jti(token)).Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	return id
}

func (e *sessionEnv) addPush(user uuid.UUID, token string) {
	e.t.Helper()
	_, err := e.p.Exec(context.Background(), `INSERT INTO push_subscriptions (user_id, endpoint, p256dh, auth, session_id, token_version)
		VALUES ($1, $2, 'k', 'a', $3, 0)`, user, "https://push.example/"+uuid.NewString(), e.jti(token))
	if err != nil {
		e.t.Fatal(err)
	}
}

func (e *sessionEnv) pushCount() int {
	var n int
	if err := e.p.QueryRow(context.Background(), `SELECT COUNT(*) FROM push_subscriptions`).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *sessionEnv) age(token string, by time.Duration) {
	e.t.Helper()
	if _, err := e.p.Exec(context.Background(), `UPDATE user_sessions SET last_active_at = NOW() - make_interval(secs => $2) WHERE jti = $1`,
		e.jti(token), by.Seconds()); err != nil {
		e.t.Fatal(err)
	}
}

func TestSessionListShowsOnlyMyOwnSessions(t *testing.T) {
	e := newSessionEnv(t)
	laptop := e.signIn(e.member, firefoxLinux)
	phone := e.signIn(e.member, safariIPhone)
	other := e.signIn(e.admin, firefoxLinux)
	e.addPush(e.member.ID, phone)

	got := e.list(laptop)
	if len(got) != 2 {
		t.Fatalf("member sees %d sessions, want their 2: %+v", len(got), got)
	}
	byDevice := map[string]MemberSession{}
	for _, s := range got {
		byDevice[s.Device] = s
	}
	if s := byDevice["Firefox · Linux"]; !s.Current || s.Push {
		t.Errorf("laptop: %+v", s)
	}
	if s := byDevice["Safari · iOS"]; s.Current || !s.Push {
		t.Errorf("phone: %+v", s)
	}
	if mine := e.list(other); len(mine) != 1 || !mine[0].Current {
		t.Errorf("the other member sees %+v", mine)
	}
}

func TestSessionOfAnotherMemberCannotBeRevoked(t *testing.T) {
	e := newSessionEnv(t)
	member := e.signIn(e.member, firefoxLinux)
	admin := e.signIn(e.admin, firefoxLinux)
	e.addPush(e.admin.ID, admin)

	if rec := e.do(member, firefoxLinux, http.MethodDelete, "/auth/sessions/"+e.idOf(admin).String()); rec.Code != http.StatusNotFound {
		t.Fatalf("revoking someone else's session: %d, want 404", rec.Code)
	}
	if rec := e.do(member, firefoxLinux, http.MethodDelete, "/auth/sessions/"+uuid.NewString()); rec.Code != http.StatusNotFound {
		t.Fatalf("revoking an unknown session: %d, want 404", rec.Code)
	}
	if rec := e.do(member, firefoxLinux, http.MethodDelete, "/auth/sessions/not-an-id"); rec.Code != http.StatusBadRequest {
		t.Fatalf("revoking a malformed id: %d, want 400", rec.Code)
	}
	if !e.alive(admin) || e.pushCount() != 1 {
		t.Fatal("the other member's session or push subscription was touched")
	}
	// "Sign out all other sessions" never reaches into another account either.
	e.do(member, firefoxLinux, http.MethodPost, "/auth/sessions/revoke-others")
	if !e.alive(admin) || e.pushCount() != 1 || !e.alive(member) {
		t.Fatal("revoke-others crossed accounts or ended the current session")
	}
}

func TestRevokingASessionEndsItAndRemovesItsPushSubscriptions(t *testing.T) {
	e := newSessionEnv(t)
	laptop := e.signIn(e.member, firefoxLinux)
	phone := e.signIn(e.member, safariIPhone)
	e.addPush(e.member.ID, phone)
	e.addPush(e.member.ID, laptop)

	rec := e.do(laptop, firefoxLinux, http.MethodDelete, "/auth/sessions/"+e.idOf(phone).String())
	if rec.Code != http.StatusNoContent {
		t.Fatalf("revoke: %d %s", rec.Code, rec.Body)
	}
	if e.alive(phone) {
		t.Error("the revoked session still works")
	}
	if !e.alive(laptop) || e.pushCount() != 1 {
		t.Error("revoking the phone must leave the laptop and its push subscription")
	}
	if got := e.list(laptop); len(got) != 1 || !got[0].Current {
		t.Errorf("list after revoke: %+v", got)
	}
}

func TestRevokingTheCurrentSessionSignsOut(t *testing.T) {
	e := newSessionEnv(t)
	laptop := e.signIn(e.member, firefoxLinux)

	rec := e.do(laptop, firefoxLinux, http.MethodDelete, "/auth/sessions/"+e.idOf(laptop).String())
	if rec.Code != http.StatusNoContent {
		t.Fatalf("revoke current: %d", rec.Code)
	}
	if c := rec.Result().Cookies(); len(c) != 1 || c[0].MaxAge >= 0 {
		t.Errorf("the cookie was not cleared: %+v", c)
	}
	if e.alive(laptop) {
		t.Error("a signed-out session still works")
	}
}

func TestSignOutAllOtherSessionsKeepsTheCurrentOne(t *testing.T) {
	e := newSessionEnv(t)
	laptop := e.signIn(e.member, firefoxLinux)
	phone := e.signIn(e.member, safariIPhone)
	tablet := e.signIn(e.member, safariIPhone)
	e.addPush(e.member.ID, phone)

	rec := e.do(laptop, firefoxLinux, http.MethodPost, "/auth/sessions/revoke-others")
	var out RevokedSessions
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil || out.Revoked != 2 {
		t.Fatalf("revoke others: %d %s", rec.Code, rec.Body)
	}
	if e.alive(phone) || e.alive(tablet) || !e.alive(laptop) || e.pushCount() != 0 {
		t.Error("expected only the current session to survive, without the others' push subscriptions")
	}
}

func TestSessionsWithoutARowStartTheirInactivityClockAtFirstUse(t *testing.T) {
	e := newSessionEnv(t)
	// Issued before the upgrade: the cookie is valid but the table never saw it.
	token, err := IssueToken(e.member.ID, 0, e.s.Secret, e.s.TTL)
	if err != nil {
		t.Fatal(err)
	}
	if !e.alive(token) {
		t.Fatal("a session the table has not seen yet must keep working")
	}
	var idle time.Duration
	var secs float64
	if err := e.p.QueryRow(context.Background(), `SELECT EXTRACT(EPOCH FROM NOW() - last_active_at) FROM user_sessions WHERE jti = $1`,
		e.jti(token)).Scan(&secs); err != nil {
		t.Fatal(err)
	}
	if idle = time.Duration(secs) * time.Second; idle > time.Minute {
		t.Errorf("the clock started %v ago, want now", idle)
	}
}

func TestInactiveSessionsAreSignedOutWithTheirPushSubscriptions(t *testing.T) {
	e := newSessionEnv(t)
	idle := e.signIn(e.member, safariIPhone)
	active := e.signIn(e.member, firefoxLinux)
	e.addPush(e.member.ID, idle)
	e.addPush(e.member.ID, active)

	e.age(idle, 6*24*time.Hour)
	if !e.alive(idle) {
		t.Fatal("six days idle must not end a session with the default of seven")
	}
	e.age(idle, 8*24*time.Hour)
	if e.alive(idle) {
		t.Fatal("eight days idle: the request must already be refused before the sweep")
	}
	if got := e.list(active); len(got) != 1 {
		t.Errorf("an inactive session is still listed: %+v", got)
	}

	jtis, err := e.s.SweepSessions(context.Background())
	if err != nil || len(jtis) != 1 || jtis[0] != e.jti(idle) {
		t.Fatalf("sweep: %v %v", jtis, err)
	}
	if e.pushCount() != 1 {
		t.Errorf("push subscriptions left: %d, want only the active session's", e.pushCount())
	}
	if !e.alive(active) {
		t.Error("the active session was signed out")
	}
}

func TestTheInactivityLimitIsTheCommunitysSetting(t *testing.T) {
	e := newSessionEnv(t)
	token := e.signIn(e.member, firefoxLinux)
	e.age(token, 3*24*time.Hour)
	set := func(days int) {
		if _, err := e.p.Exec(context.Background(), `UPDATE instance_settings SET session_inactivity_days = $1`, days); err != nil {
			t.Fatal(err)
		}
	}
	set(2)
	if e.alive(token) {
		t.Fatal("three days idle with a two-day limit must be refused")
	}
	set(30)
	if !e.alive(token) {
		t.Fatal("raising the limit must give the session back")
	}
	if _, err := e.p.Exec(context.Background(), `UPDATE instance_settings SET session_inactivity_days = 0`); err == nil {
		t.Fatal("the database accepted a limit of 0 days")
	}
}

func TestSweepDropsEndedSessionsBookkeeping(t *testing.T) {
	e := newSessionEnv(t)
	replaced := e.signIn(e.member, firefoxLinux)
	expired := e.signIn(e.member, safariIPhone)
	if _, err := e.p.Exec(context.Background(), `UPDATE user_sessions SET expires_at = NOW() - INTERVAL '1 minute' WHERE jti = $1`, e.jti(expired)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.p.Exec(context.Background(), `UPDATE users SET token_version = token_version + 1 WHERE id = $1`, e.member.ID); err != nil {
		t.Fatal(err)
	}
	if e.alive(replaced) {
		t.Fatal("a session of an old token version still works")
	}
	if _, err := e.s.SweepSessions(context.Background()); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := e.p.QueryRow(context.Background(), `SELECT COUNT(*) FROM user_sessions`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("rows left: %d %v", n, err)
	}
}
