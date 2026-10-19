package auth

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Moderation limits (doc/moderation.md).
const (
	// MinTimeoutSeconds is the shortest timeout.
	MinTimeoutSeconds = 5
	// MaxTimeoutSeconds is the longest timeout: 28 days.
	MaxTimeoutSeconds = 28 * 24 * 3600
	// MinBanSeconds and MaxBanSeconds bound a ban with a duration.
	MinBanSeconds = 60
	MaxBanSeconds = 3650 * 24 * 3600
	// banDisconnectDelay is how long a banned member's socket stays open
	// after the ban, so the notice reaches them.
	banDisconnectDelay = 300 * time.Millisecond
	// MaxReasonLen is the longest reason, in characters.
	MaxReasonLen = 200
)

var (
	errSelfModeration = httpx.ErrInvalidInput("you cannot moderate yourself")
	errNotBanned      = httpx.ErrNotFound("this member is not banned")
)

// TimeoutRequest is the body of POST /api/admin/users/{id}/timeout.
type TimeoutRequest struct {
	// DurationSeconds is how long the member is silenced, from now.
	DurationSeconds int `json:"duration_seconds" minimum:"5" maximum:"2419200"`
	// Reason is shown to the member. Optional.
	Reason string `json:"reason" maxLength:"200" binding:"optional"`
}

// TimeoutState is a member's timeout.
type TimeoutState struct {
	UserID uuid.UUID `json:"user_id" format:"uuid"`
	// TimeoutUntil is when it ends.
	TimeoutUntil time.Time `json:"timeout_until" format:"date-time"`
	Reason       string    `json:"reason"`
}

// notice is what a member is told about a moderation action (WebSocket event
// moderation_notice).
type notice struct {
	Kind    string     `json:"kind"`
	Until   *time.Time `json:"until,omitempty"`
	Seconds int        `json:"duration_seconds,omitempty"`
	Reason  string     `json:"reason"`
}

func cleanReason(s string) (string, error) {
	return httpx.CleanText("reason", s, MaxReasonLen, false)
}

func (h *Handler) mountModeration(r chi.Router) {
	r.With(RequirePerm(h.Perms, perm.TimeoutMembers)).Post("/users/{id}/timeout", httpx.Handle(h.timeoutUser))
	r.With(RequirePerm(h.Perms, perm.TimeoutMembers)).Delete("/users/{id}/timeout", httpx.Handle(h.untimeoutUser))
	bans := r.With(RequirePerm(h.Perms, perm.BanMembers))
	bans.Get("/bans", httpx.Handle(h.listBans))
	bans.Post("/users/{id}/ban", httpx.Handle(h.banUser))
	bans.Delete("/users/{id}/ban", httpx.Handle(h.unbanUser))
}

func (h *Handler) notify(id uuid.UUID, n notice) {
	if h.Events != nil {
		h.Events.SendToUsers([]uuid.UUID{id}, "moderation_notice", n)
	}
}

// moderationTarget is moderated() for actions that may never aim at oneself.
func (h *Handler) moderationTarget(r *http.Request, id uuid.UUID, fn func(q db.Querier) error) error {
	if u := UserFrom(r.Context()); u != nil && u.ID == id {
		return errSelfModeration
	}
	return h.moderated(r, id, fn)
}

// timeoutUser handles POST /api/admin/users/{id}/timeout.
//
// @Summary Time out a member
// @Description Silences a member for a while: they can read but not post, react, speak, share video or join Talks, and are removed from the Talk they are in. It ends by itself. Needs Timeout members and a higher role than the member; the Owner cannot be timed out, nobody can time themselves out. The member is told through a moderation_notice event. Replaces a running timeout.
// @ID timeoutUser
// @Tags Admin
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "User ID." Format(uuid)
// @Param request body TimeoutRequest true "Request body."
// @Success 200 {object} TimeoutState "The timeout."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/users/{id}/timeout [post]
func (h *Handler) timeoutUser(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	var req TimeoutRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if req.DurationSeconds < MinTimeoutSeconds || req.DurationSeconds > MaxTimeoutSeconds {
		return httpx.ErrInvalidInput(fmt.Sprintf("duration_seconds must be between %d and %d", MinTimeoutSeconds, MaxTimeoutSeconds))
	}
	reason, err := cleanReason(req.Reason)
	if err != nil {
		return err
	}
	actor := UserFrom(r.Context())
	var until time.Time
	err = h.moderationTarget(r, id, func(q db.Querier) error {
		return q.QueryRow(r.Context(), `UPDATE users SET timeout_until = NOW() + make_interval(secs => $2),
			timeout_reason = $3, timeout_by = $4, updated_at = NOW()
			WHERE id = $1 RETURNING timeout_until`, id, float64(req.DurationSeconds), reason, actor.ID).Scan(&until)
	})
	if err != nil {
		return err
	}
	// Rebuild the permission state: the member's rights shrink now, and a
	// Talk they are in ends for them.
	h.Perms.Changed([]uuid.UUID{id})
	h.notify(id, notice{Kind: "timeout", Until: &until, Seconds: req.DurationSeconds, Reason: reason})
	h.Events.Broadcast("member_timeout", map[string]any{"id": id, "timeout_until": until})
	httpx.WriteJSON(w, http.StatusOK, TimeoutState{UserID: id, TimeoutUntil: until, Reason: reason})
	return nil
}

// untimeoutUser handles DELETE /api/admin/users/{id}/timeout.
//
// @Summary End a member's timeout
// @Description Lifts a running timeout at once. Needs Timeout members and a higher role than the member. The member is told through a moderation_notice event.
// @ID untimeoutUser
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Param id path string true "User ID." Format(uuid)
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/users/{id}/timeout [delete]
func (h *Handler) untimeoutUser(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	err = h.moderationTarget(r, id, func(q db.Querier) error {
		_, err := q.Exec(r.Context(), `UPDATE users SET timeout_until = NULL, timeout_reason = '', timeout_by = NULL, updated_at = NOW() WHERE id = $1`, id)
		return err
	})
	if err != nil {
		return err
	}
	h.Perms.Changed([]uuid.UUID{id})
	h.notify(id, notice{Kind: "timeout_removed"})
	h.Events.Broadcast("member_timeout", map[string]any{"id": id, "timeout_until": nil})
	w.WriteHeader(http.StatusNoContent)
	return nil
}
