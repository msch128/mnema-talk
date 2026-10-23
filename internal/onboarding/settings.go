package onboarding

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// Limits of the texts the Owner writes.
const (
	maxCommunityName = 60
	maxWelcomeText   = 4000
	maxRulesText     = 10000
)

// Settings is the community's onboarding configuration.
type Settings struct {
	CommunityName string `json:"community_name" maxLength:"60"`
	// WelcomeText is Markdown, shown to new members first.
	WelcomeText string `json:"welcome_text" maxLength:"4000"`
	// RulesText is Markdown. Empty means no rules: nothing is enforced.
	RulesText string `json:"rules_text" maxLength:"10000"`
	// RulesVersion counts the published versions of the rules (0: never set).
	// Members must have accepted it before they may post, react, upload or
	// join a Talk (only while RulesText is not empty).
	RulesVersion int `json:"rules_version" minimum:"0"`
	// StartChannelID is the text channel new members land in; null for the
	// first channel they can see.
	StartChannelID *uuid.UUID `json:"start_channel_id" format:"uuid" extensions:"x-nullable"`
}

// SettingsInput is the body of PUT /api/admin/onboarding; it replaces the
// configuration.
type SettingsInput struct {
	CommunityName  string     `json:"community_name" maxLength:"60"`
	WelcomeText    string     `json:"welcome_text" maxLength:"4000"`
	RulesText      string     `json:"rules_text" maxLength:"10000"`
	StartChannelID *uuid.UUID `json:"start_channel_id" format:"uuid" extensions:"x-nullable"`
	// RequireReacceptance raises the rules version, so every member (the Owner
	// excepted) has to accept the rules again. The first time rules are
	// written the version is raised on its own. Ignored while the rules are
	// empty.
	RequireReacceptance bool `json:"require_reacceptance" binding:"optional"`
}

const settingsColumns = `community_name, welcome_text, rules_text, rules_version, start_channel_id`

func scanSettings(row pgx.Row) (Settings, error) {
	var s Settings
	err := row.Scan(&s.CommunityName, &s.WelcomeText, &s.RulesText, &s.RulesVersion, &s.StartChannelID)
	return s, err
}

// getSettings handles GET /api/admin/onboarding.
//
// @Summary Read the onboarding settings
// @Description Owner and Administrators only.
// @ID getOnboardingSettings
// @Tags Onboarding
// @Produce json
// @Security cookieAuth
// @Success 200 {object} Settings "Settings."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: only the Owner and members with the Administrator permission may read it."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/onboarding [get]
func (h *Handler) getSettings(w http.ResponseWriter, r *http.Request) error {
	s, err := scanSettings(h.DB.QueryRow(r.Context(), `SELECT `+settingsColumns+` FROM instance_settings`))
	if err != nil {
		return fmt.Errorf("read onboarding settings: %w", err)
	}
	httpx.WriteJSON(w, http.StatusOK, s)
	return nil
}

// putSettings handles PUT /api/admin/onboarding.
//
// @Summary Set the onboarding settings
// @Description Owner and Administrators only; audited. Replaces the community name, welcome text, rules and start channel. Writing rules for the first time, or `require_reacceptance`, raises the rules version: members then have to accept again before they may post, react, upload or join a Talk (the Owner and bots are exempt). Empty rules enforce nothing. Broadcasts permissions_changed.
// @ID setOnboardingSettings
// @Tags Onboarding
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param body body SettingsInput true "New settings."
// @Success 200 {object} Settings "Settings."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: only the Owner and members with the Administrator permission may change it, or cross-origin request rejected."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/onboarding [put]
func (h *Handler) putSettings(w http.ResponseWriter, r *http.Request) error {
	var in SettingsInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	if err := cleanSettings(&in); err != nil {
		return err
	}
	var out Settings
	err := pgx.BeginFunc(r.Context(), h.DB, func(tx pgx.Tx) error {
		if err := checkStartChannel(r.Context(), tx, in.StartChannelID); err != nil {
			return err
		}
		var err error
		out, err = applySettings(r.Context(), tx, in)
		return err
	})
	if err != nil {
		return err
	}
	// The rules in force may have changed: everybody's rights with them.
	h.Perms.Changed(nil)
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

func cleanSettings(in *SettingsInput) error {
	var err error
	if in.CommunityName, err = httpx.CleanText("community_name", in.CommunityName, maxCommunityName, false); err != nil {
		return err
	}
	if in.WelcomeText, err = httpx.CleanText("welcome_text", in.WelcomeText, maxWelcomeText, false); err != nil {
		return err
	}
	in.RulesText, err = httpx.CleanText("rules_text", in.RulesText, maxRulesText, false)
	return err
}

// checkStartChannel requires the start channel to be an existing text channel.
func checkStartChannel(ctx context.Context, q db.Querier, id *uuid.UUID) error {
	if id == nil {
		return nil
	}
	var kind string
	err := q.QueryRow(ctx, `SELECT type FROM channels WHERE id = $1`, *id).Scan(&kind)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && kind != "text") {
		return httpx.ErrInvalidInput("start_channel_id must be a text channel")
	}
	if err != nil {
		return fmt.Errorf("read start channel: %w", err)
	}
	return nil
}

// nextRulesVersion decides the version after a save: the first rules ever
// written start at 1, and a demanded re-acceptance counts one up. Empty rules
// keep the version where it was.
func nextRulesVersion(current int, in SettingsInput) int {
	switch {
	case in.RulesText == "":
		return current
	case current == 0 || in.RequireReacceptance:
		return current + 1
	}
	return current
}

func applySettings(ctx context.Context, tx pgx.Tx, in SettingsInput) (Settings, error) {
	var current int
	if err := tx.QueryRow(ctx, `SELECT rules_version FROM instance_settings FOR UPDATE`).Scan(&current); err != nil {
		return Settings{}, fmt.Errorf("lock onboarding settings: %w", err)
	}
	row := tx.QueryRow(ctx, `UPDATE instance_settings SET community_name = $1, welcome_text = $2, rules_text = $3,
		rules_version = $4, start_channel_id = $5, updated_at = NOW() RETURNING `+settingsColumns,
		in.CommunityName, in.WelcomeText, in.RulesText, nextRulesVersion(current, in), in.StartChannelID)
	s, err := scanSettings(row)
	if err != nil {
		return Settings{}, fmt.Errorf("write onboarding settings: %w", err)
	}
	return s, nil
}
