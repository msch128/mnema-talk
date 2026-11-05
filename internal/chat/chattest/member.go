//go:build integration

package chattest

import (
	"context"
	"testing"

	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/testutil"
)

// NewMember resets the test database and inserts one ordinary member.
func NewMember(t *testing.T) (*db.Pool, *auth.User) {
	t.Helper()
	p := testutil.DB(t)
	testutil.Reset(t, p)
	u := &auth.User{Username: "coverage_member", DisplayName: "Coverage Member", Role: auth.RoleUser}
	if err := p.QueryRow(context.Background(), `INSERT INTO users (username, display_name, password_hash) VALUES ($1,$2,'test-only-unused') RETURNING id, created_at`, u.Username, u.DisplayName).Scan(&u.ID, &u.CreatedAt); err != nil {
		t.Fatal(err)
	}
	return p, u
}
