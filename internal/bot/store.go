// Package bot implements bot accounts: hidden member-like accounts that
// authenticate with an API token (Authorization: Bot <token>) and get their
// permissions from roles and channel overwrites through internal/perm.
package bot

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

const (
	// MaxNameLen is the longest bot name (the width of users.display_name).
	MaxNameLen = 24
	// MaxBots bounds the bot accounts of the community.
	MaxBots = 50
	// MaxTokens bounds the active tokens of one bot.
	MaxTokens = 10
	// TokenPrefix starts every bot token, so secret scanners can find them.
	TokenPrefix = "mnb_"
	// MaxTokenLifetime bounds an expiry set by an admin.
	MaxTokenLifetime = 10 * 365 * 24 * time.Hour
)

// Gateway intents, with Discord's bit numbers. An admin lists the intents a
// bot may ask for; the bot picks among them when it identifies.
const (
	IntentGuilds                = 1 << 0
	IntentGuildMembers          = 1 << 1
	IntentGuildMessages         = 1 << 9
	IntentGuildMessageReactions = 1 << 10
)

// AllIntents is every intent a bot can be allowed.
const AllIntents = IntentGuilds | IntentGuildMembers | IntentGuildMessages | IntentGuildMessageReactions

var (
	errNotFound      = httpx.ErrNotFound("bot not found")
	errTokenNotFound = httpx.ErrNotFound("token not found")
)

// Bot is a bot account as the admin API shows it.
type Bot struct {
	ID     uuid.UUID `json:"id" format:"uuid"`
	UserID uuid.UUID `json:"user_id" format:"uuid"`
	Name   string    `json:"name" minLength:"1" maxLength:"24"`
	// AvatarURL is the API URL of the avatar; omitted when none.
	AvatarURL string `json:"avatar_url,omitempty" binding:"optional"`
	// AllowedIntents are the gateway intents the bot may ask for.
	AllowedIntents int64 `json:"allowed_intents" minimum:"0"`
	// Disabled bots cannot use any token or gateway connection.
	Disabled bool `json:"disabled"`
	// RoleIDs are the bot's roles, highest first; its permissions come from
	// them and from channel overwrites, never from @everyone.
	RoleIDs      []uuid.UUID `json:"role_ids" format:"uuid"`
	CreatedBy    *uuid.UUID  `json:"created_by,omitempty" binding:"optional" format:"uuid"`
	CreatedAt    time.Time   `json:"created_at" format:"date-time"`
	LastActiveAt *time.Time  `json:"last_active_at,omitempty" binding:"optional" format:"date-time"`
}

// Token describes one API token; the secret itself is never listed.
type Token struct {
	ID    uuid.UUID `json:"id" format:"uuid"`
	Name  string    `json:"name" maxLength:"32"`
	Hint  string    `json:"hint"`
	State string    `json:"state" enums:"active,expired,revoked"`
	// ExpiresAt is empty for a token that never expires.
	ExpiresAt  *time.Time `json:"expires_at,omitempty" binding:"optional" format:"date-time"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty" binding:"optional" format:"date-time"`
	CreatedAt  time.Time  `json:"created_at" format:"date-time"`
}

// NewToken makes a token and its stored hash.
func NewToken() (token string, hash []byte, hint string) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, ""
	}
	token = TokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
	return token, HashToken(token), token[len(token)-4:]
}

// HashToken is what the database stores of a token. The token has 256 random
// bits, so a fast hash is enough.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

const botColumns = `b.id, b.user_id, b.name, u.avatar_s3_key, b.allowed_intents, b.disabled, b.created_by, b.created_at, b.last_active_at`

func scanBot(row pgx.Row) (*Bot, error) {
	var b Bot
	var avatar *string
	if err := row.Scan(&b.ID, &b.UserID, &b.Name, &avatar, &b.AllowedIntents, &b.Disabled, &b.CreatedBy, &b.CreatedAt, &b.LastActiveAt); err != nil {
		return nil, err
	}
	b.AvatarURL = auth.AvatarURL(avatar)
	b.RoleIDs = []uuid.UUID{}
	return &b, nil
}

