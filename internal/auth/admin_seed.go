package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
)

// ValidateAdminSeed checks ADMIN_USERNAME and ADMIN_INITIAL_PASSWORD against
// the Owner password policy (longer than for other accounts). An empty password is allowed here: outside
// production one is then generated on first start.
func ValidateAdminSeed(username, password string) error {
	if err := ValidateUsername(username); err != nil {
		return fmt.Errorf("ADMIN_USERNAME %q is invalid: %w", username, err)
	}
	if password != "" {
		if err := ValidatePasswordFor(RoleAdmin, password); err != nil {
			return fmt.Errorf("ADMIN_INITIAL_PASSWORD is invalid: %w", err)
		}
	}
	return nil
}

func hasAdminUser(ctx context.Context, p *db.Pool) (bool, error) {
	var exists bool
	err := p.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE role = 'admin')`).Scan(&exists)
	return exists, err
}

func checkAdminUsernameAvailable(ctx context.Context, p *db.Pool, username string) error {
	// Usernames are unique case-insensitively: "herzog" blocks "Herzog".
	var holder string
	err := p.QueryRow(ctx, `SELECT username FROM users WHERE LOWER(username) = LOWER($1)`, username).Scan(&holder)
	if err == nil {
		return fmt.Errorf("ADMIN_USERNAME %q is taken by the regular user %q; choose another admin name", username, holder)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("look up admin username: %w", err)
	}
	return nil
}

func resolveAdminPassword(password string, requirePassword bool) (string, string, error) {
	if password != "" {
		return password, "", nil
	}
	if requirePassword {
		return "", "", errors.New("ADMIN_INITIAL_PASSWORD is empty: set it for the first start in production (it is only used to create the administrator), then change it after the first login")
	}
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("generate initial admin password: %w", err)
	}
	gen := base64.RawURLEncoding.EncodeToString(b)
	return gen, gen, nil
}

func insertAdminUser(ctx context.Context, p *db.Pool, username, hash string) error {
	_, err := p.Exec(ctx, `
		INSERT INTO users (username, display_name, password_hash, role)
		VALUES ($1, $2, $3, 'admin')`,
		username, defaultDisplayName(username), hash)
	if db.IsUniqueViolation(err) {
		return fmt.Errorf("ADMIN_USERNAME %q is taken by a regular user; choose another admin name", username)
	}
	if err != nil {
		return fmt.Errorf("insert admin user: %w", err)
	}
	return nil
}

func checkExistingAdmin(ctx context.Context, p *db.Pool, invalid error) (bool, error) {
	exists, err := hasAdminUser(ctx, p)
	if err != nil {
		return false, fmt.Errorf("look up admin: %w", err)
	}
	if exists {
		if invalid != nil {
			slog.Warn("ignoring invalid admin seed settings, an administrator already exists", "err", invalid)
		}
		return true, nil
	}
	return false, nil
}

func createAdminAccount(ctx context.Context, p *db.Pool, username, pass, generated string) error {
	hash, err := HashPassword(pass)
	if err != nil {
		return fmt.Errorf("hash admin password: %w", err)
	}
	if err := insertAdminUser(ctx, p, username, hash); err != nil {
		return err
	}
	slog.Info("initial administrator created", "username", username)
	if generated != "" {
		// Development only (see requirePassword); printed once on first start.
		slog.Warn("ADMIN_INITIAL_PASSWORD was empty, generated a password", "username", username, "password", generated)
	}
	return nil
}

// EnsureAdminUser seeds the administrator on first start. With
// requirePassword (production) an empty password fails the seed, so the
// initial password never ends up in container logs; otherwise an empty
// password generates a random one, which is logged exactly once. Invalid
// settings fail the seed (and so startup) with a clear message. Once an admin
// exists the settings are no longer used; invalid ones then only log a
// warning, so an upgrade never refuses to start over a value that has no
// effect.
func EnsureAdminUser(ctx context.Context, p *db.Pool, username, password string, requirePassword bool) error {
	invalid := ValidateAdminSeed(username, password)
	exists, err := checkExistingAdmin(ctx, p, invalid)
	if err != nil || exists {
		return err
	}
	if invalid != nil {
		return invalid
	}
	pass, generated, err := resolveAdminPassword(password, requirePassword)
	if err != nil {
		return err
	}
	if err := checkAdminUsernameAvailable(ctx, p, username); err != nil {
		return err
	}
	return createAdminAccount(ctx, p, username, pass, generated)
}
