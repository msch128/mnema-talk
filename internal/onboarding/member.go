package onboarding

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// State is what a member sees of the onboarding.
type State struct {
	CommunityName string `json:"community_name"`
	WelcomeText   string `json:"welcome_text"`
	RulesText     string `json:"rules_text"`
	// RulesVersion is the version in force; 0 while there are no rules.
	RulesVersion int `json:"rules_version" minimum:"0"`
	// RulesAcceptedVersion is the version the member accepted last.
	RulesAcceptedVersion int `json:"rules_accepted_version" minimum:"0"`
	// RulesPending is true while the member has to accept the rules; until
	// then posting, reacting, uploading and joining a Talk answer
	// RULES_NOT_ACCEPTED. Never true for the Owner.
	RulesPending bool `json:"rules_pending"`
	// StartChannelID is the channel to open after the welcome flow; null when
	// none is set or the member cannot view it.
	StartChannelID *uuid.UUID `json:"start_channel_id" format:"uuid" extensions:"x-nullable"`
	// DismissedTips are the tips the member closed.
	DismissedTips []string `json:"dismissed_tips"`
	// WelcomeDone is true once the member finished the welcome flow.
	WelcomeDone bool `json:"welcome_done"`
}

// stored is the part of users.onboarding the server knows; the rest of the
// JSON is left alone.
type stored struct {
	Dismissed   []string `json:"dismissed"`
	WelcomeDone bool     `json:"welcome_done"`
}

func parseStored(raw []byte) stored {
	var s stored
	// Malformed JSON cannot be written through the API; start over if it ever is.
	_ = json.Unmarshal(raw, &s)
	if s.Dismissed == nil {
		s.Dismissed = []string{}
	}
	return s
}

// getState handles GET /api/onboarding.
//
// @Summary Read the onboarding state
// @Description The community's name, welcome text and rules, whether the caller still has to accept the rules, the start channel (if the caller can view it) and the caller's dismissed tips.
// @ID getOnboarding
// @Tags Onboarding
// @Produce json
// @Security cookieAuth
// @Success 200 {object} State "State."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/onboarding [get]
func (h *Handler) getState(w http.ResponseWriter, r *http.Request) error {
	st, err := h.state(r)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, st)
	return nil
}

func (h *Handler) state(r *http.Request) (State, error) {
	user := auth.UserFrom(r.Context())
	var st State
	var raw []byte
	var set Settings
	err := h.DB.QueryRow(r.Context(), `
		SELECT s.community_name, s.welcome_text, s.rules_text, s.rules_version, s.start_channel_id,
		       u.rules_accepted_version, u.onboarding
		FROM instance_settings s, users u WHERE u.id = $1`, user.ID).
		Scan(&set.CommunityName, &set.WelcomeText, &set.RulesText, &set.RulesVersion, &set.StartChannelID, &st.RulesAcceptedVersion, &raw)
	if err != nil {
		return State{}, fmt.Errorf("read onboarding state: %w", err)
	}
	st.CommunityName, st.WelcomeText, st.RulesText = set.CommunityName, set.WelcomeText, set.RulesText
	if set.RulesText != "" {
		st.RulesVersion = set.RulesVersion
	}
	st.RulesPending = !user.IsAdmin() && st.RulesVersion > 0 && st.RulesAcceptedVersion < st.RulesVersion
	mine := parseStored(raw)
	st.DismissedTips, st.WelcomeDone = mine.Dismissed, mine.WelcomeDone
	st.StartChannelID, err = h.visibleChannel(r.Context(), user, set.StartChannelID)
	return st, err
}

// visibleChannel returns id only when the member may view that channel.
func (h *Handler) visibleChannel(ctx context.Context, user *auth.User, id *uuid.UUID) (*uuid.UUID, error) {
	if id == nil {
		return nil, nil
	}
	p, err := h.Perms.Channel(ctx, user.Subject(), *id)
	if err != nil || !p.Has(perm.ViewChannel) {
		return nil, err
	}
	return id, nil
}

// AcceptInput is the body of POST /api/onboarding/accept.
type AcceptInput struct {
	// Version is the rules version the member saw (State.RulesVersion).
	Version int `json:"version" minimum:"0"`
}