// Get loads one bot (without roles).
func Get(ctx context.Context, q db.Querier, id uuid.UUID) (*Bot, error) {
	b, err := scanBot(q.QueryRow(ctx, `SELECT `+botColumns+` FROM bots b JOIN users u ON u.id = b.user_id WHERE b.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load bot: %w", err)
	}
	return b, nil
}

// List loads every bot, oldest first.
func List(ctx context.Context, q db.Querier) ([]Bot, error) {
	rows, err := q.Query(ctx, `SELECT `+botColumns+` FROM bots b JOIN users u ON u.id = b.user_id ORDER BY b.created_at, b.id`)
	if err != nil {
		return nil, fmt.Errorf("list bots: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Bot, error) {
		b, err := scanBot(row)
		return *b, err
	})
}

// Create makes the bot account and the bot in one transaction, and gives it
// the built-in Bot role. creator is recorded and grants nothing. Callers that
// act for a person go through Handler.create, which holds the permission lock.
func Create(ctx context.Context, p *db.Pool, name string, intents int64, creator uuid.UUID) (*Bot, error) {
	var id uuid.UUID
	err := pgx.BeginFunc(ctx, p, func(tx pgx.Tx) (err error) {
		id, err = createIn(ctx, tx, name, intents, creator)
		return err
	})
	if err != nil {
		return nil, wrapCreate(err)
	}
	return Get(ctx, p, id)
}

func wrapCreate(err error) error {
	if _, ok := httpx.AsAPIError(err); ok {
		return err
	}
	return fmt.Errorf("create bot: %w", err)
}

// createIn inserts the account, the bot and its Bot role in tx.
func createIn(ctx context.Context, tx pgx.Tx, name string, intents int64, creator uuid.UUID) (uuid.UUID, error) {
	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		return uuid.Nil, err
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM bots`).Scan(&n); err != nil {
		return uuid.Nil, err
	}
	if n >= MaxBots {
		return uuid.Nil, httpx.ErrConflict(fmt.Sprintf("at most %d bots are allowed", MaxBots))
	}
	var user, id uuid.UUID
	// "!" is no password hash: the account can never sign in (login,
	// sessions and re-authentication skip bots).
	if err := tx.QueryRow(ctx, `
		INSERT INTO users (username, display_name, password_hash, role, is_bot)
		VALUES ($1, $2, '!', 'user', TRUE) RETURNING id`,
		"bot-"+hex.EncodeToString(suffix), name).Scan(&user); err != nil {
		return uuid.Nil, err
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO bots (user_id, name, allowed_intents, created_by) VALUES ($1, $2, $3, $4) RETURNING id`,
		user, name, intents, creator).Scan(&id); err != nil {
		return uuid.Nil, err
	}
	// Every bot holds the shared Bot role and a managed role of its own from
	// the start. The own role is empty: the bot can do nothing until it is given
	// something, and what is given goes through the bot invariant in perm.
	if _, err := tx.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) SELECT $1, id FROM roles WHERE managed AND bot_user_id IS NULL`, user); err != nil {
		return uuid.Nil, err
	}
	var role uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO roles (name, color, position, permissions, hoist, mentionable, managed, bot_user_id)
		VALUES ($1, 0, 1, 0, FALSE, FALSE, TRUE, $2) RETURNING id`, roleName(name), user).Scan(&role); err != nil {
		return uuid.Nil, err
	}
	_, err := tx.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2)`, user, role)
	return id, err
}

// roleName is the name of a bot's own role: the bot's name, except where role
// names are reserved (@everyone and @here).
func roleName(bot string) string {
	if n := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(bot), "@")); n == "everyone" || n == "here" {
		return bot + " bot"
	}
	return bot
}

// Update changes the name, intents and disabled flag (nil leaves one).
func Update(ctx context.Context, p *db.Pool, id uuid.UUID, name *string, intents *int64, disabled *bool) error {
	return pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		var user uuid.UUID
		err := tx.QueryRow(ctx, `
			UPDATE bots SET name = COALESCE($2, name), allowed_intents = COALESCE($3, allowed_intents),
			    disabled = COALESCE($4, disabled), updated_at = NOW()
			WHERE id = $1 RETURNING user_id`, id, name, intents, disabled).Scan(&user)
		if errors.Is(err, pgx.ErrNoRows) {
			return errNotFound
		}
		if err != nil {
			return fmt.Errorf("update bot: %w", err)
		}
		if name != nil {
			if _, err = tx.Exec(ctx, `UPDATE users SET display_name = $2, updated_at = NOW() WHERE id = $1`, user, *name); err != nil {
				return err
			}
			// The bot's own role follows the rename.
			_, err = tx.Exec(ctx, `UPDATE roles SET name = $2, updated_at = NOW() WHERE bot_user_id = $1`, user, roleName(*name))
		}
		return err
	})
}

