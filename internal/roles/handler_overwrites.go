package roles

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// OverwritesBody is the body and response of the overwrite routes.
type OverwritesBody struct {
	Overwrites []perm.Overwrite `json:"overwrites"`
}

// getChannelOverwrites handles GET /api/admin/channels/{id}/overwrites.
//
// @Summary Permission overwrites of a channel
// @Description The allow/deny pairs set directly on the channel. A channel also inherits its category's (its own win). Needs the Manage channels permission and view access.
// @ID getChannelOverwrites
// @Tags Roles
// @Produce json
// @Security cookieAuth
// @Param id path string true "Channel ID." Format(uuid)
// @Success 200 {object} OverwritesBody "Overwrites."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing permission."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/channels/{id}/overwrites [get]
func (h *Handler) getChannelOverwrites(w http.ResponseWriter, r *http.Request) error {
	return h.listOverwrites(w, r, perm.TargetChannel)
}

// setChannelOverwrites handles PUT /api/admin/channels/{id}/overwrites.
//
// @Summary Replace the permission overwrites of a channel
// @Description Replaces all overwrites of the channel. Only channel permissions can be overwritten; the caller can only allow what they hold themselves; the Owner cannot be the subject of one. Everyone affected is told to refresh and removed from Talks they may no longer join.
// @ID setChannelOverwrites
// @Tags Roles
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "Channel ID." Format(uuid)
// @Param request body OverwritesBody true "Request body."
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing permission, the subject is the Owner, or allowing a permission the caller lacks."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/channels/{id}/overwrites [put]
func (h *Handler) setChannelOverwrites(w http.ResponseWriter, r *http.Request) error {
	return h.replaceOverwrites(w, r, perm.TargetChannel)
}

// getCategoryOverwrites handles GET /api/admin/categories/{id}/overwrites.
//
// @Summary Permission overwrites of a category
// @Description The allow/deny pairs set directly on the category. Its channels inherit them. Needs the Manage channels permission and view access.
// @ID getCategoryOverwrites
// @Tags Roles
// @Produce json
// @Security cookieAuth
// @Param id path string true "Category ID." Format(uuid)
// @Success 200 {object} OverwritesBody "Overwrites."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing permission."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/categories/{id}/overwrites [get]
func (h *Handler) getCategoryOverwrites(w http.ResponseWriter, r *http.Request) error {
	return h.listOverwrites(w, r, perm.TargetCategory)
}

// setCategoryOverwrites handles PUT /api/admin/categories/{id}/overwrites.
//
// @Summary Replace the permission overwrites of a category
// @Description Replaces all overwrites of the category. Only channel permissions can be overwritten; the caller can only allow what they hold themselves; the Owner cannot be the subject of one. Everyone affected is told to refresh and removed from Talks they may no longer join.
// @ID setCategoryOverwrites
// @Tags Roles
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "Category ID." Format(uuid)
// @Param request body OverwritesBody true "Request body."
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing permission, the subject is the Owner, or allowing a permission the caller lacks."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/categories/{id}/overwrites [put]
func (h *Handler) setCategoryOverwrites(w http.ResponseWriter, r *http.Request) error {
	return h.replaceOverwrites(w, r, perm.TargetCategory)
}

// listOverwrites lists the overwrites of a channel or category.
func (h *Handler) listOverwrites(w http.ResponseWriter, r *http.Request, kind string) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	if err := h.canSee(r, kind, id); err != nil {
		return err
	}
	list, err := h.Perms.Overwrites(r.Context(), kind, id)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, OverwritesBody{Overwrites: nonNil(list)})
	return nil
}

// replaceOverwrites replaces the overwrites of a channel or category.
func (h *Handler) replaceOverwrites(w http.ResponseWriter, r *http.Request, kind string) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	var body OverwritesBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		return err
	}
	if err := h.Perms.SetOverwrites(r.Context(), auth.UserFrom(r.Context()).Subject(), kind, id, body.Overwrites); err != nil {
		return err
	}
	h.Events.Broadcast(events.ChannelsChanged, nil)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// canSee hides channels and categories the caller cannot view.
func (h *Handler) canSee(r *http.Request, kind string, id uuid.UUID) error {
	sub := auth.UserFrom(r.Context()).Subject()
	if kind == perm.TargetChannel {
		return h.Perms.RequireChannel(r.Context(), sub, id, perm.ViewChannel)
	}
	snap, err := h.Perms.Snapshot(r.Context())
	if err != nil {
		return err
	}
	if snap.KnowsCategory(id) && !snap.CategoryVisible(sub, id) {
		return httpx.ErrNotFound("category not found")
	}
	return nil
}