// accept handles POST /api/onboarding/accept.
//
// @Summary Accept the community rules
// @Description Records that the caller accepted the rules in force. `version` must be the current rules version; if the rules changed meanwhile the answer is 409 and the client reloads them. Lifts the RULES_NOT_ACCEPTED restrictions at once, including in the WebSocket hub.
// @ID acceptRules
// @Tags Onboarding
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param body body AcceptInput true "Accepted version."
// @Success 204 "Accepted."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the rules changed; reload them."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/onboarding/accept [post]
func (h *Handler) accept(w http.ResponseWriter, r *http.Request) error {
	var in AcceptInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	user := auth.UserFrom(r.Context())
	var current int
	err := h.DB.QueryRow(r.Context(), `SELECT CASE WHEN rules_text <> '' THEN rules_version ELSE 0 END FROM instance_settings`).Scan(&current)
	if err != nil {
		return fmt.Errorf("read rules version: %w", err)
	}
	if in.Version != current {
		return httpx.ErrConflict("the rules changed; reload them")
	}
	if current == 0 {
		// No rules, nothing to accept.
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
	// GREATEST: a slow request must not move an acceptance backwards.
	if _, err := h.DB.Exec(r.Context(), `UPDATE users SET rules_accepted_version = GREATEST(rules_accepted_version, $2), updated_at = NOW() WHERE id = $1`, user.ID, current); err != nil {
		return fmt.Errorf("accept rules: %w", err)
	}
	h.Perms.Changed([]uuid.UUID{user.ID})
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// TipsInput is the body of PUT /api/onboarding/tips. Dismissals are added to
// what is stored, never removed, so two devices cannot undo each other.
type TipsInput struct {
	// DismissedTips are tip ids to add (at most 32 per call, 64 stored).
	DismissedTips []string `json:"dismissed_tips" binding:"optional" maxItems:"32"`
	// WelcomeDone marks the welcome flow finished.
	WelcomeDone *bool `json:"welcome_done" binding:"optional" extensions:"x-nullable"`
}

const maxStoredTips = 64

var tipIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,39}$`)

func validTips(ids []string) error {
	if len(ids) > 32 {
		return httpx.ErrInvalidInput("at most 32 tips per request")
	}
	for _, id := range ids {
		if !tipIDPattern.MatchString(id) {
			return httpx.ErrInvalidInput("invalid tip id")
		}
	}
	return nil
}

// putTips handles PUT /api/onboarding/tips.
//
// @Summary Store dismissed tips
// @Description Adds tip ids to the caller's dismissed tips and optionally marks the welcome flow finished; returns the resulting state.
// @ID putOnboardingTips
// @Tags Onboarding
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param body body TipsInput true "Tips to dismiss."
// @Success 200 {object} State "State."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/onboarding/tips [put]
func (h *Handler) putTips(w http.ResponseWriter, r *http.Request) error {
	var in TipsInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	if err := validTips(in.DismissedTips); err != nil {
		return err
	}
	user := auth.UserFrom(r.Context())
	err := pgx.BeginFunc(r.Context(), h.DB, func(tx pgx.Tx) error {
		var raw []byte
		if err := tx.QueryRow(r.Context(), `SELECT onboarding FROM users WHERE id = $1 FOR UPDATE`, user.ID).Scan(&raw); err != nil {
			return fmt.Errorf("lock onboarding: %w", err)
		}
		merged, err := mergeTips(raw, in)
		if err != nil {
			return err
		}
		_, err = tx.Exec(r.Context(), `UPDATE users SET onboarding = $2 WHERE id = $1`, user.ID, merged)
		return err
	})
	if err != nil {
		return err
	}
	return h.getState(w, r)
}

// mergeTips adds the request to the stored JSON, keeping keys it does not know.
func mergeTips(raw []byte, in TipsInput) ([]byte, error) {
	doc := map[string]json.RawMessage{}
	_ = json.Unmarshal(raw, &doc)
	cur := parseStored(raw)
	for _, id := range in.DismissedTips {
		if !slices.Contains(cur.Dismissed, id) {
			cur.Dismissed = append(cur.Dismissed, id)
		}
	}
	if len(cur.Dismissed) > maxStoredTips {
		return nil, httpx.ErrInvalidInput("too many dismissed tips")
	}
	if in.WelcomeDone != nil {
		cur.WelcomeDone = *in.WelcomeDone
	}
	for key, value := range map[string]any{"dismissed": cur.Dismissed, "welcome_done": cur.WelcomeDone} {
		b, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		doc[key] = b
	}
	return json.Marshal(doc)
}