// Delete removes the bot, its tokens and roles. The hidden account stays when
// it has posted messages, so history keeps its author.
func Delete(ctx context.Context, p *db.Pool, id uuid.UUID) error {
	return pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		var user uuid.UUID
		err := tx.QueryRow(ctx, `DELETE FROM bots WHERE id = $1 RETURNING user_id`, id).Scan(&user)
		if errors.Is(err, pgx.ErrNoRows) {
			return errNotFound
		}
		if err != nil {
			return fmt.Errorf("delete bot: %w", err)
		}
		// The bot's own role goes with it (the bot row is gone, so the guard
		// lets it); the assignments go with the role.
		if _, err := tx.Exec(ctx, `DELETE FROM roles WHERE bot_user_id = $1`, user); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM user_roles WHERE user_id = $1`, user); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			DELETE FROM users WHERE id = $1 AND is_bot AND NOT EXISTS (SELECT 1 FROM messages WHERE user_id = $1)`, user)
		return err
	})
}

// WithRoles fills RoleIDs of the bots from the given assignment.
func WithRoles(bots []Bot, roles map[uuid.UUID][]uuid.UUID) {
	for i := range bots {
		if r := roles[bots[i].UserID]; r != nil {
			bots[i].RoleIDs = slices.Clone(r)
		}
	}
}

// Tokens lists the tokens of a bot, newest first.
func Tokens(ctx context.Context, q db.Querier, botID uuid.UUID) ([]Token, error) {
	rows, err := q.Query(ctx, `
		SELECT id, name, hint, expires_at, last_used_at, revoked_at, created_at
		FROM bot_tokens WHERE bot_id = $1 ORDER BY created_at DESC, id`, botID)
	if err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	now := time.Now()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Token, error) {
		var t Token
		var revoked *time.Time
		err := row.Scan(&t.ID, &t.Name, &t.Hint, &t.ExpiresAt, &t.LastUsedAt, &revoked, &t.CreatedAt)
		t.State = tokenState(revoked, t.ExpiresAt, now)
		return t, err
	})
}

func tokenState(revoked, expires *time.Time, now time.Time) string {
	switch {
	case revoked != nil:
		return "revoked"
	case expires != nil && !expires.After(now):
		return "expired"
	}
	return "active"
}

// IssueToken adds a token to a bot and returns the secret, the only time it
// exists in clear.
func IssueToken(ctx context.Context, p *db.Pool, botID uuid.UUID, name string, expires *time.Time) (Token, string, error) {
	secret, hash, hint := NewToken()
	var t Token
	err := pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		var one int
		if err := tx.QueryRow(ctx, `SELECT 1 FROM bots WHERE id = $1 FOR UPDATE`, botID).Scan(&one); errors.Is(err, pgx.ErrNoRows) {
			return errNotFound
		} else if err != nil {
			return err
		}
		var active int
		if err := tx.QueryRow(ctx, `
			SELECT COUNT(*) FROM bot_tokens WHERE bot_id = $1 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > NOW())`, botID).Scan(&active); err != nil {
			return err
		}
		if active >= MaxTokens {
			return httpx.ErrConflict(fmt.Sprintf("a bot can have at most %d active tokens", MaxTokens))
		}
		return tx.QueryRow(ctx, `
			INSERT INTO bot_tokens (bot_id, name, token_hash, hint, expires_at) VALUES ($1, $2, $3, $4, $5)
			RETURNING id, name, hint, expires_at, last_used_at, created_at`,
			botID, name, hash, hint, expires).Scan(&t.ID, &t.Name, &t.Hint, &t.ExpiresAt, &t.LastUsedAt, &t.CreatedAt)
	})
	if err != nil {
		if _, ok := httpx.AsAPIError(err); ok {
			return Token{}, "", err
		}
		return Token{}, "", fmt.Errorf("issue token: %w", err)
	}
	t.State = "active"
	return t, secret, nil
}

// RevokeToken ends a token at once. A token already revoked stays so.
func RevokeToken(ctx context.Context, p *db.Pool, botID, tokenID uuid.UUID) error {
	tag, err := p.Exec(ctx, `
		UPDATE bot_tokens SET revoked_at = COALESCE(revoked_at, NOW()) WHERE id = $1 AND bot_id = $2`, tokenID, botID)
	if err != nil {
		return fmt.Errorf("revoke token: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errTokenNotFound
	}
	return nil
}

// CheckExpiry validates an expiry an admin asked for.
func CheckExpiry(t, now time.Time) error {
	if !t.After(now) {
		return httpx.ErrInvalidInput("expires_at must be in the future")
	}
	if t.After(now.Add(MaxTokenLifetime)) {
		return httpx.ErrInvalidInput("expires_at is too far in the future")
	}
	return nil
}
