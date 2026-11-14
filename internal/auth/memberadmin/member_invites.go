package memberadmin

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// mountMemberInvites registers the invite routes of a member. They need a
// session only: Create invites is checked per request, in the channel the
// invite lands in.
func (h *API) mountMemberInvites(r chi.Router) {
	r.Get("/me/invites", httpx.Handle(h.listOwnInvites))
	r.Post("/me/invites", httpx.Handle(h.createMemberInvite))
	r.Delete("/me/invites/{id}", httpx.Handle(h.deleteOwnInvite))
}

// listOwnInvites handles GET /api/me/invites.
//
// @Summary List my invites
// @Description The invites the signed-in member created, newest first, with the limits new ones must stay within. Needs a session only, so a member whose right was withdrawn still sees (and can revoke) what they made.
// @ID listOwnInvites
// @Tags Invites
// @Produce json
// @Security cookieAuth
// @Success 200 {object} auth.MemberInviteList "The member's invites and the limits."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/me/invites [get]
func (h *API) listOwnInvites(w http.ResponseWriter, r *http.Request) error {
	u := auth.UserFrom(r.Context())
	list, err := auth.ListOwnInvites(r.Context(), h.Sessions.DB, u.ID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, list)
	return nil
}

// createMemberInvite handles POST /api/me/invites.
//
// @Summary Create an invite as a member
// @Description Needs the Create invites permission, in the channel when one is named (403 otherwise; a channel the member cannot see answers 404). The invite always expires and has a use limit, both within the bounds the holders of Manage invites set; omitted fields take safe defaults (24 hours, 10 uses). The number of active invites and of invites per hour is limited per member. A lockdown that pauses invites refuses it (409).
// @ID createMemberInvite
// @Tags Invites
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body auth.CreateMemberInviteRequest true "Request body."
// @Success 201 {object} auth.Invite "Created invite."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, or a value above the limits."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Create invites permission, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the channel does not exist or the member cannot see it."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the member has too many active invites, or invites are paused."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests, or too many invites created this hour."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/me/invites [post]
func (h *API) createMemberInvite(w http.ResponseWriter, r *http.Request) error {
	var req auth.CreateMemberInviteRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	u := auth.UserFrom(r.Context())
	global, err := h.memberInviteRights(r, u, req.ChannelID)
	if err != nil {
		return err
	}
	unlimited := global.HasAny(perm.Administrator | perm.CreateInvites)
	inv, err := auth.CreateMemberInvite(r.Context(), h.Sessions.DB, u.ID, req, unlimited, h.memberInviteAudit(r, true))
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, inv)
	return nil
}

// deleteOwnInvite handles DELETE /api/me/invites/{id}.
//
// @Summary Revoke my invite
// @Description Revokes an invite the signed-in member created; accounts that registered with it stay. Another member's invite answers 404. Needs no permission beyond a session, so a member whose right was withdrawn can still revoke what they made.
// @ID deleteOwnInvite
// @Tags Invites
// @Produce json
// @Security cookieAuth
// @Param id path string true "Resource ID." Format(uuid)
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the invite does not exist or is not the member's."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/me/invites/{id} [delete]
func (h *API) deleteOwnInvite(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	u := auth.UserFrom(r.Context())
	if err := auth.DeleteOwnInvite(r.Context(), h.Sessions.DB, u.ID, id, h.memberInviteAudit(r, false)); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// memberInviteRights checks that u may create invites (in channel, if one is
// named) and returns what u holds server-wide. Without a channel the
// server-wide permission counts; with one, the permission as the channel's
// overwrites leave it, and the channel must be a community channel the member
// can see: a hidden one looks like one that does not exist.
func (h *API) memberInviteRights(r *http.Request, u *auth.User, channel *uuid.UUID) (perm.Set, error) {
	ctx := r.Context()
	sub := u.Subject()
	snap, err := h.Perms.Snapshot(ctx)
	if err != nil {
		return 0, err
	}
	global := snap.Global(sub)
	if channel == nil {
		return global, snap.Deny(sub, global, perm.CreateMemberInvites)
	}
	if snap, err = h.Perms.ForChannel(ctx, *channel); err != nil {
		return 0, err
	}
	if !snap.KnowsChannel(*channel) || snap.KnowsDM(*channel) || snap.IsDMTalk(*channel) {
		return 0, perm.ErrChannelHidden
	}
	p := snap.Channel(sub, *channel)
	if !p.Has(perm.ViewChannel) {
		return 0, perm.ErrChannelHidden
	}
	return global, snap.Deny(sub, p, perm.CreateMemberInvites)
}

// memberInviteAudit writes the entry of a member's invite action in the
// transaction of the change. The code is not recorded.
func (h *API) memberInviteAudit(r *http.Request, created bool) auth.MemberInviteAudit {
	return func(tx pgx.Tx, inv *auth.Invite) error {
		action := "invite.member_revoke"
		if created {
			action = "invite.member_create"
		}
		details := map[string]any{"max_uses": inv.MaxUses, "uses_count": inv.UsesCount, "expires_at": inv.ExpiresAt}
		if inv.ChannelID != nil {
			details["channel_id"] = inv.ChannelID.String()
		}
		return h.Audit.RecordTx(r.Context(), tx, audit.Entry{Action: action, TargetType: "invite", TargetID: inv.ID.String(), Details: details})
	}
}

// memberInviteLimitsRoutes registers the bounds the admins set for them.
func (h *API) memberInviteLimitsRoutes(r chi.Router) {
	r.Get("/invites/limits", httpx.Handle(h.getMemberInviteLimits))
	r.Put("/invites/limits", httpx.Handle(h.putMemberInviteLimits))
}

// getMemberInviteLimits handles GET /api/admin/invites/limits.
//
// @Summary Read the limits of member invites
// @Description Requires the Manage invites permission (403 otherwise).
// @ID getMemberInviteLimits
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Success 200 {object} auth.MemberInviteLimits "The limits."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage invites permission."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/invites/limits [get]
func (h *API) getMemberInviteLimits(w http.ResponseWriter, r *http.Request) error {
	l, err := auth.LoadMemberInviteLimits(r.Context(), h.Sessions.DB)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, l)
	return nil
}

// putMemberInviteLimits handles PUT /api/admin/invites/limits.
//
// @Summary Set the limits of member invites
// @Description Requires the Manage invites permission (403 otherwise). Replaces all four limits. They apply to invites created from now on; invites that exist keep what they have.
// @ID putMemberInviteLimits
// @Tags Admin
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body auth.MemberInviteLimits true "Request body."
// @Success 200 {object} auth.MemberInviteLimits "The limits now in force."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields or a value out of range."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage invites permission, or cross-origin request rejected."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/invites/limits [put]
func (h *API) putMemberInviteLimits(w http.ResponseWriter, r *http.Request) error {
	var l auth.MemberInviteLimits
	if err := httpx.DecodeJSON(r, &l); err != nil {
		return err
	}
	if err := auth.SaveMemberInviteLimits(r.Context(), h.Sessions.DB, l); err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, l)
	return nil
}
