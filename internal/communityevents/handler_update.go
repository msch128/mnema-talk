package communityevents

import (
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// statusRequest checks the status of a PATCH. An event's status follows its
// times, so the only change a member can ask for is cancelling it (which the
// handler runs through the cancel path); naming the current status is a no-op.
func statusRequest(ev *CommunityEvent, status *string) (cancelling bool, err error) {
	switch {
	case status == nil || *status == ev.Status:
		return false, nil
	case !validStatus(*status):
		return false, httpx.ErrInvalidInput("status must be scheduled, active, completed or cancelled")
	case *status == StatusCancelled:
		if ev.Status == StatusCompleted {
			return false, httpx.ErrConflict("this event is completed and cannot be cancelled")
		}
		return true, nil
	default:
		return false, httpx.ErrConflict("the status follows the event's times; an event can only be cancelled")
	}
}

// changesFields reports whether the request edits more than the status.
func (r UpdateEventRequest) changesFields() bool {
	return r.Title != nil || r.Description != nil || r.StartTime != nil || r.EndTime != nil || r.ClearEndTime ||
		r.ChannelID != nil || r.Location != nil
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
	} else if req.ClearEndTime {
		m.end = nil
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
