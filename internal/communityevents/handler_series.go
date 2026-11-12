package communityevents

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/communityevents/recurrence"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// followingScope reads ?scope= of a request about an occurrence: "this" (the
// default) is the one event, "following" is it and every later occurrence of its series.
func (h *Handler) followingScope(r *http.Request, ev *CommunityEvent) (bool, error) {
	switch r.URL.Query().Get("scope") {
	case "", "this":
		return false, nil
	case "following":
		if ev.Series == nil {
			return false, httpx.ErrInvalidInput("this event is not part of a series")
		}
		return true, nil
	default:
		return false, httpx.ErrInvalidInput("scope must be this or following")
	}
}

// createOrStartSeries creates the event, or the first occurrence of a series
// when the request asks for a recurrence, and creates the occurrences of the
// next weeks right away.
func (h *Handler) createOrStartSeries(r *http.Request, req CreateEventRequest, userID uuid.UUID) (*CommunityEvent, error) {
	if req.Recurrence == nil {
		return h.Store.create(r.Context(), req, userID, func(tx pgx.Tx, ev *CommunityEvent) error {
			return h.Audit.RecordTx(r.Context(), tx, eventEntry("event.create", ev))
		})
	}
	rec := *req.Recurrence
	if !recurrence.ValidFrequency(rec.Frequency) {
		return nil, httpx.ErrInvalidInput("frequency must be weekly, biweekly or monthly")
	}
	if rec.Until != nil && !rec.Until.After(req.StartTime) {
		return nil, httpx.ErrInvalidInput("until must be after start_time")
	}
	ev, seriesID, err := h.Store.CreateSeries(r.Context(), req, rec, userID)
	if err != nil {
		return nil, err
	}
	h.auditRecord(r, "event.create", ev)
	ids, err := h.Store.GenerateSeries(r.Context(), seriesID, time.Now())
	if err != nil {
		slog.ErrorContext(r.Context(), "create the first occurrences failed", "series_id", seriesID, "err", err)
	}
	h.broadcastIDs(r, ids)
	return ev, nil
}

// broadcastIDs announces the current state of each event.
func (h *Handler) broadcastIDs(r *http.Request, ids []uuid.UUID) {
	for _, id := range ids {
		if ev, err := h.Store.Get(r.Context(), id, nil); err == nil {
			h.broadcast(ev)
		}
	}
}

func (h *Handler) auditSeries(r *http.Request, action string, ev *CommunityEvent, n int) {
	if !h.auditable(r, ev) {
		return
	}
	if ev.ChannelID != nil {
		action = "event.talk_" + action[len("event."):]
	}
	h.Audit.RecordOrLog(r.Context(), audit.Entry{
		Action: action, TargetType: "event", TargetID: ev.ID.String(), TargetName: ev.Title, ChannelID: ev.ChannelID,
		Details: map[string]any{"title": ev.Title, "scope": "following", "series_id": ev.Series.ID, "occurrences": n},
	})
}

// updateFollowing runs an edit (or a cancellation) of an occurrence and every later one.
func (h *Handler) updateFollowing(w http.ResponseWriter, r *http.Request, ev *CommunityEvent, req UpdateEventRequest, cancelling bool) error {
	if cancelling {
		return h.cancelFollowing(w, r, ev)
	}
	req = withoutUnchanged(ev, req)
	if !req.changesFields() {
		httpx.WriteJSON(w, http.StatusOK, ev)
		return nil
	}
	ids, err := h.Store.UpdateFollowing(r.Context(), ev, req)
	if err != nil {
		return err
	}
	h.auditSeries(r, "event.edit", ev, len(ids))
	h.broadcastIDs(r, ids)
	if req.ChannelID != nil {
		for _, id := range ids {
			if moved, err := h.Store.Get(r.Context(), id, nil); err == nil && !sameChannel(ev.ChannelID, moved.ChannelID) {
				if err := h.retractFor(r.Context(), moved, ev.ChannelID); err != nil {
					slog.ErrorContext(r.Context(), "retract moved event failed", "event_id", id, "err", err)
				}
			}
		}
	}
	return h.answerWith(w, r, ev.ID)
}

// cancelFollowing cancels an occurrence and every later one and ends the series.
func (h *Handler) cancelFollowing(w http.ResponseWriter, r *http.Request, ev *CommunityEvent) error {
	ids, err := h.Store.CancelFollowing(r.Context(), ev)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return httpx.ErrConflict("this event is " + ev.Status + " and cannot be cancelled")
	}
	h.auditSeries(r, "event.cancel", ev, len(ids))
	h.broadcastIDs(r, ids)
	return h.answerWith(w, r, ev.ID)
}

// answerWith answers with the event as it stands now.
func (h *Handler) answerWith(w http.ResponseWriter, r *http.Request, id uuid.UUID) error {
	updated, err := h.Store.Get(r.Context(), id, &auth.UserFrom(r.Context()).ID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, updated)
	return nil
}
