//go:build integration

package bot

import (
	"context"
	"testing"

	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/testutil/pgfault"
)

// legacyFixture is a bot token without a recorded issuer, and an
// Authenticator whose database and permission pools can be made to fail.
func legacyFixture(t *testing.T, dbFault, permFault *pgfault.Tracer) (*env, fixtureBot, *Authenticator) {
	t.Helper()
	e := newEnv(t)
	f := e.newBot()
	if _, err := e.p.Exec(context.Background(), `UPDATE bot_tokens SET created_by = NULL WHERE id = $1`, f.token.ID); err != nil {
		t.Fatal(err)
	}
	a := NewAuthenticator(e.p)
	a.Perms = perm.NewService(e.p)
	if dbFault != nil {
		a.DB = &db.Pool{Pool: pgfault.Pool(t, e.p.Pool, dbFault)}
	}
	if permFault != nil {
		a.Perms = perm.NewService(&db.Pool{Pool: pgfault.Pool(t, e.p.Pool, permFault)})
	}
	return e, f, a
}

func tokenIsBound(t *testing.T, e *env, f fixtureBot) bool {
	t.Helper()
	var bound bool
	if err := e.p.QueryRow(context.Background(), `SELECT created_by IS NOT NULL FROM bot_tokens WHERE id = $1`, f.token.ID).Scan(&bound); err != nil {
		t.Fatal(err)
	}
	return bound
}

// Database trouble never revokes a legacy token and never binds it by guess:
// it stays unbound, and the next attempt waits out the retry interval.
func TestBindLegacyLeavesTheTokenUnboundOnDatabaseTrouble(t *testing.T) {
	cases := []struct {
		name  string
		db    *pgfault.Tracer
		perms *pgfault.Tracer
	}{
		{"issuer lookup", &pgfault.Tracer{Query: "SELECT b.created_by FROM bot_tokens"}, nil},
		{"qualification check", nil, &pgfault.Tracer{}},
		{"binding", &pgfault.Tracer{Query: "UPDATE bot_tokens SET created_by"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e, f, a := legacyFixture(t, c.db, c.perms)
			a.bindLegacy(context.Background(), f.token.ID, f.bot.UserID)
			if tokenIsBound(t, e, f) {
				t.Fatal("the token was bound despite the database failure")
			}
			// Within the retry interval the attempt is not repeated, so
			// the healthy path below cannot bind it either.
			a.DB, a.Perms = e.p, perm.NewService(e.p)
			a.bindLegacy(context.Background(), f.token.ID, f.bot.UserID)
			if tokenIsBound(t, e, f) {
				t.Fatal("the token was retried before the retry interval")
			}
			a.bindTried = nil
			a.bindLegacy(context.Background(), f.token.ID, f.bot.UserID)
			if !tokenIsBound(t, e, f) {
				t.Fatal("the token was not bound once the database recovered")
			}
		})
	}
}

func TestBindCandidatesReportsAFailedOwnerLookup(t *testing.T) {
	_, f, a := legacyFixture(t, &pgfault.Tracer{Query: "FROM users WHERE role = 'admin'"}, nil)
	if got, err := a.bindCandidates(context.Background(), f.token.ID); err == nil {
		t.Fatalf("candidates = %v, want the database error", got)
	}
}
