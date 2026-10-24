package communityevents

import (
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/httpx"
)

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
