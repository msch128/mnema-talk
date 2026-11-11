package memberadmin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/events/wire"
	"github.com/msch128/mnema-talk/internal/safego"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/worker"
)

func parseBanDuration(durationSeconds *int) (*time.Time, error) {
	if durationSeconds == nil {
		return nil, nil
	}
	if *durationSeconds < MinBanSeconds || *durationSeconds > MaxBanSeconds {
		return nil, httpx.ErrInvalidInput(fmt.Sprintf("duration_seconds must be between %d and %d", MinBanSeconds, MaxBanSeconds))
	}
	t := time.Now().Add(time.Duration(*durationSeconds) * time.Second)
	return &t, nil
}

func (h *API) postBanCleanup(id uuid.UUID, until *time.Time, reason string) {
	// Tell them first; the connection closes a moment later so the notice
	// is written. Their sessions are already dead, but an open socket could
	// still send frames (a voice join would re-seat them), so it stops acting
	// before the notice goes out.
	if h.Live != nil {
		h.Live.EndingUser(id)
	}
	h.notify(id, wire.ModerationNotice{Kind: "ban", Until: until, Reason: reason})
	if h.OnAccountEnded != nil {
		h.OnAccountEnded(id)
	}
	if h.Live != nil {
		h.Live.KickFromVoice(id)
		safego.AfterFunc("ban_disconnect", banDisconnectDelay, safego.Crash, func() { h.Live.DisconnectUser(id) })
	}
	h.Events.Broadcast(wire.UserDisabled{ID: id, Disabled: true})
}

// banUser handles POST /api/admin/users/{id}/ban.
//
// @Summary Ban a member
// @Description Ends the member's sessions and connections and blocks signing in, with an optional reason and an optional duration (otherwise for good). The account and its messages stay. By default the invite the account registered with is revoked, so that code cannot create the account again; that only happens when the caller also holds Manage invites (or is the Owner), otherwise the ban goes through and the invite stays. Needs Ban members and a higher role than the member; the Owner cannot be banned. Kicking someone without a record is Disable.
// @ID banUser
// @Tags Admin
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "User ID." Format(uuid)
// @Param request body auth.BanRequest true "Request body."
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Ban members permission, the member's highest role is not below yours, or the member is the Owner, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/users/{id}/ban [post]
func (h *API) banUser(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	var req auth.BanRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if err := h.BanMember(r, id, req); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// BanMember bans a member. The REST route and the /ban slash command both run
// it; the caller has checked the Ban members permission (RequirePermission).
func (h *API) BanMember(r *http.Request, id uuid.UUID, req auth.BanRequest) error {
	reason, err := cleanReason(req.Reason)
	if err != nil {
		return err
	}
	until, err := parseBanDuration(req.DurationSeconds)
	if err != nil {
		return err
	}
	actor := auth.UserFrom(r.Context())
	revoke := req.RevokeInvite == nil || *req.RevokeInvite
	if revoke {
		// Deleting an invite is Manage invites territory: a moderator who
		// may only ban cannot use a ban to destroy a shared code.
		allowed, err := h.Perms.Global(r.Context(), actor.Subject())
		if err != nil {
			return err
		}
		revoke = allowed.HasAny(perm.CreateInvites)
	}
	err = h.moderationTarget(r, id, func(q db.Querier) error {
		return setBan(r.Context(), q, id, actor.ID, reason, until, revoke)
	})
	if err != nil {
		return err
	}
	h.postBanCleanup(id, until, reason)
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
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Ban members permission, or the member's highest role is not below yours, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the member does not exist or is not banned."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/users/{id}/ban [delete]
func (h *API) unbanUser(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	var disabled bool
	err = h.moderationTarget(r, id, func(q db.Querier) error {
		err := q.QueryRow(r.Context(), clearBanSQL+` AND banned_at IS NOT NULL RETURNING disabled_at IS NOT NULL`, id).Scan(&disabled)
		if errors.Is(err, pgx.ErrNoRows) {
			return errNotBanned
		}
		return err
	})
	if err != nil {
		return err
	}
	h.Events.Broadcast(wire.UserDisabled{ID: id, Disabled: disabled})
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// clearBanColumns resets every ban column. Bans never touch disabled_at (see
// setBan), so lifting one does not re-enable an account an admin disabled.
const clearBanColumns = `banned_at = NULL, banned_until = NULL, ban_reason = '', banned_by = NULL, updated_at = NOW()`

// clearBanSQL lifts one member's ban.
const clearBanSQL = `UPDATE users SET ` + clearBanColumns + ` WHERE id = $1`

// listBans handles GET /api/admin/bans.
//
// @Summary List bans
// @Description Active bans, newest first. Needs Ban members.
// @ID listBans
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Success 200 {array} auth.Ban "Bans."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Ban members permission."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/bans [get]
func (h *API) listBans(w http.ResponseWriter, r *http.Request) error {
	rows, err := h.Sessions.DB.Query(r.Context(), `
		SELECT u.id, u.username, u.display_name, u.ban_reason, u.banned_at, u.banned_until, COALESCE(b.username, '')
		FROM users u LEFT JOIN users b ON b.id = u.banned_by
		WHERE u.banned_at IS NOT NULL ORDER BY u.banned_at DESC`)
	if err != nil {
		return err
	}
	bans, err := pgx.CollectRows(rows, pgx.RowToStructByPos[auth.Ban])
	if err != nil {
		return err
	}
	if bans == nil {
		bans = []auth.Ban{}
	}
	httpx.WriteJSON(w, http.StatusOK, bans)
	return nil
}

// RunBanExpiry lifts bans whose time has come, every interval until ctx ends.
func (h *API) RunBanExpiry(ctx context.Context, interval time.Duration) {
	if h.Sessions == nil || h.Sessions.DB == nil {
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		worker.Tick(worker.JobBanExpiry, func() error {
			return worker.IgnoreDone(ctx, h.ExpireBans(ctx))
		})
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// ExpireBans lifts the bans whose time has come. It returns the error it
// logged; a row that cannot be read does not drop the ones that can.
func (h *API) ExpireBans(ctx context.Context) error {
	rows, err := h.Sessions.DB.Query(ctx, `UPDATE users SET `+clearBanColumns+`
		WHERE banned_at IS NOT NULL AND banned_until IS NOT NULL AND banned_until <= NOW()
		RETURNING id, username, disabled_at IS NOT NULL`)
	if err != nil {
		slog.Error("ban expiry failed", "err", err)
		return err
	}
	type expired struct {
		ID       uuid.UUID
		Name     string
		Disabled bool
	}
	// The UPDATE is already committed: audit and announce every row that can
	// be read, and say what could not be, instead of losing the lifted bans.
	var done []expired
	var failed error
	for rows.Next() {
		var e expired
		if err := rows.Scan(&e.ID, &e.Name, &e.Disabled); err != nil {
			slog.Error("ban expiry: read lifted ban", "err", err)
			failed = err
			continue
		}
		done = append(done, e)
	}
	if err := rows.Err(); err != nil {
		slog.Error("ban expiry: iterate lifted bans", "err", err, "read", len(done))
		failed = err
	}
	rows.Close()
	for _, e := range done {
		id := e.ID
		h.Audit.RecordOrLog(ctx, audit.Entry{
			Action: "moderation.ban_expired", TargetType: "user", TargetID: id.String(), TargetName: e.Name,
		})
		if h.Events != nil {
			h.Events.Broadcast(wire.UserDisabled{ID: id, Disabled: e.Disabled})
		}
	}
	return failed
}
