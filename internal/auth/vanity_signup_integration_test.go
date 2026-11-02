//go:build integration

package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/testutil"
)

func TestVanitySignupCodeNeedsAnEnabledLinkAndReportsStorageFailures(t *testing.T) {
	p := testutil.DB(t)
	testutil.Reset(t, p)
	ctx := context.Background()
	if _, err := p.Exec(ctx, `UPDATE instance_settings SET vanity_slug = 'owls', vanity_accepts_signups = false`); err != nil {
		t.Fatal(err)
	}
	if err := checkCodeKnown(ctx, p, "~owls"); !errors.Is(err, ErrInvalidInvite) {
		t.Errorf("link that takes no sign-ups: %v", err)
	}
	if _, err := p.Exec(ctx, `UPDATE instance_settings SET vanity_accepts_signups = true`); err != nil {
		t.Fatal(err)
	}
	if err := checkCodeKnown(ctx, p, "~OWLS"); err != nil {
		t.Errorf("enabled link, any case: %v", err)
	}
	if err := checkCodeKnown(ctx, p, "~other"); !errors.Is(err, ErrInvalidInvite) {
		t.Errorf("other name: %v", err)
	}

	// A database failure is not "invalid invite": it must surface as an error.
	dead, cancel := context.WithCancel(ctx)
	cancel()
	if err := checkCodeKnown(dead, p, "~owls"); err == nil || errors.Is(err, ErrInvalidInvite) {
		t.Errorf("failing lookup: %v", err)
	}
	// The cancelled statement may leave the transaction unusable; only the error above matters.
	_ = pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		if id, err := claimCode(ctx, tx, "~owls"); err != nil || id != nil {
			t.Errorf("claim: %v %v, want no invite row and no error", id, err)
		}
		if _, err := claimCode(dead, tx, "~owls"); err == nil || errors.Is(err, ErrInvalidInvite) {
			t.Errorf("failing claim: %v", err)
		}
		return nil
	})
}
