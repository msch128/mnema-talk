package communityevents

import (
	"net/http"

	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// @Summary Sign up for a role of an event
// @Description Puts the caller on a role: confirmed while the role has room, otherwise on its waitlist (the answer's my_signup says which, and the place in line). A member who holds another role of the event moves over and the old role's waitlist moves up. The capacity check runs under a lock, so the last slot is never given twice. A sign-up counts as RSVP going. Everyone who can view the event may sign up while it is scheduled or active.
// @ID signUpForCommunityEventRole
// @Tags Events
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "Event ID" Format(uuid)
// @Param body body SignupRequest true "The role"
// @Success 200 {object} CommunityEvent
// @Failure 400 {object} httpx.ErrorResponse "INVALID_INPUT"
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND"
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the event is completed or cancelled."
// @Router /api/events/{id}/signup [put]
func (h *Handler) signUp(w http.ResponseWriter, r *http.Request) error {
	ev, err := h.loadViewable(r)
	if err != nil {
		return err
	}
	var req SignupRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	user := auth.UserFrom(r.Context())
	_, promoted, err := h.Store.SignUp(r.Context(), ev.ID, user.ID, req.RoleID)
	if err != nil {
		return err
	}
	return h.finishRoster(w, r, ev.ID, promoted)
}

// @Summary Leave the role of an event
// @Description Removes the caller's sign-up and moves the role's waitlist up. Always allowed, also on a finished event and for a member who can no longer view the event (a Talk they lost access to): such a member gets 204 instead of the event. The RSVP stays as it is.
// @ID leaveCommunityEventRole
// @Tags Events
// @Produce json
// @Security cookieAuth
// @Param id path string true "Event ID" Format(uuid)
// @Success 200 {object} CommunityEvent
// @Success 204 "Left; the caller can no longer view the event."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND"
// @Router /api/events/{id}/signup [delete]
func (h *Handler) leaveSignup(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	user := auth.UserFrom(r.Context())
	_, promoted, err := h.Store.Leave(r.Context(), id, user.ID)
	if err != nil {
		return err
	}
	ev, err := h.Store.Get(r.Context(), id, &user.ID)
	if err != nil {
		return err
	}
	if h.requireViewable(r, ev) != nil {
		h.broadcast(ev)
		h.notifyPromoted(r, ev, promoted)
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
	return h.finishRoster(w, r, id, promoted)
}

// @Summary Remove a member from the roster of an event
// @Description Removes another member's sign-up and moves the role's waitlist up; the member is told. Allowed for the event's creator and for members with Manage events; not on a completed or cancelled event.
// @ID removeCommunityEventSignup
// @Tags Events
// @Produce json
// @Security cookieAuth
// @Param id path string true "Event ID" Format(uuid)
// @Param userID path string true "Member ID" Format(uuid)
// @Success 200 {object} CommunityEvent
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN"
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND"
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the event is completed or cancelled."
// @Router /api/events/{id}/signup/{userID} [delete]
func (h *Handler) removeSignup(w http.ResponseWriter, r *http.Request) error {
	ev, err := h.loadViewable(r)
	if err != nil {
		return err
	}
	if err := h.requireRoster(r, ev); err != nil {
		return err
	}
	target, err := httpx.PathUUID(r, "userID")
	if err != nil {
		return err
	}
	left, promoted, err := h.Store.RemoveSignup(r.Context(), ev.ID, target)
	if err != nil {
		return err
	}
	if left == nil {
		return httpx.ErrNotFound("that member has no sign-up for this event")
	}
	h.auditRoster(r, ev, "signup_removed", map[string]any{"user_id": target, "role": left.Name})
	if target != auth.UserFrom(r.Context()).ID {
		h.notifyMembers(r.Context(), ev, SignupKindRemoved, left.Name, []Promotion{{EventID: ev.ID, UserID: target, RoleName: left.Name}})
	}
	return h.finishRoster(w, r, ev.ID, promoted)
}
