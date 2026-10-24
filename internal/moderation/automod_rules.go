// Package moderation serves the moderators' tools of 1.2: the automod rules
// (Administrator). The rules themselves run in internal/automod; this package
// is only their REST surface.
package moderation

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/automod"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Handler serves the automod routes.
type Handler struct {
	DB    *db.Pool
	Perms *perm.Service
	// Engine is told when the rules change; nil in tests that only store them.
	Engine *automod.Engine
}

// MountAdmin registers the routes below /api/admin.
func (h *Handler) MountAdmin(r chi.Router) {
	cfg := r.With(auth.RequirePerm(h.Perms, perm.Administrator))
	cfg.Get("/automod", httpx.Handle(h.getAutomod))
	cfg.Post("/automod/rules", httpx.Handle(h.createRule))
	cfg.Put("/automod/rules/{id}", httpx.Handle(h.updateRule))
	cfg.Delete("/automod/rules/{id}", httpx.Handle(h.deleteRule))
}

// AutomodList is the answer of GET /api/admin/automod.
type AutomodList struct {
	Rules []automod.Rule `json:"rules"`
	// RaidUntil is set while raid protection is restricting newcomers.
	RaidUntil *time.Time `json:"raid_until,omitempty" binding:"optional" format:"date-time"`
}

func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

// getAutomod handles GET /api/admin/automod.
//
// @Summary List automod rules
// @Description The automod rules in the order they were made, and the end of the running raid restriction if there is one. Needs the Administrator permission (the Owner always). The rules check every new or edited message and every sign-up; see doc/moderation.md.
// @ID getAutomod
// @Tags Moderation
// @Produce json
// @Security cookieAuth
// @Success 200 {object} AutomodList "The rules."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Administrator permission."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/automod [get]
func (h *Handler) getAutomod(w http.ResponseWriter, r *http.Request) error {
	rules, _, err := automod.ListRules(r.Context(), h.DB)
	if err != nil {
		return err
	}
	out := AutomodList{Rules: rules}
	if err := h.DB.QueryRow(r.Context(), `SELECT raid_until FROM automod_state WHERE raid_until > NOW()`).Scan(&out.RaidUntil); err != nil && !isNoRows(err) {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

// parseRule reads and validates a rule body and checks the roles and channels
// it exempts exist.
func (h *Handler) parseRule(r *http.Request) (automod.RuleInput, error) {
	var in automod.RuleInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return in, err
	}
	in, err := in.Normalize()
	if err != nil {
		return in, err
	}
	snap, err := h.Perms.Snapshot(r.Context())
	if err != nil {
		return in, err
	}
	for _, id := range in.ExemptRoles {
		if _, ok := snap.Role(id); !ok {
			return in, httpx.ErrInvalidInput("exempt_roles: unknown role " + id.String())
		}
	}
	for _, id := range in.ExemptChannels {
		if !snap.KnowsChannel(id) {
			return in, httpx.ErrInvalidInput("exempt_channels: unknown channel " + id.String())
		}
	}
	return in, nil
}

// createRule handles POST /api/admin/automod/rules.
//
// @Summary Create an automod rule
// @Description Adds a rule: blocked words, link filter, mention spam, new-account protection or raid protection (only one). At most 25 rules. Needs the Administrator permission. Recorded in the audit log as automod.rule_create.
// @ID createAutomodRule
// @Tags Moderation
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body automod.RuleInput true "Request body."
// @Success 201 {object} automod.Rule "The rule."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Administrator permission."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: 25 rules exist, or raid protection exists already."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/automod/rules [post]
func (h *Handler) createRule(w http.ResponseWriter, r *http.Request) error {
	in, err := h.parseRule(r)
	if err != nil {
		return err
	}
	rule, err := automod.CreateRule(r.Context(), h.DB, in)
	if err != nil {
		return err
	}
	h.Engine.Invalidate()
	httpx.WriteJSON(w, http.StatusCreated, rule)
	return nil
}

// updateRule handles PUT /api/admin/automod/rules/{id}.
//
// @Summary Replace an automod rule
// @Description Replaces every setting of a rule; its kind cannot change. Needs the Administrator permission. Recorded in the audit log as automod.rule_update.
// @ID updateAutomodRule
// @Tags Moderation
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "Rule ID." Format(uuid)
// @Param request body automod.RuleInput true "Request body."
// @Success 200 {object} automod.Rule "The rule."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Administrator permission."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/automod/rules/{id} [put]
func (h *Handler) updateRule(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	in, err := h.parseRule(r)
	if err != nil {
		return err
	}
	rule, err := automod.UpdateRule(r.Context(), h.DB, id, in)
	if err != nil {
		return err
	}
	h.Engine.Invalidate()
	httpx.WriteJSON(w, http.StatusOK, rule)
	return nil
}

// deleteRule handles DELETE /api/admin/automod/rules/{id}.
//
// @Summary Delete an automod rule
// @Description Removes a rule. The entries it already made in the moderation queue stay. Needs the Administrator permission. Recorded in the audit log as automod.rule_delete.
// @ID deleteAutomodRule
// @Tags Moderation
// @Security cookieAuth
// @Param id path string true "Rule ID." Format(uuid)
// @Success 204 "Deleted."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Administrator permission."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/automod/rules/{id} [delete]
func (h *Handler) deleteRule(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	if _, err := automod.DeleteRule(r.Context(), h.DB, id); err != nil {
		return err
	}
	h.Engine.Invalidate()
	w.WriteHeader(http.StatusNoContent)
	return nil
}
