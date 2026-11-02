package communityevents

import (
	"github.com/go-chi/chi/v5"
	"net/http"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// loadViewable returns the event of the {id} path parameter, 404 when the
// caller may not view it.
func (h *Handler) loadViewable(r *http.Request) (*CommunityEvent, error) {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return nil, err
	}
	ev, err := h.Store.Get(r.Context(), id, &auth.UserFrom(r.Context()).ID)
	if err != nil {
		return nil, err
	}
	if err := h.requireViewable(r, ev); err != nil {
		return nil, err
	}
	return ev, nil
}

// requireRoster lets through whoever may manage the roster of ev: the member
// who created the event and moderators with Manage events.
func (h *Handler) requireRoster(r *http.Request, ev *CommunityEvent) error {
	user := auth.UserFrom(r.Context())
	if ev.CreatedBy != nil && *ev.CreatedBy == user.ID {
		return nil
	}
	if h.Perms == nil {
		return nil
	}
	snap, err := h.Perms.Snapshot(r.Context())
	if err != nil {
		return err
	}
	if snap.Global(user.Subject()).Has(perm.ManageEvents) {
		return nil
	}
	return httpx.ErrForbidden("only the event's creator and members who can manage events change its roles and sign-ups")
}

// auditRoster records a change of an event's roles or sign-ups as an edit of the event.
func (h *Handler) auditRoster(r *http.Request, ev *CommunityEvent, change string, extra map[string]any) {
	if h.Audit == nil {
		return
	}
	action := "event.edit"
	if ev.ChannelID != nil {
		action = "event.talk_edit"
	}
	details := map[string]any{"title": ev.Title, "channel_id": ev.ChannelID, "roster": change}
	for k, v := range extra {
		details[k] = v
	}
	h.Audit.RecordOrLog(r.Context(), audit.Entry{
		Action: action, TargetType: "event", TargetID: ev.ID.String(), TargetName: ev.Title, ChannelID: ev.ChannelID, Details: details,
	})
}

// finishRoster answers a roster change with the event as it stands, tells the
// clients and the members who moved up.
func (h *Handler) finishRoster(w http.ResponseWriter, r *http.Request, id uuid.UUID, promoted []Promotion) error {
	updated, err := h.Store.Get(r.Context(), id, &auth.UserFrom(r.Context()).ID)
	if err != nil {
		return err
	}
	h.broadcast(updated)
	h.notifyPromoted(r, updated, promoted)
	httpx.WriteJSON(w, http.StatusOK, updated)
	return nil
}

// @Summary Set the roles of an event
// @Description Replaces the event's role list (at most 12; each with a name of up to 40 characters, an optional emoji and a capacity of 1 to 1000, or none for unlimited). A role sent with its id keeps its sign-ups; one that is left out is removed, which is refused with 409 while it has sign-ups (hand them over with DELETE .../roles/{roleID}?move_to=). A capacity below the number of confirmed members is refused with 409 naming how many to remove; a larger capacity moves the waitlist up. Allowed for the event's creator and for members with Manage events; not on a completed or cancelled event.
// @ID setCommunityEventRoles
// @Tags Events
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "Event ID" Format(uuid)
// @Param body body SetRolesRequest true "The new role list, in order"
// @Success 200 {object} CommunityEvent
// @Failure 400 {object} httpx.ErrorResponse "INVALID_INPUT"
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN"
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND"
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the event is over, a role to remove has sign-ups, a capacity is below the confirmed members, or two roles share a name."
// @Router /api/events/{id}/roles [put]
func (h *Handler) setRoles(w http.ResponseWriter, r *http.Request) error {
	ev, err := h.loadViewable(r)
	if err != nil {
		return err
	}
	if err := h.requireRoster(r, ev); err != nil {
		return err
	}
	var req SetRolesRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	promoted, err := h.Store.SetRoles(r.Context(), ev.ID, req.Roles)
	if err != nil {
		return err
	}
	h.auditRoster(r, ev, "roles_set", map[string]any{"roles": len(req.Roles)})
	return h.finishRoster(w, r, ev.ID, promoted)
}

// @Summary Delete a role of an event
// @Description Removes a role. A role with sign-ups is refused with 409 unless move_to names another role of the event, which takes the sign-ups over (members who held a slot keep their place in line). Allowed for the event's creator and for members with Manage events; not on a completed or cancelled event.
// @ID deleteCommunityEventRole
// @Tags Events
// @Produce json
// @Security cookieAuth
// @Param id path string true "Event ID" Format(uuid)
// @Param roleID path string true "Role ID" Format(uuid)
// @Param move_to query string false "Another role of the event that takes the sign-ups over." Format(uuid)
// @Success 200 {object} CommunityEvent
// @Failure 400 {object} httpx.ErrorResponse "INVALID_INPUT"
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN"
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND"
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the event is over or the role has sign-ups and no move_to."
// @Router /api/events/{id}/roles/{roleID} [delete]
func (h *Handler) deleteRole(w http.ResponseWriter, r *http.Request) error {
	ev, err := h.loadViewable(r)
	if err != nil {
		return err
	}
	if err := h.requireRoster(r, ev); err != nil {
		return err
	}
	roleID, err := httpx.PathUUID(r, "roleID")
	if err != nil {
		return err
	}
	var moveTo *uuid.UUID
	if raw := r.URL.Query().Get("move_to"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return httpx.ErrInvalidInput("move_to must be a UUID")
		}
		moveTo = &id
	}
	promoted, err := h.Store.DeleteRole(r.Context(), ev.ID, roleID, moveTo)
	if err != nil {
		return err
	}
	h.auditRoster(r, ev, "role_deleted", map[string]any{"role_id": roleID, "moved_to": moveTo})
	return h.finishRoster(w, r, ev.ID, promoted)
}

func (h *Handler) mountRoles(r chi.Router) {
	r.Put("/events/{id}/roles", httpx.Handle(h.setRoles))
	r.Delete("/events/{id}/roles/{roleID}", httpx.Handle(h.deleteRole))
	r.Put("/events/{id}/signup", httpx.Handle(h.signUp))
	r.Delete("/events/{id}/signup", httpx.Handle(h.leaveSignup))
	r.Delete("/events/{id}/signup/{userID}", httpx.Handle(h.removeSignup))
	r.Get("/event-role-templates", httpx.Handle(h.listTemplates))
	r.Post("/event-role-templates", httpx.Handle(h.createTemplate))
	r.Patch("/event-role-templates/{templateID}", httpx.Handle(h.updateTemplate))
	r.Delete("/event-role-templates/{templateID}", httpx.Handle(h.deleteTemplate))
}
