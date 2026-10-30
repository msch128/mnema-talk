package chat

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// MaxForumGuidelinesLen is the longest text shown above the new-post form.
const MaxForumGuidelinesLen = 1000

var errTagRequired = httpx.ErrInvalidInput("this forum requires at least one tag on every post")

// ForumSettings are the per-forum rules a moderator sets.
type ForumSettings struct {
	// RequireTag: every post carries at least one tag (waived for a member who
	// may apply none, so a forum without usable tags is not closed).
	RequireTag bool `json:"require_tag"`
	// Guidelines is shown above the new-post form (plain text, at most 1000 characters).
	Guidelines string `json:"guidelines" maxLength:"1000"`
}

// ForumSettingsPatch is the set of settings an update changes.
type ForumSettingsPatch struct {
	RequireTag *bool
	Guidelines *string
}

// GetForumSettings returns the settings of a forum; a forum nobody configured has the defaults.
func GetForumSettings(ctx context.Context, q db.Querier, channelID uuid.UUID) (ForumSettings, error) {
	var s ForumSettings
	err := q.QueryRow(ctx, `SELECT require_tag, guidelines FROM forum_settings WHERE channel_id = $1`, channelID).
		Scan(&s.RequireTag, &s.Guidelines)
	if errors.Is(err, pgx.ErrNoRows) {
		return ForumSettings{}, nil
	}
	if err != nil {
		return ForumSettings{}, fmt.Errorf("load forum settings: %w", err)
	}
	return s, nil
}

// UpdateForumSettings changes the set fields and returns the settings before and after.
func UpdateForumSettings(ctx context.Context, p *db.Pool, channelID uuid.UUID, patch ForumSettingsPatch) (updated, previous ForumSettings, err error) {
	if patch.RequireTag == nil && patch.Guidelines == nil {
		return updated, previous, httpx.ErrInvalidInput("no fields to update")
	}
	var guidelines *string
	if patch.Guidelines != nil {
		g, err := httpx.CleanText("guidelines", *patch.Guidelines, MaxForumGuidelinesLen, false)
		if err != nil {
			return updated, previous, err
		}
		guidelines = &g
	}
	err = pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM channels WHERE id = $1 FOR UPDATE`, channelID); err != nil {
			return err
		}
		if previous, err = GetForumSettings(ctx, tx, channelID); err != nil {
			return err
		}
		updated = previous
		if patch.RequireTag != nil {
			updated.RequireTag = *patch.RequireTag
		}
		if guidelines != nil {
			updated.Guidelines = *guidelines
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO forum_settings (channel_id, require_tag, guidelines) VALUES ($1, $2, $3)
			ON CONFLICT (channel_id) DO UPDATE SET require_tag = EXCLUDED.require_tag, guidelines = EXCLUDED.guidelines`,
			channelID, updated.RequireTag, updated.Guidelines)
		return err
	})
	return updated, previous, err
}
