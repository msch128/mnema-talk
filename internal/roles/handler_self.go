package roles

import (
	"net/http"

	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// listSelfAssignable handles GET /api/roles/self-assignable.
//
// @Summary List the roles members can choose
// @Description The roles an admin opened to self-assignment, highest first: ordinary roles without Administrator or admin-area permissions. Any member may read it; PUT and DELETE /api/me/roles/{id} take and drop them.
// @ID listSelfAssignableRoles
// @Tags Roles
// @Produce json
// @Security cookieAuth
// @Success 200 {array} perm.Role "Roles members may choose for themselves."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/roles/self-assignable [get]
func (h *Handler) listSelfAssignable(w http.ResponseWriter, r *http.Request) error {
	open, err := h.Perms.SelfAssignableRoles(r.Context())
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, open)
	return nil
}

// takeSelfRole handles PUT /api/me/roles/{id}.
//
// @Summary Take a self-assignable role
// @Description Adds the role to the caller's own account. Only roles listed by GET /api/roles/self-assignable; asking for a role the caller already holds changes nothing. Recorded in the audit log as role.self_assign. Broadcasts member_roles_changed (the member's id and roles) and tells the caller to refresh (permissions_changed).
// @ID takeSelfRole
// @Tags Roles
// @Security cookieAuth
// @Param id path string true "Role ID." Format(uuid)
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: the role is not open to self-assignment, or the caller is the Owner, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/me/roles/{id} [put]
func (h *Handler) takeSelfRole(w http.ResponseWriter, r *http.Request) error {
	return h.setSelfRole(w, r, true)
}

// dropSelfRole handles DELETE /api/me/roles/{id}.
//
// @Summary Drop a self-assignable role
// @Description Removes the role from the caller's own account. Only roles listed by GET /api/roles/self-assignable; dropping a role the caller does not hold changes nothing. Recorded in the audit log as role.self_remove. Broadcasts member_roles_changed (the member's id and roles) and tells the caller to refresh (permissions_changed).
// @ID dropSelfRole
// @Tags Roles
// @Security cookieAuth
// @Param id path string true "Role ID." Format(uuid)
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: the role is not open to self-assignment, or the caller is the Owner, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/me/roles/{id} [delete]
func (h *Handler) dropSelfRole(w http.ResponseWriter, r *http.Request) error {
	return h.setSelfRole(w, r, false)
}

func (h *Handler) setSelfRole(w http.ResponseWriter, r *http.Request, on bool) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	user := auth.UserFrom(r.Context())
	role, changed, held, err := h.Perms.SetSelfRole(r.Context(), user.ID, id, on)
	if err != nil {
		return err
	}
	if changed {
		action := "role.self_remove"
		if on {
			action = "role.self_assign"
		}
		h.Audit.RecordOrLog(r.Context(), audit.Entry{Action: action, TargetType: "role", TargetID: role.ID.String(), TargetName: role.Name})
		h.Events.Broadcast(events.MemberRolesChanged, map[string]any{"id": user.ID, "role_ids": held})
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
