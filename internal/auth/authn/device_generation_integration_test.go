//go:build integration

package authn

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/auth"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm/permtest"
)

// Every event that bumps token_version retires the known-device cookies issued
// before it, against the real database.
func TestDeviceCookieDoesNotSurviveCredentialEvents(t *testing.T) {
	p, admin, member := authFixture(t)
	ctx := context.Background()
	h := NewHandler(&auth.Sessions{DB: p, Secret: strings.Repeat("k", 32), TTL: time.Hour}, permtest.NewFixture().Service(), nil)

	issue := func(id uuid.UUID, account string) *http.Cookie {
		var tv int
		if err := p.QueryRow(ctx, `SELECT token_version FROM users WHERE id = $1`, id).Scan(&tv); err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		h.Sessions.MarkDevice(w, account, id, tv)
		return w.Result().Cookies()[0]
	}
	known := func(c *http.Cookie, account string) bool {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(c)
		return h.isKnownDevice(r, account)
	}

	c := issue(admin.ID, "cool_guy")
	if !known(c, "cool_guy") {
		t.Fatal("fresh cookie must be known")
	}
	if known(c, "member") {
		t.Fatal("cookie is bound to its account")
	}
	steps := []struct {
		name string
		id   uuid.UUID
		acct string
		do   func() error
	}{
		{"password change", admin.ID, "cool_guy", func() error {
			_, err := auth.ChangePassword(ctx, p, admin.ID, testPassword, "another-long-password")
			return err
		}},
		{"log out everywhere", admin.ID, "cool_guy", func() error { return auth.RevokeSessions(ctx, p, admin.ID) }},
		{"admin reset", member.ID, "member", func() error { _, err := auth.ResetPassword(ctx, p, member.ID); return err }},
		{"admin sets password", member.ID, "member", func() error { return auth.SetPassword(ctx, p, member.ID, "chosen-password-1") }},
		{"disable", member.ID, "member", func() error { return auth.SetDisabled(ctx, p, member.ID, true) }},
	}
	for _, st := range steps {
		cookie := issue(st.id, st.acct)
		if !known(cookie, st.acct) {
			t.Fatalf("%s: fresh cookie not known", st.name)
		}
		if err := st.do(); err != nil {
			t.Fatalf("%s: %v", st.name, err)
		}
		if known(cookie, st.acct) {
			t.Fatalf("%s: a cookie issued before it still counts as a known device", st.name)
		}
	}
	// Re-enabling does not revive the cookie issued before disabling.
	if err := auth.SetDisabled(ctx, p, member.ID, false); err != nil {
		t.Fatal(err)
	}
	if known(c, "cool_guy") {
		t.Fatal("the original cookie survived the whole sequence")
	}
	// A cookie issued after the events works again.
	if !known(issue(admin.ID, "cool_guy"), "cool_guy") {
		t.Fatal("a new cookie must be known")
	}
}
