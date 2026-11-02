// Package roles serves the role and permission API: who holds which roles,
// the roles themselves and the per-channel overwrites. The model and its
// rules live in package perm; this is the HTTP surface.
package roles

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
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
	// Audit records members taking and dropping self-assignable roles (nil: nothing is recorded).
	Audit *audit.Log
	// SelfRoleLimit throttles those changes per member (nil: unlimited, for tests).
	SelfRoleLimit *httpx.RateLimiter
}

// Mount registers the routes every member may call (RequireUser applies).
func (h *Handler) Mount(r chi.Router) {
	r.Get("/permissions", httpx.Handle(h.myPermissions))
	r.Get("/roles", httpx.Handle(h.listRoles))
	r.Get("/roles/self-assignable", httpx.Handle(h.listSelfAssignable))
	self := r
	if h.SelfRoleLimit != nil {
		self = r.With(h.SelfRoleLimit.By(auth.UserKey))
	}
	self.Put("/me/roles/{id}", httpx.Handle(h.takeSelfRole))
	self.Delete("/me/roles/{id}", httpx.Handle(h.dropSelfRole))
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

	owner := r.With(auth.RequireOwner)
	owner.Get("/upload-limit", httpx.Handle(h.getUploadLimit))
	owner.Put("/upload-limit", httpx.Handle(h.setUploadLimit))

	chans := r.With(auth.RequirePerm(h.Perms, perm.ManageChannels))
	chans.Get("/channels/{id}/overwrites", httpx.Handle(h.getChannelOverwrites))
	chans.Put("/channels/{id}/overwrites", httpx.Handle(h.setChannelOverwrites))
	chans.Get("/categories/{id}/overwrites", httpx.Handle(h.getCategoryOverwrites))
	chans.Put("/categories/{id}/overwrites", httpx.Handle(h.setCategoryOverwrites))
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

func (h *Handler) rolesChanged() { h.Events.Broadcast(events.RolesChanged, nil) }

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
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage roles permission, or granting a permission you lack, or cross-origin request rejected."
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
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage roles permission, the role is not below your highest role, or granting a permission you lack, or cross-origin request rejected."
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
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage roles permission, or the role is not below your highest role, or cross-origin request rejected."
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
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage roles permission, or moving roles at or above your highest role, or cross-origin request rejected."
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
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage roles permission, the member is the Owner or not below you, or a role is not below your highest role, or cross-origin request rejected."
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
