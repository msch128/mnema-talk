package communityevents

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/media"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/push"
)

// Handler serves the community events API.
type Handler struct {
	DB          *db.Pool
	Store       *Store
	MediaStore  media.Store
	MediaBucket string
	Events      events.Publisher
	Perms       *perm.Service
	Audit       *audit.Log
	Push        *push.Service
}

func (h *Handler) Mount(r chi.Router) {
	r.Get("/events", httpx.Handle(h.listEvents))
	r.Post("/events", httpx.Handle(h.createEvent))
	r.Get("/events/{id}", httpx.Handle(h.getEvent))
	r.Patch("/events/{id}", httpx.Handle(h.updateEvent))
	r.Post("/events/{id}/cancel", httpx.Handle(h.cancelEvent))
	r.Put("/events/{id}/rsvp", httpx.Handle(h.setRSVP))
	r.Delete("/events/{id}/rsvp", httpx.Handle(h.deleteRSVP))
	r.Get("/events/{id}/rsvps", httpx.Handle(h.listRSVPs))
	r.Post("/events/{id}/cover", httpx.Handle(h.uploadCover))
	r.Delete("/events/{id}/cover", httpx.Handle(h.deleteCover))
}

func (h *Handler) canViewChannel(r *http.Request, channelID uuid.UUID) bool {
	if h.Perms == nil {
		return true
	}
	user := auth.UserFrom(r.Context())
	snap, err := h.Perms.Snapshot(r.Context())
	if err != nil {
		return false
	}
	return snap.Channel(user.Subject(), channelID).Has(perm.ViewChannel)
}

func (h *Handler) requireViewable(r *http.Request, ev *CommunityEvent) error {
	if ev.ChannelID != nil && !h.canViewChannel(r, *ev.ChannelID) {
		return httpx.ErrNotFound("event not found")
	}
	return nil
}

func (h *Handler) broadcast(ev *CommunityEvent) {
	if h.Events == nil {
		return
	}
	if ev.ChannelID != nil {
		h.Events.BroadcastChannel(*ev.ChannelID, events.EventUpdate, ev)
	} else {
		h.Events.Broadcast(events.EventUpdate, ev)
	}
}

// @Summary List community events
// @Description Returns community events visible to the caller. Events located in a Talk channel require ViewChannel on that channel.
// @ID listCommunityEvents
// @Tags Events
// @Produce json
// @Security cookieAuth
// @Success 200 {array} CommunityEvent
// @Router /api/events [get]
func (h *Handler) listEvents(w http.ResponseWriter, r *http.Request) error {
	user := auth.UserFrom(r.Context())
	all, err := h.Store.List(r.Context(), &user.ID)
	if err != nil {
		return err
	}
	var snap *perm.Snapshot
	if h.Perms != nil {
		if snap, err = h.Perms.Snapshot(r.Context()); err != nil {
			return err
		}
	}
	visible := []*CommunityEvent{}
	for _, ev := range all {
		if ev.ChannelID != nil && snap != nil && !snap.Channel(user.Subject(), *ev.ChannelID).Has(perm.ViewChannel) {
			continue
		}
		visible = append(visible, ev)
	}
	httpx.WriteJSON(w, http.StatusOK, visible)
	return nil
}

// @Summary Create a community event
// @Description Creates an event. Requires ManageEvents permission. Location must be either a Talk voice channel or free text.
// @ID createCommunityEvent
// @Tags Events
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param body body CreateEventRequest true "Event creation request"
// @Success 201 {object} CommunityEvent
// @Failure 400 {object} httpx.ErrorResponse "INVALID_INPUT"
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN"
// @Router /api/events [post]
func (h *Handler) createEvent(w http.ResponseWriter, r *http.Request) error {
	user := auth.UserFrom(r.Context())
	if err := h.requireManageEvents(r); err != nil {
		return err
	}
	var req CreateEventRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	req.normalize()
	if err := h.validateEventPayload(r, req.Title, req.Description, req.StartTime, req.EndTime, req.ChannelID, req.Location); err != nil {
		return err
	}
	ev, err := h.Store.Create(r.Context(), req, user.ID)
	if err != nil {
		return err
	}
	_ = h.Store.fillRSVPs(r.Context(), ev, &user.ID)
	h.auditRecord(r, "event.create", ev)
	h.broadcast(ev)
	httpx.WriteJSON(w, http.StatusCreated, ev)
	return nil
}

// @Summary Get a community event
// @Description Returns event details with RSVPs. If in a Talk channel, requires ViewChannel.
// @ID getCommunityEvent
// @Tags Events
// @Produce json
// @Security cookieAuth
// @Param id path string true "Event ID" Format(uuid)
// @Success 200 {object} CommunityEvent
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND"
// @Router /api/events/{id} [get]
func (h *Handler) getEvent(w http.ResponseWriter, r *http.Request) error {
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
	httpx.WriteJSON(w, http.StatusOK, ev)
	return nil
}

