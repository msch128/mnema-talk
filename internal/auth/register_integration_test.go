//go:build integration

package auth

import (
	"context"
	"testing"

	"github.com/msch128/mnema-talk/internal/db"
)

func TestAuthRegistrationConcurrentChanges(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, query string
		change      func(*testing.T, *db.Pool)
		want        error
	}{
		{"invite removed after precheck", "FROM invites WHERE code = $1 FOR UPDATE", func(t *testing.T, p *db.Pool) {
			if _, e := p.Exec(ctx, `DELETE FROM invites WHERE code='member-invite-k7x2'`); e != nil {
				t.Fatal(e)
			}
		}, ErrInvalidInvite},
		{"username inserted after check", "INSERT INTO users (username, display_name, password_hash, role, invite_id)", func(t *testing.T, p *db.Pool) {
			if _, e := p.Exec(ctx, `INSERT INTO users(username,display_name,password_hash,role) VALUES('newmember','newmember','test-only-hash','user')`); e != nil {
				t.Fatal(e)
			}
		}, ErrUsernameTaken},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, _, _ := authFixture(t)
			hook := &authQueryHook{match: tc.query, before: func() { tc.change(t, p) }}
			fault := authHookPool(t, p, hook)
			u, e := Register(ctx, fault, "newmember", "", testPassword, "member-invite-k7x2")
			if u != nil || e != tc.want {
				t.Fatalf("register = %+v, %v want %v", u, e, tc.want)
			}
		})
	}
}
