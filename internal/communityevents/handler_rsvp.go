package communityevents

import (
	"net/http"
	"strings"

	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// @Summary Set RSVP response for an event
// @Description Sets caller's response to going, interested, or not_going.
// @ID setCommunityEventRSVP
// @Tags Events
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "Event ID" Format(uuid)
// @Param body body RSVPRequest true "RSVP response"
// @Success 200 {object} CommunityEvent
// @Failure 400 {object} httpx.ErrorResponse "INVALID_INPUT"
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND"
// @Router /api/events/{id}/rsvp [put]
func (h *Handler) setRSVP(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	user := auth.UserFrom(r.Context())
	ev, err := h.Store.Get(r.Context(), id, &user.ID)
	if err != nil {
		return err
	}
	if err := h.requireViewable(r, ev); err != nil {
		return err
	}
	var req RSVPRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	resp := strings.TrimSpace(req.Response)
	if resp == "not going" {
		resp = RSVPNotGoing
	}
	if resp != RSVPGoing && resp != RSVPInterested && resp != RSVPNotGoing {
		return httpx.ErrInvalidInput("response must be going, interested, or not_going")
	}
	if err := h.Store.SetRSVP(r.Context(), id, user.ID, resp); err != nil {
		return err
	}
	updated, err := h.Store.Get(r.Context(), id, &user.ID)
	if err != nil {
		return err
	}
	h.broadcast(updated)
	httpx.WriteJSON(w, http.StatusOK, updated)
	return nil
}

// @Summary Delete RSVP response for an event
// @Description Removes caller's RSVP from the event.
// @ID deleteCommunityEventRSVP
// @Tags Events
// @Produce json
// @Security cookieAuth
// @Param id path string true "Event ID" Format(uuid)
// @Success 200 {object} CommunityEvent
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND"
// @Router /api/events/{id}/rsvp [delete]
func (h *Handler) deleteRSVP(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	user := auth.UserFrom(r.Context())
	ev, err := h.Store.Get(r.Context(), id, &user.ID)
	if err != nil {
		return err
	}
	if err := h.requireViewable(r, ev); err != nil {
		return err
	}
	if err := h.Store.DeleteRSVP(r.Context(), id, user.ID); err != nil {
		return err
	}
	updated, err := h.Store.Get(r.Context(), id, &user.ID)
	if err != nil {
		return err
	}
	h.broadcast(updated)
	httpx.WriteJSON(w, http.StatusOK, updated)
	return nil
}

// @Summary List RSVPs for an event
// @Description Lists members who responded to the event.
// @ID listCommunityEventRSVPs
// @Tags Events
// @Produce json
// @Security cookieAuth
// @Param id path string true "Event ID" Format(uuid)
// @Success 200 {array} RSVPItem
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND"
// @Router /api/events/{id}/rsvps [get]
func (h *Handler) listRSVPs(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	user := auth.UserFrom(r.Context())
	ev, err := h.Store.Get(r.Context(), id, &user.ID)
	if err != nil {
		return err
	}
	if err := h.requireViewable(r, ev); err != nil {
		return err
	}
	rsvps, err := h.Store.ListRSVPs(r.Context(), id)
	if err != nil {
		return err
	}
	if rsvps == nil {
		rsvps = []RSVPItem{}
	}
	httpx.WriteJSON(w, http.StatusOK, rsvps)
	return nil
}
