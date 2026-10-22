//go:build integration

package bot

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

// A token stops working once the person who issued it no longer holds Manage
// bots, is disabled, or could no longer act as the bot.
func TestTokenStopsWhenItsMinterLosesAuthority(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	f := e.newBot()

	var delegate, delegateRole uuid.UUID
	if err := e.p.QueryRow(ctx, `INSERT INTO users (username, display_name, password_hash, role) VALUES ('delegate', 'd', 'x', 'user') RETURNING id`).Scan(&delegate); err != nil {
		t.Fatal(err)
	}
	if err := e.p.QueryRow(ctx, `INSERT INTO roles (name, position, permissions) VALUES ('delegates', 2, $1) RETURNING id`, int64(perm.ManageBots|perm.SendMessages|perm.ViewChannel)).Scan(&delegateRole); err != nil {
		t.Fatal(err)
	}
	if _, err := e.p.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2)`, delegate, delegateRole); err != nil {
		t.Fatal(err)
	}
	svc := perm.NewService(e.p)
	a := NewAuthenticator(e.p)
	a.Perms = svc
	now := time.Now()
	a.now = func() time.Time { return now }

	mint := func(minter uuid.UUID) string {
		t.Helper()
		_, secret, err := IssueToken(ctx, e.p, f.bot.ID, "m", nil, minter)
		if err != nil {
			t.Fatal(err)
		}
		return secret
	}
	denied := func(secret, why string) {
		t.Helper()
		now = now.Add(minterRecheck + time.Second)
		svc.Invalidate()
		if _, err := a.Lookup(ctx, secret); !errors.Is(err, errUnauthorized) {
			t.Fatalf("%s: lookup = %v, want unauthorized", why, err)
		}
	}

	fromDelegate, fromOwner, legacy := mint(delegate), mint(e.owner.ID), mint(uuid.Nil)
	for name, tok := range map[string]string{"delegate": fromDelegate, "owner": fromOwner, "legacy": legacy} {
		if _, err := a.Lookup(ctx, tok); err != nil {
			t.Fatalf("%s token refused while valid: %v", name, err)
		}
	}

	// The delegate is demoted: their token ends, the Owner's and old ones stay.
	if _, err := e.p.Exec(ctx, `DELETE FROM user_roles WHERE user_id = $1`, delegate); err != nil {
		t.Fatal(err)
	}
	denied(fromDelegate, "minter lost Manage bots")
	for name, tok := range map[string]string{"owner": fromOwner, "legacy": legacy} {
		if _, err := a.Lookup(ctx, tok); err != nil {
			t.Fatalf("%s token must survive: %v", name, err)
		}
	}

	// Regaining the role is not enough for the old verdict to linger: it is re-read.
	if _, err := e.p.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2)`, delegate, delegateRole); err != nil {
		t.Fatal(err)
	}
	now = now.Add(minterRecheck + time.Second)
	svc.Invalidate()
	if _, err := a.Lookup(ctx, fromDelegate); err != nil {
		t.Fatalf("token refused although the minter qualifies again: %v", err)
	}

	// The bot grows a permission the delegate lacks.
	var elevated uuid.UUID
	if err := e.p.QueryRow(ctx, `INSERT INTO roles (name, position, permissions) VALUES ('more', 1, $1) RETURNING id`, int64(perm.ManageMessages)).Scan(&elevated); err != nil {
		t.Fatal(err)
	}
	if _, err := e.p.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2)`, f.bot.UserID, elevated); err != nil {
		t.Fatal(err)
	}
	denied(fromDelegate, "bot holds more than the minter")
	if _, err := e.p.Exec(ctx, `DELETE FROM user_roles WHERE user_id = $1 AND role_id = $2`, f.bot.UserID, elevated); err != nil {
		t.Fatal(err)
	}

	// A disabled minter.
	now = now.Add(minterRecheck + time.Second)
	svc.Invalidate()
	if _, err := a.Lookup(ctx, fromDelegate); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if _, err := e.p.Exec(ctx, `UPDATE users SET disabled_at = NOW() WHERE id = $1`, delegate); err != nil {
		t.Fatal(err)
	}
	denied(fromDelegate, "minter disabled")
}
