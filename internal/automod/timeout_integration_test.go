//go:build integration

package automod

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/perm"
)

func newUser(t *testing.T, p *db.Pool, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := p.QueryRow(context.Background(), `INSERT INTO users (username, display_name, password_hash) VALUES ($1, $1, 'x') RETURNING id`, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func timeoutOf(t *testing.T, p *db.Pool, id uuid.UUID) (until time.Time, reason string, by *uuid.UUID) {
	t.Helper()
	var r *string
	if err := p.QueryRow(context.Background(), `SELECT timeout_until, timeout_reason, timeout_by FROM users WHERE id = $1`, id).Scan(&until, &r, &by); err != nil {
		t.Fatal(err)
	}
	if r != nil {
		reason = *r
	}
	return until, reason, by
}

func TestAShorterAutomodTimeoutKeepsTheLongerOnesReasonAndModerator(t *testing.T) {
	p := newStore(t)
	ctx := context.Background()
	e := New(p, perm.NewService(p), nil, nil)
	mod, bob := newUser(t, p, "mod_user"), newUser(t, p, "bob_user")
	if _, err := p.Exec(ctx, `UPDATE users SET timeout_until = NOW() + interval '7 days', timeout_reason = 'harassment', timeout_by = $2 WHERE id = $1`, bob, mod); err != nil {
		t.Fatal(err)
	}
	before, _, _ := timeoutOf(t, p, bob)

	until, err := e.setTimeout(ctx, bob, 60, "Automod: spam")
	if err != nil {
		t.Fatal(err)
	}
	if !until.Equal(before) {
		t.Fatalf("returned until %v, want the running %v", until, before)
	}
	after, reason, by := timeoutOf(t, p, bob)
	if !after.Equal(before) || reason != "harassment" || by == nil || *by != mod {
		t.Fatalf("timeout = %v %q %v, want the moderator's untouched", after, reason, by)
	}

	// A longer automod timeout (or none running) takes over.
	if _, err := e.setTimeout(ctx, bob, 30*24*3600, "Automod: spam"); err != nil {
		t.Fatal(err)
	}
	if _, reason, by = timeoutOf(t, p, bob); reason != "Automod: spam" || by != nil {
		t.Fatalf("longer timeout: reason %q by %v", reason, by)
	}
}

func TestRaidRestrictionReportsEachJoinersOwnEndTime(t *testing.T) {
	p := newStore(t)
	ctx := context.Background()
	e := New(p, perm.NewService(p), nil, nil)
	long, fresh := newUser(t, p, "long_user"), newUser(t, p, "fresh_user")
	if _, err := p.Exec(ctx, `INSERT INTO automod_state DEFAULT VALUES ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Exec(ctx, `UPDATE users SET timeout_until = NOW() + interval '7 days', timeout_reason = 'harassment' WHERE id = $1`, long); err != nil {
		t.Fatal(err)
	}
	raid := &compiled{Rule: Rule{Name: "Raid", Kind: KindRaid, Config: Settings{Joins: 1, WindowSeconds: 600, RestrictSeconds: 300}}}
	restricted, started, err := e.restrictJoiners(ctx, raid, fresh)
	if err != nil || !started {
		t.Fatalf("restrictJoiners = %v, %v, %v", restricted, started, err)
	}
	if len(restricted) != 2 {
		t.Fatalf("restricted %d, want 2", len(restricted))
	}
	if got := time.Until(restricted[long]); got < 6*24*time.Hour {
		t.Errorf("long timeout reported as ending in %v", got)
	}
	if got := time.Until(restricted[fresh]); got > 10*time.Minute {
		t.Errorf("fresh joiner reported as ending in %v", got)
	}
	if _, reason, _ := timeoutOf(t, p, long); reason != "harassment" {
		t.Errorf("reason overwritten: %q", reason)
	}
	if _, reason, _ := timeoutOf(t, p, fresh); reason != raidReason {
		t.Errorf("fresh reason %q", reason)
	}
}

func TestAnInvalidateDuringAReloadIsNotLost(t *testing.T) {
	p := newStore(t)
	ctx := context.Background()
	e := New(p, perm.NewService(p), nil, nil)
	if _, err := e.current(ctx); err != nil {
		t.Fatal(err)
	}
	// The rule is committed and Invalidate called while a reload that read the
	// old rules is still running.
	e.Invalidate()
	e.listed = func() {
		e.listed = nil
		e.Invalidate()
	}
	if _, err := e.reload(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if e.checked.Load() != 0 {
		t.Fatal("the reload marked its stale rules as checked, so the next call would not load again")
	}
}
