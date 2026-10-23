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

// legacyToken makes a token the way a pre-1.0 server did: no recorded issuer.
func legacyToken(t *testing.T, e *env, botID uuid.UUID) (Token, string) {
	t.Helper()
	tok, secret, err := IssueToken(context.Background(), e.p, botID, "old", nil, uuid.Nil)
	if err != nil {
		t.Fatal(err)
	}
	return tok, secret
}

func tokenBinding(t *testing.T, e *env, botID, tokenID uuid.UUID) string {
	t.Helper()
	list, err := Tokens(context.Background(), e.p, botID)
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range list {
		if tok.ID == tokenID {
			return tok.IssuerBinding
		}
	}
	t.Fatal("token not listed")
	return ""
}

func boundTo(t *testing.T, e *env, tokenID uuid.UUID) (uuid.UUID, bool) {
	t.Helper()
	var who *uuid.UUID
	var inferred bool
	if err := e.p.QueryRow(context.Background(), `SELECT created_by, minter_inferred FROM bot_tokens WHERE id = $1`, tokenID).Scan(&who, &inferred); err != nil {
		t.Fatal(err)
	}
	if who == nil {
		return uuid.Nil, inferred
	}
	return *who, inferred
}

func TestLegacyTokenIsBoundToTheBotsCreatorOnFirstUse(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

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
	b, err := Create(ctx, e.p, "legacy", 0, delegate)
	if err != nil {
		t.Fatal(err)
	}
	tok, secret := legacyToken(t, e, b.ID)

	svc := perm.NewService(e.p)
	a := NewAuthenticator(e.p)
	a.Perms = svc
	now := time.Now()
	a.now = func() time.Time { return now }

	if got := tokenBinding(t, e, b.ID, tok.ID); got != "unbound" {
		t.Fatalf("before first use: binding = %q, want unbound", got)
	}
	if _, err := a.Lookup(ctx, secret); err != nil {
		t.Fatalf("a legacy token must keep working: %v", err)
	}
	if who, inferred := boundTo(t, e, tok.ID); who != delegate || !inferred {
		t.Fatalf("bound to %s inferred=%v, want the bot's creator %s, inferred", who, inferred, delegate)
	}
	if got := tokenBinding(t, e, b.ID, tok.ID); got != "inferred" {
		t.Fatalf("after first use: binding = %q, want inferred", got)
	}

	// From now on it is held to the same rule as any token: when its
	// (inferred) issuer is demoted, it stops.
	if _, err := e.p.Exec(ctx, `DELETE FROM user_roles WHERE user_id = $1`, delegate); err != nil {
		t.Fatal(err)
	}
	now = now.Add(minterRecheck + time.Second)
	svc.Invalidate()
	if _, err := a.Lookup(ctx, secret); !errors.Is(err, errUnauthorized) {
		t.Fatalf("lookup after demotion = %v, want unauthorized", err)
	}
}

// Binding must never be the reason a bot stops: a creator who no longer
// qualifies is skipped and the Owner, who always does, takes the binding.
func TestLegacyTokenFallsBackToTheOwnerAndNeverBreaks(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	var drifter uuid.UUID // created the bot, holds no Manage bots any more
	if err := e.p.QueryRow(ctx, `INSERT INTO users (username, display_name, password_hash, role) VALUES ('drifter', 'd', 'x', 'user') RETURNING id`).Scan(&drifter); err != nil {
		t.Fatal(err)
	}
	b, err := Create(ctx, e.p, "legacy", 0, drifter)
	if err != nil {
		t.Fatal(err)
	}
	tok, secret := legacyToken(t, e, b.ID)
	a := NewAuthenticator(e.p)
	a.Perms = perm.NewService(e.p)

	if _, err := a.Lookup(ctx, secret); err != nil {
		t.Fatalf("legacy token refused: %v", err)
	}
	if who, inferred := boundTo(t, e, tok.ID); who != e.owner.ID || !inferred {
		t.Fatalf("bound to %s inferred=%v, want the Owner, inferred", who, inferred)
	}

	// The creator was deleted entirely (created_by is NULL): same outcome.
	b2, err := Create(ctx, e.p, "orphan", 0, e.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.p.Exec(ctx, `UPDATE bots SET created_by = NULL WHERE id = $1`, b2.ID); err != nil {
		t.Fatal(err)
	}
	tok2, secret2 := legacyToken(t, e, b2.ID)
	if _, err := a.Lookup(ctx, secret2); err != nil {
		t.Fatalf("orphaned legacy token refused: %v", err)
	}
	if who, _ := boundTo(t, e, tok2.ID); who != e.owner.ID {
		t.Fatalf("orphan bound to %s, want the Owner", who)
	}
}

func TestLegacyTokenStaysUnboundAndWorkingWhenNobodyQualifies(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	f := e.newBot()
	tok, secret := legacyToken(t, e, f.bot.ID)
	if _, err := e.p.Exec(ctx, `UPDATE users SET disabled_at = NOW() WHERE id = $1`, e.owner.ID); err != nil {
		t.Fatal(err)
	}
	a := NewAuthenticator(e.p)
	a.Perms = perm.NewService(e.p)
	now := time.Now()
	a.now = func() time.Time { return now }

	if _, err := a.Lookup(ctx, secret); err != nil {
		t.Fatalf("a token nobody could be bound to must keep working: %v", err)
	}
	if who, _ := boundTo(t, e, tok.ID); who != uuid.Nil {
		t.Fatalf("bound to %s, want unbound", who)
	}
	if got := tokenBinding(t, e, f.bot.ID, tok.ID); got != "unbound" {
		t.Fatalf("binding = %q, want unbound", got)
	}

	// It is retried later, and binds once somebody qualifies again.
	if _, err := e.p.Exec(ctx, `UPDATE users SET disabled_at = NULL WHERE id = $1`, e.owner.ID); err != nil {
		t.Fatal(err)
	}
	now = now.Add(bindRetry + time.Second)
	if _, err := a.Lookup(ctx, secret); err != nil {
		t.Fatal(err)
	}
	if who, inferred := boundTo(t, e, tok.ID); who != e.owner.ID || !inferred {
		t.Fatalf("retry: bound to %s inferred=%v", who, inferred)
	}
}

func TestIssuedTokensAreNeverMarkedOrRebound(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	f := e.newBot()
	tok, secret, err := IssueToken(ctx, e.p, f.bot.ID, "new", nil, e.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	a := NewAuthenticator(e.p)
	a.Perms = perm.NewService(e.p)
	if _, err := a.Lookup(ctx, secret); err != nil {
		t.Fatal(err)
	}
	if who, inferred := boundTo(t, e, tok.ID); who != e.owner.ID || inferred {
		t.Fatalf("bound to %s inferred=%v, want the issuer, not inferred", who, inferred)
	}
	if got := tokenBinding(t, e, f.bot.ID, tok.ID); got != "" {
		t.Fatalf("binding = %q, want none for a token issued with its issuer", got)
	}
}
