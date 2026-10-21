// Package webhook implements incoming webhooks: a per-channel URL that posts
// messages in the Discord execute format. The messages are authored by a
// hidden bot account per webhook, never by a member.
package webhook

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/media"
)

const (
	// MaxNameLen is the longest webhook name (Discord allows 80 as well).
	MaxNameLen = 80
	// botNameLen is the width of users.display_name.
	botNameLen = 24
	// maxPerChannel bounds the webhooks of one channel (Discord: 15).
	maxPerChannel = 15
)

var errNotFound = httpx.ErrNotFound("webhook not found")

// Settings are what an incoming webhook may do. An administrator sets them for
// each webhook; the webhook posts with exactly these, whoever made it.
type Settings struct {
	// AllowEveryone lets @all and @here notify people (default off). It also
	// takes "everyone" in the sender's allowed_mentions.
	AllowEveryone bool `json:"allow_everyone"`
	// AllowAttachments lets a post carry files (default on).
	AllowAttachments bool `json:"allow_attachments"`
	// AllowEmbeds lets a post carry embeds (default on).
	AllowEmbeds bool `json:"allow_embeds"`
	// AllowUserMentions lets @username notify members (default on, as Discord
	// does with allowed_mentions.parse = users).
	AllowUserMentions bool `json:"allow_user_mentions"`
	// AllowIdentityOverride lets a post set its own username (default on).
	// Avatars from other hosts are never used.
	AllowIdentityOverride bool `json:"allow_identity_override"`
}

// DefaultSettings are the settings of a new webhook.
var DefaultSettings = Settings{AllowAttachments: true, AllowEmbeds: true, AllowUserMentions: true, AllowIdentityOverride: true}

// SettingsPatch changes some settings; nil leaves one as it is.
type SettingsPatch struct {
	AllowEveryone         *bool `json:"allow_everyone,omitempty" binding:"optional"`
	AllowAttachments      *bool `json:"allow_attachments,omitempty" binding:"optional"`
	AllowEmbeds           *bool `json:"allow_embeds,omitempty" binding:"optional"`
	AllowUserMentions     *bool `json:"allow_user_mentions,omitempty" binding:"optional"`
	AllowIdentityOverride *bool `json:"allow_identity_override,omitempty" binding:"optional"`
}

// apply returns s with the patch applied.
func (p SettingsPatch) apply(s Settings) Settings {
	set := func(dst *bool, v *bool) {
		if v != nil {
			*dst = *v
		}
	}
	set(&s.AllowEveryone, p.AllowEveryone)
	set(&s.AllowAttachments, p.AllowAttachments)
	set(&s.AllowEmbeds, p.AllowEmbeds)
	set(&s.AllowUserMentions, p.AllowUserMentions)
	set(&s.AllowIdentityOverride, p.AllowIdentityOverride)
	return s
}

// Webhook is a webhook as the management API shows it. The token is only
// ever shown once, in CreatedWebhook.
type Webhook struct {
	ID        uuid.UUID `json:"id" format:"uuid"`
	ChannelID uuid.UUID `json:"channel_id" format:"uuid"`
	Name      string    `json:"name" maxLength:"80"`
	// AvatarURL is the API URL of the avatar; omitted when none is set.
	AvatarURL string `json:"avatar_url,omitempty" binding:"optional"`
	// CreatedBy is the administrator who created it, kept for the record (null
	// once that account is gone). It grants nothing.
	CreatedBy *uuid.UUID `json:"created_by" format:"uuid" extensions:"x-nullable"`
	CreatedAt time.Time  `json:"created_at" format:"date-time"`
	UpdatedAt time.Time  `json:"updated_at" format:"date-time"`
	Settings
}

// CreatedWebhook is the answer to creating a webhook or regenerating its
// token: the only time the secret is shown.
type CreatedWebhook struct {
	Webhook
	// Token is the secret part of the URL. It is stored hashed and cannot be
	// shown again; regenerate the token to get a new URL.
	Token string `json:"token"`
	// URL is the full execute URL, token included.
	URL string `json:"url"`
}

// record is a webhook with what only the server needs.
type record struct {
	Webhook
	botUserID uuid.UUID
	tokenHash []byte
}

const recordSelect = `
	SELECT w.id, w.channel_id, w.name, u.avatar_s3_key, w.created_by, w.created_at, w.updated_at, w.bot_user_id, w.token_hash,
	       w.allow_everyone, w.allow_attachments, w.allow_embeds, w.allow_user_mentions, w.allow_identity_override
	FROM webhooks w JOIN users u ON u.id = w.bot_user_id`

func scanRecord(row pgx.Row) (*record, error) {
	var rec record
	var avatar *string
	err := row.Scan(&rec.ID, &rec.ChannelID, &rec.Name, &avatar, &rec.CreatedBy, &rec.CreatedAt, &rec.UpdatedAt, &rec.botUserID, &rec.tokenHash,
		&rec.AllowEveryone, &rec.AllowAttachments, &rec.AllowEmbeds, &rec.AllowUserMentions, &rec.AllowIdentityOverride)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load webhook: %w", err)
	}
	rec.AvatarURL = auth.AvatarURL(avatar)
	return &rec, nil
}

func get(ctx context.Context, q db.Querier, id uuid.UUID) (*record, error) {
	return scanRecord(q.QueryRow(ctx, recordSelect+` WHERE w.id = $1`, id))
}

