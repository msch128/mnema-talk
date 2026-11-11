package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/db"
)

const rotateKeysUsage = "usage: mnema-talk rotate-keys"

// rotateKeysCommand moves every stored authenticator secret to the current
// data key, so the previous key can be removed from the settings afterwards
// (the runbook is "Rotating keys" in doc/operations.md). It reads the same
// settings as the server and can run while the server does.
func rotateKeysCommand(ctx context.Context, args []string, out io.Writer) error {
	if len(args) != 0 {
		return errors.New(rotateKeysUsage)
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
	return rotateKeys(ctx, sessionKeys(cfg), pool, out)
}

// sessionKeys carries the configured keys, as the server's sessions do.
func sessionKeys(cfg *config.Config) *auth.Sessions {
	return &auth.Sessions{
		Secret:          cfg.JWTSecret,
		PreviousSecret:  cfg.JWTSecretPrevious,
		DataKey:         cfg.DataKey,
		DataKeyPrevious: cfg.DataKeyPrevious,
	}
}

func rotateKeys(ctx context.Context, s *auth.Sessions, pool *db.Pool, out io.Writer) error {
	keys := s.KeySetup()
	fmt.Fprintf(out, "This run seals under %s, fingerprint %s. The server logs the fingerprint of its key at start (\"authenticator secrets are sealed under\"): when the two differ, the server runs with other settings. Then run `docker compose up -d` and this command again.\n", keys.Sealing, keys.Fingerprint)
	if len(keys.Previous) > 0 {
		fmt.Fprintf(out, "Previous keys configured for this run: %s.\n", strings.Join(keys.Previous, ", "))
	}
	res, err := s.ResealSecrets(ctx, pool)
	if errors.Is(err, auth.ErrUnreadableSecrets) {
		return fmt.Errorf("nothing was changed: %d authenticator secret(s) cannot be read with any configured key: %s. Put the key they were sealed with back into DATA_KEY_PREVIOUS (or JWT_SECRET_PREVIOUS) and run this again, or reset these accounts with `mnema-talk reset-2fa <username>`. Do not remove a previous key yet",
			len(res.Unreadable), strings.Join(res.Unreadable, ", "))
	}
	if err != nil {
		return fmt.Errorf("nothing was changed: %w", err)
	}
	fmt.Fprintf(out, "Authenticator secrets: %d moved to %s, %d were under it already.\n", res.Moved, keys.Sealing, res.Current)
	if len(keys.Previous) > 0 || res.Moved > 0 {
		fmt.Fprintln(out, "No authenticator secret is under a previous key any more.")
	} else {
		fmt.Fprintln(out, "WARNING: no previous key is configured, so this run could not move anything from a replaced key. If you are replacing a key, its old value belongs in JWT_SECRET_PREVIOUS or DATA_KEY_PREVIOUS, and the server must be restarted with it (`docker compose up -d`) before this command. Do not remove or overwrite the old value until a run lists it above.")
	}
	if len(res.Abandoned) > 0 {
		fmt.Fprintf(out, "%d two-factor setup(s) that were started but never confirmed could not be read and were removed: %s. Two-factor sign-in was not on for these accounts; they start the setup again if they want it.\n",
			len(res.Abandoned), strings.Join(res.Abandoned, ", "))
	}
	if res.RecoveryAccounts > 0 {
		fmt.Fprintf(out, "%d account(s) hold unused recovery codes. Codes made before the rotation are checked with the previous key and stop working when you remove it; the authenticator keeps working, and new codes are made in Settings > Account & security.\n", res.RecoveryAccounts)
	}
	return nil
}
