// Package roles serves the role and permission API: who holds which roles,
// the roles themselves and the per-channel overwrites. The model and its
// rules live in package perm; this is the HTTP surface.
package roles

import (
	"bytes"
	"net/http"
	"slices"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Handler serves /api/permissions, /api/roles and the role administration
// under /api/admin.
type Handler struct {
	Perms  *perm.Service
	Events events.Publisher
}

// Mount registers the routes every member may call (RequireUser applies).
func (h *Handler) Mount(r chi.Router) {
	r.Get("/permissions", httpx.Handle(h.myPermissions))
	r.Get("/roles", httpx.Handle(h.listRoles))
}

// MountAdmin registers role administration; each route checks its own
// permission (RequireUser applies).
func (h *Handler) MountAdmin(r chi.Router) {
	roles := r.With(auth.RequirePerm(h.Perms, perm.ManageRoles))
	roles.Post("/roles", httpx.Handle(h.createRole))
	roles.Put("/roles/order", httpx.Handle(h.reorderRoles))
	roles.Patch("/roles/{id}", httpx.Handle(h.updateRole))
	roles.Delete("/roles/{id}", httpx.Handle(h.deleteRole))
	roles.Put("/users/{id}/roles", httpx.Handle(h.setMemberRoles))

	chans := r.With(auth.RequirePerm(h.Perms, perm.ManageChannels))
	chans.Get("/channels/{id}/overwrites", httpx.Handle(h.getChannelOverwrites))
	chans.Put("/channels/{id}/overwrites", httpx.Handle(h.setChannelOverwrites))
	chans.Get("/categories/{id}/overwrites", httpx.Handle(h.getCategoryOverwrites))
	chans.Put("/categories/{id}/overwrites", httpx.Handle(h.setCategoryOverwrites))
}

// ChannelPermissions is what the caller may do in one channel.
type ChannelPermissions struct {
	ChannelID   uuid.UUID `json:"channel_id" format:"uuid"`
	Permissions perm.Set  `json:"permissions" swaggertype:"integer" minimum:"0"`
}

// PermissionsInfo is what the caller may do.
type PermissionsInfo struct {
	// Owner is true for the Owner, who may do everything and cannot be
	// restricted.
	Owner bool `json:"owner"`
	// Permissions are the server-wide permissions (bits, see catalog).
	Permissions perm.Set `json:"permissions" swaggertype:"integer" minimum:"0"`
	// RoleIDs are the caller's custom roles, highest first.
	RoleIDs []uuid.UUID `json:"role_ids" format:"uuid"`
	// TopPosition is the position of the caller's highest role (0 without
	// one); the Owner is above every position. A member can only manage roles
	// and members below it.
	TopPosition int `json:"top_position" minimum:"0"`
	// Channels lists every channel the caller may view with what they may do
	// there (bits).
	Channels []ChannelPermissions `json:"channels"`
	// Catalog lists every permission bit.
	Catalog []perm.Info `json:"catalog"`
}

// myPermissions handles GET /api/permissions.
//
// @Summary Own permissions
// @Description The caller's server-wide permissions and, per viewable channel, what they may do there. Refetch after a permissions_changed event.
// @ID getPermissions
// @Tags Roles
// @Produce json
// @Security cookieAuth
// @Success 200 {object} PermissionsInfo "Permissions."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/permissions [get]
func (h *Handler) myPermissions(w http.ResponseWriter, r *http.Request) error {
	user := auth.UserFrom(r.Context())
	snap, err := h.Perms.Snapshot(r.Context())
	if err != nil {
		return err
	}
	sub := user.Subject()
	channels := []ChannelPermissions{}
	for id, p := range snap.ChannelPermissions(sub) {
		channels = append(channels, ChannelPermissions{ChannelID: id, Permissions: p})
	}
	slices.SortFunc(channels, func(a, b ChannelPermissions) int { return bytes.Compare(a.ChannelID[:], b.ChannelID[:]) })
	httpx.WriteJSON(w, http.StatusOK, PermissionsInfo{
		Owner:       snap.IsOwner(sub),
		Permissions: snap.Global(sub),
		RoleIDs:     nonNil(snap.RoleIDs(user.ID)),
		TopPosition: min(snap.TopPosition(sub), maxReportedPosition),
		Channels:    channels,
		Catalog:     perm.Catalog,
	})
	return nil
}

// maxReportedPosition is what the Owner's unlimited position is reported as.
const maxReportedPosition = 1 << 20

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// listRoles handles GET /api/roles.
//
// @Summary List roles
// @Description Every role, highest first; @everyone (is_everyone) is last. Roles are visible to all members, they colour names.
// @ID listRoles
// @Tags Roles
// @Produce json
// @Security cookieAuth
// @Success 200 {array} perm.Role "Roles."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/roles [get]
func (h *Handler) listRoles(w http.ResponseWriter, r *http.Request) error {
	roles, err := h.Perms.Roles(r.Context())
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, roles)
	return nil
}