func list(ctx context.Context, p *db.Pool, channel uuid.UUID) ([]Webhook, error) {
	rows, err := p.Query(ctx, recordSelect+` WHERE w.channel_id = $1 ORDER BY w.created_at, w.id`, channel)
	if err != nil {
		return nil, fmt.Errorf("list webhooks: %w", err)
	}
	recs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (*record, error) { return scanRecord(row) })
	if err != nil {
		return nil, err
	}
	hooks := make([]Webhook, len(recs))
	for i, rec := range recs {
		hooks[i] = rec.Webhook
	}
	return hooks, nil
}

// newToken returns a fresh secret and its hash. The token carries 256 random
// bits, so a plain SHA-256 is enough to store it.
func newToken() (token string, hash []byte) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, hashToken(token)
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// tokenMatches compares in constant time.
func (r *record) tokenMatches(token string) bool {
	return subtle.ConstantTimeCompare(r.tokenHash, hashToken(token)) == 1
}

// cleanName trims and validates a webhook name or username override.
func cleanName(field, s string) (string, error) {
	s, err := httpx.CleanText(field, s, MaxNameLen, true)
	if err != nil {
		return "", err
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return "", httpx.ErrInvalidInput(field + " contains control characters")
		}
	}
	return s, nil
}

func botDisplayName(name string) string {
	r := []rune(name)
	if len(r) > botNameLen {
		r = r[:botNameLen]
	}
	return string(r)
}

// create stores a webhook with its bot account and returns it with the
// secret token.
func create(ctx context.Context, p *db.Pool, channel, creator uuid.UUID, name string, st Settings) (*record, string, error) {
	token, hash := newToken()
	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		panic(err)
	}
	var id uuid.UUID
	err := pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM webhooks WHERE channel_id = $1`, channel).Scan(&n); err != nil {
			return err
		}
		if n >= maxPerChannel {
			return httpx.ErrConflict(fmt.Sprintf("a channel can have at most %d webhooks", maxPerChannel))
		}
		var bot uuid.UUID
		// "!" is no password hash: the account can never sign in (Login, sessions and
		// re-authentication skip bots).
		if err := tx.QueryRow(ctx, `
			INSERT INTO users (username, display_name, password_hash, role, is_bot)
			VALUES ($1, $2, '!', 'user', TRUE) RETURNING id`,
			"wh-"+hex.EncodeToString(suffix), botDisplayName(name)).Scan(&bot); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `
			INSERT INTO webhooks (channel_id, bot_user_id, name, token_hash, created_by,
			    allow_everyone, allow_attachments, allow_embeds, allow_user_mentions, allow_identity_override)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`,
			channel, bot, name, hash, creator,
			st.AllowEveryone, st.AllowAttachments, st.AllowEmbeds, st.AllowUserMentions, st.AllowIdentityOverride).Scan(&id)
	})
	if err != nil {
		if _, ok := httpx.AsAPIError(err); ok {
			return nil, "", err
		}
		return nil, "", fmt.Errorf("create webhook: %w", err)
	}
	rec, err := get(ctx, p, id)
	return rec, token, err
}

// update changes the name and the settings (nil/empty leaves them) and the
// bot account's name with it.
func update(ctx context.Context, p *db.Pool, rec *record, name *string, st Settings) error {
	return pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		if name != nil {
			if _, err := tx.Exec(ctx, `UPDATE users SET display_name = $1, updated_at = NOW() WHERE id = $2`, botDisplayName(*name), rec.botUserID); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `
			UPDATE webhooks SET name = COALESCE($2, name), updated_at = NOW(),
			    allow_everyone = $3, allow_attachments = $4, allow_embeds = $5, allow_user_mentions = $6, allow_identity_override = $7
			WHERE id = $1`, rec.ID, name, st.AllowEveryone, st.AllowAttachments, st.AllowEmbeds, st.AllowUserMentions, st.AllowIdentityOverride)
		return err
	})
}

// regenerate makes a new token and changes nothing else.
func regenerate(ctx context.Context, p *db.Pool, rec *record) (string, error) {
	token, hash := newToken()
	if _, err := p.Exec(ctx, `UPDATE webhooks SET token_hash = $1, updated_at = NOW() WHERE id = $2`, hash, rec.ID); err != nil {
		return "", fmt.Errorf("regenerate webhook token: %w", err)
	}
	return token, nil
}

func cleanupWebhookAvatar(ctx context.Context, p *db.Pool, store media.Store, avatarURL string) {
	if store == nil || avatarURL == "" {
		return
	}
	id, err := uuid.Parse(path.Base(avatarURL))
	if err != nil {
		return
	}
	if err := media.DeleteMedia(ctx, p, store, id); err != nil {
		slog.Warn("could not delete webhook avatar", "media_id", id, "err", err)
	}
}

func deleteWebhookRecords(ctx context.Context, p *db.Pool, webhookID, botUserID uuid.UUID) error {
	return pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM webhooks WHERE id = $1`, webhookID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			DELETE FROM users WHERE id = $1 AND is_bot AND NOT EXISTS (SELECT 1 FROM messages WHERE user_id = $1)`, botUserID)
		return err
	})
}

// remove deletes the webhook. Its bot account stays while messages by it
// exist, so history keeps its author; without any, the account goes too, and
// its avatar object with it (store may be nil).
func remove(ctx context.Context, p *db.Pool, store media.Store, rec *record) error {
	var hasMessages bool
	if err := p.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM messages WHERE user_id = $1)`, rec.botUserID).Scan(&hasMessages); err != nil {
		return fmt.Errorf("delete webhook: %w", err)
	}
	if !hasMessages {
		cleanupWebhookAvatar(ctx, p, store, rec.AvatarURL)
	}
	if err := deleteWebhookRecords(ctx, p, rec.ID, rec.botUserID); err != nil {
		return fmt.Errorf("delete webhook: %w", err)
	}
	return nil
}
