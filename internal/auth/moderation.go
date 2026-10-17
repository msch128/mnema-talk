package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/audit"
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

// BanRequest is the body of POST /api/admin/users/{id}/ban.
type BanRequest struct {
	// Reason is shown to the member when they try to sign in. Optional.
	Reason string `json:"reason" maxLength:"200" binding:"optional"`
	// DurationSeconds ends the ban by itself after that long; null or
	// missing bans for good.
	DurationSeconds *int `json:"duration_seconds" minimum:"60" maximum:"315360000" extensions:"x-nullable" binding:"optional"`
	// RevokeInvite also revokes the invite the account registered with, so
	// that code cannot create the account anew (default true).
	RevokeInvite *bool `json:"revoke_invite" binding:"optional"`
}

// Ban is an active ban as the admin list shows it.
type Ban struct {
	UserID      uuid.UUID `json:"user_id" format:"uuid"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	Reason      string    `json:"reason"`
	BannedAt    time.Time `json:"banned_at" format:"date-time"`
	// BannedUntil is null for a ban without an end.
	BannedUntil *time.Time `json:"banned_until" format:"date-time" extensions:"x-nullable"`
	// BannedBy names the moderator; empty when that account is gone.
	BannedBy string `json:"banned_by"`
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

// banUser handles POST /api/admin/users/{id}/ban.
//
// @Summary Ban a member
// @Description Ends the member's sessions and connections and blocks signing in, with an optional reason and an optional duration (otherwise for good). The account and its messages stay. By default the invite the account registered with is revoked, so that code cannot create the account again. Needs Ban members and a higher role than the member; the Owner cannot be banned. Kicking someone without a record is Disable.
// @ID banUser
// @Tags Admin
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "User ID." Format(uuid)
// @Param request body BanRequest true "Request body."
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/users/{id}/ban [post]
func (h *Handler) banUser(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	var req BanRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	reason, err := cleanReason(req.Reason)
	if err != nil {
		return err
	}
	var until *time.Time
	if req.DurationSeconds != nil {
		if *req.DurationSeconds < MinBanSeconds || *req.DurationSeconds > MaxBanSeconds {
			return httpx.ErrInvalidInput(fmt.Sprintf("duration_seconds must be between %d and %d", MinBanSeconds, MaxBanSeconds))
		}
		t := time.Now().Add(time.Duration(*req.DurationSeconds) * time.Second)
		until = &t
	}
	revoke := req.RevokeInvite == nil || *req.RevokeInvite
	actor := UserFrom(r.Context())
	err = h.moderationTarget(r, id, func(q db.Querier) error {
		return setBan(r.Context(), q, id, actor.ID, reason, until, revoke)
	})
	if err != nil {
		return err
	}
	// Tell them first; the connection closes a moment later so the notice
	// is written. Their sessions are already dead, so nothing can be done
	// with the socket meanwhile.
	h.notify(id, notice{Kind: "ban", Until: until, Reason: reason})
	if h.Live != nil {
		h.Live.KickFromVoice(id)
		time.AfterFunc(banDisconnectDelay, func() { h.Live.DisconnectUser(id) })
	}
	h.Events.Broadcast("user_update", map[string]any{"id": id, "disabled": true})
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// setBan records a ban: it blocks every session and sign-in (see ActiveSQL),
// its sessions end, and optionally its invite is revoked.
func setBan(ctx context.Context, q db.Querier, id, by uuid.UUID, reason string, until *time.Time, revokeInvite bool) error {
	// The ban is its own state: disabled_at is neither set nor cleared, so
	// lifting the ban never re-enables an account an admin disabled, and a
	// running timeout stays what it was.
	if _, err := q.Exec(ctx, `UPDATE users SET banned_at = NOW(), banned_until = $2, ban_reason = $3, banned_by = $4,
		token_version = token_version + 1, updated_at = NOW() WHERE id = $1`, id, until, reason, by); err != nil {
		return err
	}
	if revokeInvite {
		_, err := q.Exec(ctx, `DELETE FROM invites WHERE id = (SELECT invite_id FROM users WHERE id = $1)`, id)
		return err
	}
	return nil
}

// unbanUser handles DELETE /api/admin/users/{id}/ban.
//
// @Summary Lift a ban
// @Description Lets the member sign in again. A revoked invite stays revoked. Needs Ban members and a higher role than the member.
// @ID unbanUser
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Param id path string true "User ID." Format(uuid)
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the member does not exist or is not banned."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/users/{id}/ban [delete]
func (h *Handler) unbanUser(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	err = h.moderationTarget(r, id, func(q db.Querier) error {
		tag, err := q.Exec(r.Context(), clearBanSQL+` AND banned_at IS NOT NULL`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errNotBanned
		}
		return nil
	})
	if err != nil {
		return err
	}
	h.Events.Broadcast("user_update", map[string]any{"id": id, "disabled": false})
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// clearBanSQL lifts a ban and the disabling that came with it.
const clearBanSQL = `UPDATE users SET banned_at = NULL, banned_until = NULL,
	ban_reason = '', banned_by = NULL, updated_at = NOW() WHERE id = $1`

// listBans handles GET /api/admin/bans.
//
// @Summary List bans
// @Description Active bans, newest first. Needs Ban members.
// @ID listBans
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Success 200 {array} Ban "Bans."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/bans [get]
func (h *Handler) listBans(w http.ResponseWriter, r *http.Request) error {
	rows, err := h.Sessions.DB.Query(r.Context(), `
		SELECT u.id, u.username, u.display_name, u.ban_reason, u.banned_at, u.banned_until, COALESCE(b.username, '')
		FROM users u LEFT JOIN users b ON b.id = u.banned_by
		WHERE u.banned_at IS NOT NULL ORDER BY u.banned_at DESC`)
	if err != nil {
		return err
	}
	defer rows.Close()
	bans := []Ban{}
	for rows.Next() {
		var b Ban
		if err := rows.Scan(&b.UserID, &b.Username, &b.DisplayName, &b.Reason, &b.BannedAt, &b.BannedUntil, &b.BannedBy); err != nil {
			return err
		}
		bans = append(bans, b)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, bans)
	return nil
}

// bannedError is the sign-in refusal for a banned account: the member sees
// why and until when.
func bannedError(ctx context.Context, p db.Querier, id uuid.UUID) error {
	var reason string
	var until *time.Time
	err := p.QueryRow(ctx, `SELECT ban_reason, banned_until FROM users
		WHERE id = $1 AND banned_at IS NOT NULL AND (banned_until IS NULL OR banned_until > NOW())`, id).Scan(&reason, &until)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAccountDisabled
	}
	if err != nil {
		return err
	}
	msg := "this account is banned"
	if until != nil {
		msg += " until " + until.UTC().Format("2006-01-02 15:04 UTC")
	}
	if reason != "" {
		msg += ": " + reason
	}
	return httpx.NewAPIError(http.StatusForbidden, httpx.CodeAccountDisabled, msg)
}

// RunBanExpiry lifts bans whose time has come, every interval until ctx ends.
func (h *Handler) RunBanExpiry(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		h.ExpireBans(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// ExpireBans lifts the bans whose time has come.
func (h *Handler) ExpireBans(ctx context.Context) {
	rows, err := h.Sessions.DB.Query(ctx, `UPDATE users SET banned_at = NULL, banned_until = NULL,
		ban_reason = '', banned_by = NULL, updated_at = NOW()
		WHERE banned_at IS NOT NULL AND banned_until IS NOT NULL AND banned_until <= NOW()
		RETURNING id, username`)
	if err != nil {
		slog.Error("ban expiry failed", "err", err)
		return
	}
	defer rows.Close()
	type expired struct {
		id   uuid.UUID
		name string
	}
	var done []expired
	for rows.Next() {
		var e expired
		if rows.Scan(&e.id, &e.name) == nil {
			done = append(done, e)
		}
	}
	rows.Close()
	for _, e := range done {
		id := e.id
		h.Audit.RecordOrLog(ctx, audit.Entry{
			Action: "moderation.ban_expired", TargetType: "user", TargetID: id.String(), TargetName: e.name,
		})
		if h.Events != nil {
			h.Events.Broadcast("user_update", map[string]any{"id": id, "disabled": false})
		}
	}
}