func (h *Handler) rolesChanged() { h.Events.Broadcast("roles_changed", nil) }

// createRole handles POST /api/admin/roles.
//
// @Summary Create a role
// @Description The new role sits at the bottom of the hierarchy, just above @everyone. Needs the Manage roles permission and the caller cannot grant permissions they lack. Broadcasts roles_changed.
// @ID createRole
// @Tags Roles
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body perm.RoleInput true "Request body."
// @Success 201 {object} perm.Role "Created role."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing permission, or granting a permission the caller lacks."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: too many roles."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/roles [post]
func (h *Handler) createRole(w http.ResponseWriter, r *http.Request) error {
	var in perm.RoleInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	role, err := h.Perms.CreateRole(r.Context(), auth.UserFrom(r.Context()).Subject(), in)
	if err != nil {
		return err
	}
	h.rolesChanged()
	httpx.WriteJSON(w, http.StatusCreated, role)
	return nil
}

// updateRole handles PATCH /api/admin/roles/{id}.
//
// @Summary Edit a role
// @Description Omitted fields are kept. Only roles below the caller's highest role can be edited, and only permissions the caller holds can be added or removed. @everyone only has its permissions editable. Members who hold the role are told to refresh (permissions_changed) and removed from Talks they may no longer join. Broadcasts roles_changed.
// @ID updateRole
// @Tags Roles
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "Role ID." Format(uuid)
// @Param request body perm.RoleInput true "Request body."
// @Success 200 {object} perm.Role "Updated role."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing permission, role not below the caller's highest role, or granting a permission the caller lacks."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/roles/{id} [patch]
func (h *Handler) updateRole(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	var in perm.RoleInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	role, err := h.Perms.UpdateRole(r.Context(), auth.UserFrom(r.Context()).Subject(), id, in)
	if err != nil {
		return err
	}
	h.rolesChanged()
	httpx.WriteJSON(w, http.StatusOK, role)
	return nil
}

// deleteRole handles DELETE /api/admin/roles/{id}.
//
// @Summary Delete a role
// @Description Removes it from everyone who has it and from all overwrites. Only roles below the caller's highest role. @everyone cannot be deleted. Broadcasts roles_changed.
// @ID deleteRole
// @Tags Roles
// @Produce json
// @Security cookieAuth
// @Param id path string true "Role ID." Format(uuid)
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing permission or role not below the caller's highest role."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/roles/{id} [delete]
func (h *Handler) deleteRole(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	if err := h.Perms.DeleteRole(r.Context(), auth.UserFrom(r.Context()).Subject(), id); err != nil {
		return err
	}
	h.rolesChanged()
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// ReorderRequest is the body of PUT /api/admin/roles/order.
type ReorderRequest struct {
	// RoleIDs lists every custom role (not @everyone) exactly once, highest
	// first.
	RoleIDs []uuid.UUID `json:"role_ids" format:"uuid"`
}

// reorderRoles handles PUT /api/admin/roles/order.
//
// @Summary Reorder roles
// @Description Sets the hierarchy. The caller can only move roles below their own highest role, to places below it. Broadcasts roles_changed.
// @ID reorderRoles
// @Tags Roles
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body ReorderRequest true "Request body."
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing permission, or moving roles at or above the caller's highest role."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/roles/order [put]
func (h *Handler) reorderRoles(w http.ResponseWriter, r *http.Request) error {
	var req ReorderRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if err := h.Perms.ReorderRoles(r.Context(), auth.UserFrom(r.Context()).Subject(), req.RoleIDs); err != nil {
		return err
	}
	h.rolesChanged()
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// MemberRolesRequest is the body of PUT /api/admin/users/{id}/roles.
type MemberRolesRequest struct {
	// RoleIDs are all custom roles the member should have.
	RoleIDs []uuid.UUID `json:"role_ids" format:"uuid"`
}

// setMemberRoles handles PUT /api/admin/users/{id}/roles.
//
// @Summary Set a member's roles
// @Description Replaces the member's custom roles. Only roles below the caller's highest role can be added or removed, and only on members whose highest role is below it. The Owner's roles cannot be changed (403). The member is told to refresh and removed from Talks they may no longer join. Broadcasts roles_changed.
// @ID setMemberRoles
// @Tags Roles
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "User ID." Format(uuid)
// @Param request body MemberRolesRequest true "Request body."
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing permission, the target is the Owner or not below the caller, or a role is not below the caller's highest role."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/users/{id}/roles [put]
func (h *Handler) setMemberRoles(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	var req MemberRolesRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if err := h.Perms.SetMemberRoles(r.Context(), auth.UserFrom(r.Context()).Subject(), id, req.RoleIDs); err != nil {
		return err
	}
	h.rolesChanged()
	w.WriteHeader(http.StatusNoContent)
	return nil
}

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
	h.Events.Broadcast("channels_changed", nil)
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
