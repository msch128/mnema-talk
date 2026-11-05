package readstate

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// readState handles GET /api/read-state.
//
// @Summary Unread summary of all channels
// @Description One entry per channel: unread messages and mentions since the last read, the time of the last read and the effective notification level. Drives the unread badges in the channel list.
// @ID getReadState
// @Tags Channels
// @Produce json
// @Security cookieAuth
// @Success 200 {array} ReadState "One entry per channel (text channels and voice channels' chats)."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/read-state [get]
func (a API) readState(w http.ResponseWriter, r *http.Request) error {
	states, err := GetReadStates(r.Context(), a.DB, auth.UserFrom(r.Context()))
	if err != nil {
		return err
	}
	if states, err = visibleReadStates(a.Guard, r, states); err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, states)
	return nil
}

// channelParam returns the {channelID} of an existing channel (text or
// voice: both have a chat and so a read state).
func (a API) channelParam(r *http.Request) (uuid.UUID, error) {
	chID, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return chID, err
	}
	if err := a.Guard.Require(r, chID, perm.ViewChannel); err != nil {
		return chID, err
	}
	if _, err := core.LoadChannel(r.Context(), a.DB, chID); err != nil {
		return chID, err
	}
	return chID, nil
}

// publishReadState tells the user's other sessions (tabs, devices) about it.
func (a API) publishReadState(userID, channelID uuid.UUID, payload map[string]any) {
	payload["channel_id"] = channelID
	a.Events.SendToUsers([]uuid.UUID{userID}, events.ReadState, payload)
}

// markRead handles POST /api/channels/{channelID}/read.
//
// @Summary Mark a channel read
// @Description Moves the read marker forward to the given message, or to now when the body is omitted. Never moves backwards. Text and voice channels. Notifies the user's other sessions.
// @ID markChannelRead
// @Tags Channels
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param request body MarkReadRequest false "Optional; omit the body to mark everything read."
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: cross-origin request rejected by the CSRF check."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/channels/{channelID}/read [post]
func (a API) markRead(w http.ResponseWriter, r *http.Request) error {
	chID, err := a.channelParam(r)
	if err != nil {
		return err
	}
	var req MarkReadRequest
	if err := httpx.DecodeOptionalJSON(r, &req); err != nil {
		return err
	}
	user := auth.UserFrom(r.Context())
	at, err := MarkRead(r.Context(), a.DB, user.ID, chID, req.MessageID)
	if err != nil {
		return err
	}
	a.publishReadState(user.ID, chID, map[string]any{"last_read_at": at, "unread_count": 0, "mention_count": 0})
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// markUnread handles POST /api/channels/{channelID}/unread.
//
// @Summary Mark unread from a message
// @Description Sets the marker just before the message so it and everything after it counts as unread.
// @ID markChannelUnread
// @Tags Channels
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param request body MarkUnreadRequest true "Request body."
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: cross-origin request rejected by the CSRF check."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/channels/{channelID}/unread [post]
func (a API) markUnread(w http.ResponseWriter, r *http.Request) error {
	chID, err := a.channelParam(r)
	if err != nil {
		return err
	}
	var req MarkUnreadRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	user := auth.UserFrom(r.Context())
	at, err := MarkUnread(r.Context(), a.DB, user.ID, chID, req.MessageID)
	if err != nil {
		return err
	}
	a.publishReadState(user.ID, chID, map[string]any{"last_read_at": at, "refresh": true})
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// FirstUnreadResponse is the body of GET /api/channels/{channelID}/first-unread.
type FirstUnreadResponse struct {
	// MessageID is the oldest top-level message by someone else after `since`; null when there is none.
	MessageID *uuid.UUID `json:"message_id" format:"uuid" extensions:"x-nullable"`
}

// firstUnread handles GET /api/channels/{channelID}/first-unread.
//
// @Summary First unread message of a channel
// @Description Returns the oldest top-level message by another member posted after `since` (the read marker the client saw when it opened the channel), so the client can jump to it when it is older than the loaded page. One indexed lookup; it changes nothing.
// @ID getFirstUnread
// @Tags Channels
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param since query string true "RFC 3339 time: the last-read time of the channel." Format(date-time)
// @Success 200 {object} FirstUnreadResponse
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/channels/{channelID}/first-unread [get]
func (a API) firstUnread(w http.ResponseWriter, r *http.Request) error {
	chID, err := a.channelParam(r)
	if err != nil {
		return err
	}
	since, err := time.Parse(time.RFC3339Nano, r.URL.Query().Get("since"))
	if err != nil {
		return httpx.ErrInvalidInput("since must be an RFC 3339 time")
	}
	id, err := FirstUnread(r.Context(), a.DB, auth.UserFrom(r.Context()).ID, chID, since)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, FirstUnreadResponse{MessageID: id})
	return nil
}

// MarkReadRequest is the optional body of POST /api/channels/{channelID}/read.
type MarkReadRequest struct {
	// MessageID is the message to mark read up to; null or omitted means now.
	MessageID *uuid.UUID `json:"message_id" format:"uuid" binding:"optional" extensions:"x-nullable"`
}

// MarkUnreadRequest is the body of POST /api/channels/{channelID}/unread.
type MarkUnreadRequest struct {
	MessageID uuid.UUID `json:"message_id" format:"uuid"`
}
