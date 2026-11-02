package communityevents

import (
	"net/http"

	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// requireTemplateAdmin lets through the Owner and members with Administrator.
func (h *Handler) requireTemplateAdmin(r *http.Request) error {
	if !h.isTemplateAdmin(r) {
		return httpx.ErrForbidden("only administrators manage role templates")
	}
	return nil
}

func (h *Handler) isTemplateAdmin(r *http.Request) bool {
	user := auth.UserFrom(r.Context())
	if h.Perms == nil {
		return user.Role == "admin"
	}
	snap, err := h.Perms.Snapshot(r.Context())
	return err == nil && snap.Global(user.Subject()).Has(perm.Administrator)
}

func (h *Handler) auditTemplate(r *http.Request, action string, t RoleTemplate) {
	if h.Audit == nil {
		return
	}
	h.Audit.RecordOrLog(r.Context(), audit.Entry{
		Action: action, TargetType: "event_role_template", TargetID: t.ID.String(), TargetName: t.Name,
		Details: map[string]any{"name": t.Name, "enabled": t.Enabled, "roles": len(t.Roles)},
	})
}

// @Summary List role templates
// @Description Sets of roles to start an event's role list from. Members see the templates an administrator enabled; administrators see all, including the disabled presets that ship with the server.
// @ID listCommunityEventRoleTemplates
// @Tags Events
// @Produce json
// @Security cookieAuth
// @Success 200 {array} RoleTemplate
// @Router /api/event-role-templates [get]
func (h *Handler) listTemplates(w http.ResponseWriter, r *http.Request) error {
	list, err := h.Store.ListTemplates(r.Context(), h.isTemplateAdmin(r))
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, list)
	return nil
}

// @Summary Create a role template
// @Description Adds a template (enabled unless enabled is false). Administrators only.
// @ID createCommunityEventRoleTemplate
// @Tags Events
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param body body RoleTemplateRequest true "name and roles are required"
// @Success 201 {object} RoleTemplate
// @Failure 400 {object} httpx.ErrorResponse "INVALID_INPUT"
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN"
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the name is taken or there are 50 templates."
// @Router /api/event-role-templates [post]
func (h *Handler) createTemplate(w http.ResponseWriter, r *http.Request) error {
	if err := h.requireTemplateAdmin(r); err != nil {
		return err
	}
	var req RoleTemplateRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if req.Name == nil || req.Roles == nil {
		return httpx.ErrInvalidInput("name and roles are required")
	}
	enabled := req.Enabled == nil || *req.Enabled
	t, err := h.Store.CreateTemplate(r.Context(), *req.Name, *req.Roles, enabled)
	if err != nil {
		return err
	}
	h.auditTemplate(r, "event.role_template_create", t)
	httpx.WriteJSON(w, http.StatusCreated, t)
	return nil
}

// @Summary Update a role template
// @Description Changes the fields sent: enabled, name or roles. A preset can only be enabled or disabled. Administrators only.
// @ID updateCommunityEventRoleTemplate
// @Tags Events
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param templateID path string true "Template ID" Format(uuid)
// @Param body body RoleTemplateRequest true "Fields to change"
// @Success 200 {object} RoleTemplate
// @Failure 400 {object} httpx.ErrorResponse "INVALID_INPUT"
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN"
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND"
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the name is taken, or the change is to a preset's content."
// @Router /api/event-role-templates/{templateID} [patch]
func (h *Handler) updateTemplate(w http.ResponseWriter, r *http.Request) error {
	if err := h.requireTemplateAdmin(r); err != nil {
		return err
	}
	id, err := httpx.PathUUID(r, "templateID")
	if err != nil {
		return err
	}
	var req RoleTemplateRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	t, err := h.Store.UpdateTemplate(r.Context(), id, req)
	if err != nil {
		return err
	}
	h.auditTemplate(r, "event.role_template_update", t)
	httpx.WriteJSON(w, http.StatusOK, t)
	return nil
}

// @Summary Delete a role template
// @Description Removes a template. A preset that ships with the server cannot be deleted, only disabled. Administrators only.
// @ID deleteCommunityEventRoleTemplate
// @Tags Events
// @Security cookieAuth
// @Param templateID path string true "Template ID" Format(uuid)
// @Success 204 "Deleted."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN"
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND"
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the template is a preset."
// @Router /api/event-role-templates/{templateID} [delete]
func (h *Handler) deleteTemplate(w http.ResponseWriter, r *http.Request) error {
	if err := h.requireTemplateAdmin(r); err != nil {
		return err
	}
	id, err := httpx.PathUUID(r, "templateID")
	if err != nil {
		return err
	}
	if err := h.Store.DeleteTemplate(r.Context(), id); err != nil {
		return err
	}
	h.auditTemplate(r, "event.role_template_delete", RoleTemplate{ID: id})
	w.WriteHeader(http.StatusNoContent)
	return nil
}
