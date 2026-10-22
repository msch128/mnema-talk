package chat

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// readState handles GET /api/read-state.
//
// @Summary Unread summary of all channels
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
func (h *Handler) readState(w http.ResponseWriter, r *http.Request) error {
	states, err := GetReadStates(r.Context(), h.DB, auth.UserFrom(r.Context()))
	if err != nil {
		return err
	}
	if states, err = h.visibleReadStates(r, states); err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, states)
	return nil
}

// channelParam returns the {channelID} of an existing channel (text or
// voice: both have a chat and so a read state).
func (h *Handler) channelParam(r *http.Request) (uuid.UUID, error) {
	chID, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return chID, err
	}
	if err := h.require(r, chID, perm.ViewChannel); err != nil {
		return chID, err
	}
	if _, err := LoadChannel(r.Context(), h.DB, chID); err != nil {
		return chID, err
	}
	return chID, nil
}

// publishReadState tells the user's other sessions (tabs, devices) about it.
func (h *Handler) publishReadState(userID, channelID uuid.UUID, payload map[string]any) {
	payload["channel_id"] = channelID
	h.Events.SendToUsers([]uuid.UUID{userID}, "read_state", payload)
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
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/channels/{channelID}/read [post]
func (h *Handler) markRead(w http.ResponseWriter, r *http.Request) error {
	chID, err := h.channelParam(r)
	if err != nil {
		return err
	}
	var req MarkReadRequest
	if r.ContentLength > 0 {
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}
	}
	user := auth.UserFrom(r.Context())
	at, err := MarkRead(r.Context(), h.DB, user.ID, chID, req.MessageID)
	if err != nil {
		return err
	}
	h.publishReadState(user.ID, chID, map[string]any{"last_read_at": at, "unread_count": 0, "mention_count": 0})
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
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/channels/{channelID}/unread [post]
func (h *Handler) markUnread(w http.ResponseWriter, r *http.Request) error {
	chID, err := h.channelParam(r)
	if err != nil {
		return err
	}
	var req MarkUnreadRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	user := auth.UserFrom(r.Context())
	at, err := MarkUnread(r.Context(), h.DB, user.ID, chID, req.MessageID)
	if err != nil {
		return err
	}
	h.publishReadState(user.ID, chID, map[string]any{"last_read_at": at, "refresh": true})
	w.WriteHeader(http.StatusNoContent)
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