// @Summary Update a community event
// @Description Updates an event. Requires ManageEvents permission.
// @ID updateCommunityEvent
// @Tags Events
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "Event ID" Format(uuid)
// @Param body body UpdateEventRequest true "Event update request"
// @Success 200 {object} CommunityEvent
// @Failure 400 {object} httpx.ErrorResponse "INVALID_INPUT"
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN"
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND"
// @Router /api/events/{id} [patch]
func (h *Handler) updateEvent(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	if err := h.requireManageEvents(r); err != nil {
		return err
	}
	ev, err := h.Store.Get(r.Context(), id, nil)
	if err != nil {
		return err
	}
	if err := h.requireViewable(r, ev); err != nil {
		return err
	}
	var req UpdateEventRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	req.normalize()
	if req.Status != nil && !validStatus(*req.Status) {
		return httpx.ErrInvalidInput("status must be scheduled, active, completed or cancelled")
	}
	title := ev.Title
	if req.Title != nil {
		title = *req.Title
	}
	desc := ev.Description
	if req.Description != nil {
		desc = *req.Description
	}
	startTime := ev.StartTime
	if req.StartTime != nil {
		startTime = *req.StartTime
	}
	endTime := ev.EndTime
	if req.EndTime != nil {
		endTime = req.EndTime
	}
	chID := ev.ChannelID
	loc := ev.Location
	if req.ChannelID != nil {
		chID = req.ChannelID
		loc = nil
	} else if req.Location != nil {
		loc = req.Location
		chID = nil
	}
	if err := h.validateEventPayload(r, title, desc, startTime, endTime, chID, loc); err != nil {
		return err
	}
	updated, err := h.Store.Update(r.Context(), id, req)
	if err != nil {
		return err
	}
	h.auditRecord(r, "event.edit", updated)
	h.broadcast(updated)
	httpx.WriteJSON(w, http.StatusOK, updated)
	return nil
}

// @Summary Cancel a community event
// @Description Marks an event as cancelled. Requires ManageEvents permission.
// @ID cancelCommunityEvent
// @Tags Events
// @Produce json
// @Security cookieAuth
// @Param id path string true "Event ID" Format(uuid)
// @Success 200 {object} CommunityEvent
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN"
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND"
// @Router /api/events/{id}/cancel [post]
func (h *Handler) cancelEvent(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	if err := h.requireManageEvents(r); err != nil {
		return err
	}
	current, err := h.Store.Get(r.Context(), id, nil)
	if err != nil {
		return err
	}
	if err := h.requireViewable(r, current); err != nil {
		return err
	}
	ev, err := h.Store.Cancel(r.Context(), id)
	if err != nil {
		return err
	}
	h.auditRecord(r, "event.cancel", ev)
	h.broadcast(ev)
	httpx.WriteJSON(w, http.StatusOK, ev)
	return nil
}

func (h *Handler) requireManageEvents(r *http.Request) error {
	if h.Perms == nil {
		return nil
	}
	user := auth.UserFrom(r.Context())
	snap, err := h.Perms.Snapshot(r.Context())
	if err != nil {
		return err
	}
	if !snap.Global(user.Subject()).Has(perm.ManageEvents) {
		return httpx.ErrForbidden("missing ManageEvents permission")
	}
	return nil
}

func validateEventText(title, desc string) error {
	if _, err := httpx.CleanText("title", title, 100, true); err != nil {
		return err
	}
	_, err := httpx.CleanText("description", desc, 2000, false)
	return err
}

func validateEventTimes(start time.Time, end *time.Time) error {
	if start.IsZero() {
		return httpx.ErrInvalidInput("start_time is required")
	}
	if end != nil && end.Before(start) {
		return httpx.ErrInvalidInput("end_time must be at or after start_time")
	}
	return nil
}

func (h *Handler) validateEventLocation(r *http.Request, chID *uuid.UUID, loc *string) error {
	hasChannel := chID != nil
	hasLoc := loc != nil && strings.TrimSpace(*loc) != ""

	if !hasChannel && !hasLoc {
		return httpx.ErrInvalidInput("either channel_id (Talk) or location (text) must be set")
	}
	if hasChannel && hasLoc {
		return httpx.ErrInvalidInput("cannot specify both channel_id and location text")
	}
	if hasChannel {
		var kind string
		err := h.DB.QueryRow(r.Context(), `SELECT type FROM channels WHERE id = $1`, *chID).Scan(&kind)
		if err != nil || kind != "voice" {
			return httpx.ErrInvalidInput("channel_id must be a Talk (voice) channel")
		}
		if !h.canViewChannel(r, *chID) {
			return httpx.ErrForbidden("you cannot view this Talk channel")
		}
	}
	if hasLoc {
		if _, err := httpx.CleanText("location", *loc, 255, true); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) validateEventPayload(r *http.Request, title, desc string, start time.Time, end *time.Time, chID *uuid.UUID, loc *string) error {
	if err := validateEventText(title, desc); err != nil {
		return err
	}
	if err := validateEventTimes(start, end); err != nil {
		return err
	}
	return h.validateEventLocation(r, chID, loc)
}

func (h *Handler) auditRecord(r *http.Request, action string, ev *CommunityEvent) {
	if h.Audit == nil || ev == nil {
		return
	}
	if ev.ChannelID != nil {
		// A Talk event names a possibly private Talk: only auditors who can
		// view that Talk read it (class event.talk_%).
		action = "event.talk_" + strings.TrimPrefix(action, "event.")
	}
	h.Audit.RecordOrLog(r.Context(), audit.Entry{
		Action:     action,
		TargetType: "event",
		TargetID:   ev.ID.String(),
		TargetName: ev.Title,
		ChannelID:  ev.ChannelID,
		Details: map[string]any{
			"title":      ev.Title,
			"start_time": ev.StartTime.Format(time.RFC3339),
			"channel_id": ev.ChannelID,
			"location":   ev.Location,
			"status":     ev.Status,
		},
	})
}

func validStatus(s string) bool {
	switch s {
	case StatusScheduled, StatusActive, StatusCompleted, StatusCancelled:
		return true
	}
	return false
}

func trimPtr(s *string) {
	if s != nil {
		*s = strings.TrimSpace(*s)
	}
}

func (r *CreateEventRequest) normalize() {
	r.Title = strings.TrimSpace(r.Title)
	r.Description = strings.TrimSpace(r.Description)
	trimPtr(r.Location)
}

func (r *UpdateEventRequest) normalize() {
	trimPtr(r.Title)
	trimPtr(r.Description)
	trimPtr(r.Location)
}
