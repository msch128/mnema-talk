package memberadmin

import (
	"net/http"

	"github.com/msch128/mnema-talk/internal/auth"

	"github.com/go-chi/chi/v5"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// MountAdmin registers invite and account management; each route checks its
// own permission (RequireUser must already apply).
func (h *API) MountAdmin(r chi.Router) {
	invites := r.With(auth.RequirePerm(h.Perms, perm.CreateInvites))
	invites.Get("/invites", httpx.Handle(h.listInvites))
	invites.Post("/invites", httpx.Handle(h.createInvite))
	invites.Patch("/invites/{id}", httpx.Handle(h.updateInvite))
	invites.Delete("/invites/{id}", httpx.Handle(h.deleteInvite))
	h.mountUserAdmin(r)
}

// listInvites handles GET /api/admin/invites.
//
// @Summary List invites
// @Description Requires the Manage invites permission (403 otherwise).
// @ID listInvites
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Success 200 {array} auth.Invite "Invites, newest first."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage invites permission."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/invites [get]
func (h *API) listInvites(w http.ResponseWriter, r *http.Request) error {
	invites, err := auth.ListInvites(r.Context(), h.Sessions.DB)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, invites)
	return nil
}

// createInvite handles POST /api/admin/invites.
//
// @Summary Create an invite
// @Description Requires the Manage invites permission (403 otherwise).
// @ID createInvite
// @Tags Admin
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body auth.CreateInviteRequest true "Request body."
// @Success 201 {object} auth.Invite "Created invite."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage invites permission, or cross-origin request rejected."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the resource already exists."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/invites [post]
func (h *API) createInvite(w http.ResponseWriter, r *http.Request) error {
	var req auth.CreateInviteRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	inv, err := auth.CreateInvite(r.Context(), h.Sessions.DB, auth.UserFrom(r.Context()).ID, req.Code, req.MaxUses, req.ExpiresInHours)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, inv)
	return nil
}

// updateInvite handles PATCH /api/admin/invites/{id}.
//
// @Summary Change an invite's expiry or use limit
// @Description Requires the Manage invites permission (403 otherwise). Absent fields are kept; null removes the expiry or the limit. A max_uses below the uses already consumed is refused (400).
// @ID updateInvite
// @Tags Admin
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "Resource ID." Format(uuid)
// @Param request body auth.UpdateInviteRequest true "Request body."
// @Success 200 {object} auth.Invite "Updated invite."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage invites permission, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/invites/{id} [patch]
func (h *API) updateInvite(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	var req auth.UpdateInviteRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	inv, err := auth.UpdateInvite(r.Context(), h.Sessions.DB, id, req)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, inv)
	return nil
}

// deleteInvite handles DELETE /api/admin/invites/{id}.
//
// @Summary Delete an invite
// @Description Requires the Manage invites permission (403 otherwise).
// @ID deleteInvite
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Param id path string true "Resource ID." Format(uuid)
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage invites permission, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/invites/{id} [delete]
func (h *API) deleteInvite(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	if err := auth.DeleteInvite(r.Context(), h.Sessions.DB, id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
