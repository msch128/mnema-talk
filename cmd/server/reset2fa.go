package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/db"
)

const reset2faUsage = "usage: mnema-talk reset-2fa <username>"

// reset2faCommand turns off the two-factor sign-in of one account and ends its
// sessions. It is how the Owner gets back in after losing the authenticator
// and the recovery codes: whoever can run it on the host can already read the
// database, so it asks for nothing more. The change is written to the audit log.
func reset2faCommand(ctx context.Context, args []string, out io.Writer) error {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return errors.New(reset2faUsage)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	return resetTwoFactorFor(ctx, pool, args[0], out)
}

func resetTwoFactorFor(ctx context.Context, pool *db.Pool, username string, out io.Writer) error {
	var id uuid.UUID
	var name string
	err := pool.QueryRow(ctx, `SELECT id, username FROM users WHERE LOWER(username) = LOWER($1) AND NOT is_bot`, strings.TrimSpace(username)).Scan(&id, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("no user named %q", username)
	}
	if err != nil {
		return err
	}
	if err := auth.ResetTwoFactor(ctx, pool, id); err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `INSERT INTO audit_log (action, target_type, target_id, target_name, details)
		VALUES ('user.two_factor_reset', 'user', $1, $2, '{"via":"host command"}')`, id.String(), name)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Two-factor sign-in is off for %s and all of their sessions ended.\n", name)
	return nil
}
