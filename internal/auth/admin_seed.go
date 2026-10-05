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
// the same policy as any account. An empty password is allowed: one is then
// generated on first start.
func ValidateAdminSeed(username, password string) error {
	if err := ValidateUsername(username); err != nil {
		return fmt.Errorf("ADMIN_USERNAME %q is invalid: %w", username, err)
	}
	if password != "" {
		if err := ValidatePassword(password); err != nil {
			return fmt.Errorf("ADMIN_INITIAL_PASSWORD is invalid: %w", err)
		}
	}
	return nil
}

// EnsureAdminUser seeds the administrator on first start. An empty password
// generates a random one, which is logged exactly once. Invalid settings fail
// the seed (and so startup) with a clear message. Once an admin exists the
// settings are no longer used; invalid ones then only log a warning, so an
// upgrade never refuses to start over a value that has no effect.
func EnsureAdminUser(ctx context.Context, p *db.Pool, username, password string) error {
	invalid := ValidateAdminSeed(username, password)

	var exists bool
	if err := p.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE role = 'admin')`).Scan(&exists); err != nil {
		return fmt.Errorf("look up admin: %w", err)
	}
	if exists {
		if invalid != nil {
			slog.Warn("ignoring invalid admin seed settings, an administrator already exists", "err", invalid)
		}
		return nil
	}
	if invalid != nil {
		return invalid
	}

	// Usernames are unique case-insensitively: "herzog" blocks "Herzog".
	var holder string
	err := p.QueryRow(ctx, `SELECT username FROM users WHERE LOWER(username) = LOWER($1)`, username).Scan(&holder)
	if err == nil {
		return fmt.Errorf("ADMIN_USERNAME %q is taken by the regular user %q; choose another admin name", username, holder)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("look up admin username: %w", err)
	}

	generated := password == ""
	if generated {
		b := make([]byte, 18)
		if _, err := rand.Read(b); err != nil {
			return fmt.Errorf("generate admin password: %w", err)
		}
		password = base64.RawURLEncoding.EncodeToString(b)
	}
	hash, err := HashPassword(password)
	if err != nil {
		return fmt.Errorf("hash admin password: %w", err)
	}

	_, err = p.Exec(ctx, `
		INSERT INTO users (username, display_name, password_hash, role)
		VALUES ($1, $2, $3, 'admin')`,
		username, defaultDisplayName(username), hash)
	if isUniqueViolation(err) {
		return fmt.Errorf("ADMIN_USERNAME %q is taken by a regular user; choose another admin name", username)
	}
	if err != nil {
		return fmt.Errorf("insert admin user: %w", err)
	}

	slog.Info("initial administrator created", "username", username)
	if generated {
		// Printed once on first start only; change it after the first login.
		slog.Warn("ADMIN_INITIAL_PASSWORD was empty, generated a password", "username", username, "password", password)
	}
	return nil
}
