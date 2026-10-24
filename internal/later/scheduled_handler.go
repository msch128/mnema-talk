package later

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// CreateScheduledRequest is the body of POST /api/channels/{channelID}/scheduled.
type CreateScheduledRequest struct {
	Content string `json:"content" minLength:"1" maxLength:"4000"`
	// SendAt is the absolute time to send at, worked out by the client in the
	// member's own time zone.
	SendAt    time.Time  `json:"send_at" format:"date-time"`
	ParentID  *uuid.UUID `json:"parent_id" binding:"optional" format:"uuid" extensions:"x-nullable"`
	ReplyToID *uuid.UUID `json:"reply_to_id" binding:"optional" format:"uuid" extensions:"x-nullable"`
}

// UpdateScheduledRequest is the body of PATCH /api/me/scheduled/{scheduledID};
// a field left out stays as it is.
type UpdateScheduledRequest struct {
	Content *string `json:"content" binding:"optional" minLength:"1" maxLength:"4000" extensions:"x-nullable"`
	// SendAt moves the message; for a failed one it also puts it back to waiting.
	SendAt *time.Time `json:"send_at" binding:"optional" format:"date-time" extensions:"x-nullable"`
}

// ScheduledList is the response of GET /api/me/scheduled.
type ScheduledList struct {
	Items []Scheduled `json:"items"`
}

// listScheduled handles GET /api/me/scheduled.
//
// @Summary List my scheduled messages
// @Description The caller's scheduled messages in channels and conversations: failed ones first (the server refused to send them at the set time and kept them as drafts), then waiting ones by time. At most 50.
// @ID listScheduledMessages
// @Tags Messages
// @Produce json
// @Security cookieAuth
// @Success 200 {object} ScheduledList "The scheduled messages."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/me/scheduled [get]
func (s *Service) listScheduled(w http.ResponseWriter, r *http.Request) error {
	items, err := s.ListScheduled(r.Context(), auth.UserFrom(r.Context()).Subject())
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, ScheduledList{Items: items})
	return nil
}

// createScheduled handles POST /api/channels/{channelID}/scheduled.
//
// @Summary Schedule a message
// @Description Stores a message that the server sends at send_at as the caller, through the normal send path: permissions, slow mode, timeouts, blocks and the direct message rules are checked again at send time, and a refusal turns it into a failed draft (scheduled_message_update with state failed). Needs Send messages in the channel now. Works in channels and conversations. Text only. send_at lies at least 10 seconds and at most a year ahead. At most 50 per member (409 beyond).
// @ID createScheduledMessage
// @Tags Messages
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel or conversation ID." Format(uuid)
// @Param request body CreateScheduledRequest true "Request body."
// @Success 201 {object} Scheduled "The scheduled message."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): bad content, a time that is not in the future or too far ahead, an unknown reply target."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Send messages permission in the channel. RULES_NOT_ACCEPTED: the member has not accepted the community rules yet."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such channel, or it is hidden from the caller."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the member has 50 scheduled messages already."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/channels/{channelID}/scheduled [post]
func (s *Service) createScheduled(w http.ResponseWriter, r *http.Request) error {
	ch, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return err
	}
	ctx, err := chat.SendContext(s.Perms, r, ch, perm.SendMessages)
	if err != nil {
		return err
	}
	var req CreateScheduledRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	content, err := chat.ValidateContent(req.Content, false)
	if err != nil {
		return err
	}
	if _, err := chat.ChannelForMessages(ctx, s.DB, ch); err != nil {
		return err
	}
	if err := chat.ValidateTarget(ctx, s.DB, ch, req.ParentID, req.ReplyToID); err != nil {
		return err
	}
	out, err := s.AddScheduled(ctx, NewScheduled{
		User: auth.UserFrom(ctx).ID, Channel: ch, ParentID: req.ParentID, ReplyToID: req.ReplyToID, Content: content, SendAt: req.SendAt,
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
	return nil
}

// updateScheduled handles PATCH /api/me/scheduled/{scheduledID}.
//
// @Summary Edit a scheduled message
// @Description Changes the text and/or the time of a waiting or failed scheduled message. A new time puts a failed message back to waiting. Checks run at send time, not here.
// @ID updateScheduledMessage
// @Tags Messages
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param scheduledID path string true "Scheduled message ID." Format(uuid)
// @Param request body UpdateScheduledRequest true "Request body."
// @Success 200 {object} Scheduled "The scheduled message."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): bad content or time."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the caller has no such scheduled message (it may have been sent)."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/me/scheduled/{scheduledID} [patch]
func (s *Service) updateScheduled(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "scheduledID")
	if err != nil {
		return err
	}
	var req UpdateScheduledRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if req.Content != nil {
		c, err := chat.ValidateContent(*req.Content, false)
		if err != nil {
			return err
		}
		req.Content = &c
	}
	out, err := s.UpdateScheduled(r.Context(), auth.UserFrom(r.Context()).ID, id, ScheduledPatch{Content: req.Content, SendAt: req.SendAt})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

// deleteScheduled handles DELETE /api/me/scheduled/{scheduledID}.
//
// @Summary Delete a scheduled message
// @Description Removes a waiting or failed scheduled message.
// @ID deleteScheduledMessage
// @Tags Messages
// @Produce json
// @Security cookieAuth
// @Param scheduledID path string true "Scheduled message ID." Format(uuid)
// @Success 204 "Removed."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): bad ID."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the caller has no such scheduled message (it may have been sent)."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/me/scheduled/{scheduledID} [delete]
func (s *Service) deleteScheduled(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "scheduledID")
	if err != nil {
		return err
	}
	if err := s.RemoveScheduled(r.Context(), auth.UserFrom(r.Context()).ID, id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
