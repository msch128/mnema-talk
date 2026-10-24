package later

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// Mount registers the member routes (RequireUser applies).
func (s *Service) Mount(r chi.Router) {
	changes := r.With(s.changes.By(auth.UserKey))
	r.Get("/me/reminders", httpx.Handle(s.listReminders))
	changes.Post("/me/reminders", httpx.Handle(s.createReminder))
	changes.Delete("/me/reminders/{reminderID}", httpx.Handle(s.deleteReminder))
	r.Get("/me/scheduled", httpx.Handle(s.listScheduled))
	changes.Post("/channels/{channelID}/scheduled", httpx.Handle(s.createScheduled))
	changes.Patch("/me/scheduled/{scheduledID}", httpx.Handle(s.updateScheduled))
	changes.Delete("/me/scheduled/{scheduledID}", httpx.Handle(s.deleteScheduled))
}

// CreateReminderRequest is the body of POST /api/me/reminders.
type CreateReminderRequest struct {
	MessageID uuid.UUID `json:"message_id" format:"uuid"`
	// RemindAt is the absolute time to remind at. The client works it out in the
	// member's own time zone ("tomorrow", "in 3 hours").
	RemindAt time.Time `json:"remind_at" format:"date-time"`
}

// ReminderList is the response of GET /api/me/reminders.
type ReminderList struct {
	Items []Reminder `json:"items"`
}

// listReminders handles GET /api/me/reminders.
//
// @Summary List my reminders
// @Description The caller's reminders: delivered ones first (they wait until dismissed), then waiting ones by time. Visibility is checked on every read: a message the caller can no longer see comes back with available=false and no message. At most 100 reminders.
// @ID listReminders
// @Tags Messages
// @Produce json
// @Security cookieAuth
// @Success 200 {object} ReminderList "The reminders."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/me/reminders [get]
func (s *Service) listReminders(w http.ResponseWriter, r *http.Request) error {
	items, err := s.ListReminders(r.Context(), auth.UserFrom(r.Context()).Subject())
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, ReminderList{Items: items})
	return nil
}

// createReminder handles POST /api/me/reminders.
//
// @Summary Remind me of a message
// @Description Stores a private reminder. At remind_at the caller gets an in-app notification (reminder_update with state due) and a Web Push; the message is checked against their access at that moment, and a message they can no longer see is described as such. The message must be visible to the caller now (404 otherwise). remind_at lies at least 10 seconds and at most a year ahead. At most 100 reminders per member (409 beyond); at most 60 changes a minute.
// @ID createReminder
// @Tags Messages
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body CreateReminderRequest true "Request body."
// @Success 201 {object} Reminder "The reminder."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, or a time that is not in the future or too far ahead."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such message, or the channel is hidden from the caller."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the member has 100 reminders already."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/me/reminders [post]
func (s *Service) createReminder(w http.ResponseWriter, r *http.Request) error {
	var req CreateReminderRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	out, err := s.AddReminder(r.Context(), auth.UserFrom(r.Context()).Subject(), req.MessageID, req.RemindAt)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
	return nil
}

// deleteReminder handles DELETE /api/me/reminders/{reminderID}.
//
// @Summary Cancel or dismiss a reminder
// @Description Cancels a waiting reminder or dismisses a delivered one. Sends reminder_update (removed) to the caller's own sessions.
// @ID deleteReminder
// @Tags Messages
// @Produce json
// @Security cookieAuth
// @Param reminderID path string true "Reminder ID." Format(uuid)
// @Success 204 "Removed."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): bad ID."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the caller has no such reminder."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/me/reminders/{reminderID} [delete]
func (s *Service) deleteReminder(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "reminderID")
	if err != nil {
		return err
	}
	found, err := s.RemoveReminder(r.Context(), auth.UserFrom(r.Context()).ID, id)
	if err != nil {
		return err
	}
	if !found {
		return httpx.ErrNotFound("reminder not found")
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
