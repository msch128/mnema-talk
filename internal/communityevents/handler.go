package communityevents

import (
	"log/slog"
	"net/http"
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
// @Description Returns community events visible to the caller, ordered by start time. Events located in a Talk channel require ViewChannel on that channel. By default the window runs from 1 day ago to 90 days ahead; an event is in the window while it has not ended before after and starts before before.
// @ID listCommunityEvents
// @Tags Events
// @Produce json
// @Security cookieAuth
// @Param after query string false "Earliest end (or start, for events without an end) to include, RFC 3339. Default: now minus 1 day." Format(date-time)
// @Param before query string false "Events must start before this time, RFC 3339. Default: now plus 90 days." Format(date-time)
// @Param limit query int false "Page size, 1 to 200." default(50)
// @Success 200 {array} CommunityEvent
// @Failure 400 {object} httpx.ErrorResponse "INVALID_INPUT"
// @Router /api/events [get]
func (h *Handler) listEvents(w http.ResponseWriter, r *http.Request) error {
	user := auth.UserFrom(r.Context())
	q, err := h.listQuery(r)
	if err != nil {
		return err
	}
	list, err := h.Store.List(r.Context(), &user.ID, q)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, list)
	return nil
}

// listQuery reads after, before and limit and builds the visibility check
// from one permission snapshot. Without a snapshot nothing in a Talk is shown.
func (h *Handler) listQuery(r *http.Request) (ListQuery, error) {
	now := time.Now()
	q := ListQuery{
		After:  now.Add(-listLookBehind),
		Before: now.Add(listLookAhead),
		Limit:  httpx.QueryLimit(r, "limit", DefaultListLimit, MaxListLimit),
	}
	for name, dst := range map[string]*time.Time{"after": &q.After, "before": &q.Before} {
		raw := r.URL.Query().Get(name)
		if raw == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return q, httpx.ErrInvalidInput(name + " must be an RFC 3339 time")
		}
		*dst = t
	}
	if h.Perms == nil {
		return q, nil
	}
	snap, err := h.Perms.Snapshot(r.Context())
	if err != nil {
		return q, err
	}
	sub := auth.UserFrom(r.Context()).Subject()
	q.Allow = func(id uuid.UUID) bool { return snap.Channel(sub, id).Has(perm.ViewChannel) }
	return q, nil
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
	m := mergeUpdate(ev, req)
	if err := h.validateEventPayload(r, m.title, m.desc, m.start, m.end, m.channel, m.location); err != nil {
		return err
	}
	updated, err := h.Store.Update(r.Context(), id, req)
	if err != nil {
		return err
	}
	if updated.ChannelID != nil && !sameChannel(ev.ChannelID, updated.ChannelID) {
		// Members who cannot view the new Talk drop their copy.
		if err := h.retractFor(r.Context(), updated); err != nil {
			slog.ErrorContext(r.Context(), "retract moved event failed", "event_id", id, "err", err)
		}
	}
	h.auditRecord(r, "event.edit", updated)
	h.broadcast(updated)
	httpx.WriteJSON(w, http.StatusOK, updated)
	return nil
}

// merged is an event as it would be after applying an update.
type merged struct {
	title, desc string
	start       time.Time
	end         *time.Time
	channel     *uuid.UUID
	location    *string
}

func mergeUpdate(ev *CommunityEvent, req UpdateEventRequest) merged {
	m := merged{title: ev.Title, desc: ev.Description, start: ev.StartTime, end: ev.EndTime, channel: ev.ChannelID, location: ev.Location}
	if req.Title != nil {
		m.title = *req.Title
	}
	if req.Description != nil {
		m.desc = *req.Description
	}
	if req.StartTime != nil {
		m.start = *req.StartTime
	}
	if req.EndTime != nil {
		m.end = req.EndTime
	}
	if req.ChannelID != nil {
		m.channel, m.location = req.ChannelID, nil
	} else if req.Location != nil {
		m.location, m.channel = req.Location, nil
	}
	return m
}

func sameChannel(a, b *uuid.UUID) bool {
	return a != nil && b != nil && *a == *b
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
