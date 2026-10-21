package chat

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

func (h *Handler) validateListChannel(r *http.Request) (uuid.UUID, error) {
	chID, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return uuid.Nil, err
	}
	if err := h.require(r, chID, perm.ViewChannel); err != nil {
		return uuid.Nil, err
	}
	if _, err := LoadChannel(r.Context(), h.DB, chID); err != nil {
		return uuid.Nil, err
	}
	return chID, nil
}

func (h *Handler) parseChannelMessageQuery(r *http.Request, chID uuid.UUID) (HistoryQuery, error) {
	q := HistoryQuery{Limit: httpx.QueryLimit(r, "limit", 50, 100)}
	anchors, err := queryIDs(r, "a message id", map[string]**uuid.UUID{"before": &q.Before, "after": &q.After, "around": &q.Around})
	if err != nil {
		return q, err
	}
	if anchors > 1 {
		return q, httpx.ErrInvalidInput("use only one of before, after or around")
	}
	// The anchor must be a root message of this channel.
	if anchor := firstID(q.Before, q.After, q.Around); anchor != nil {
		ref, err := loadMessageRef(r.Context(), h.DB, *anchor)
		if err != nil || ref.ChannelID != chID || ref.ParentID != nil {
			return q, errMessageNotFound
		}
	}
	return q, nil
}

// listMessages handles GET /api/channels/{channelID}/messages.
//
// @Summary List channel messages
// @Description Top-level messages only, oldest first within the page. Without an anchor returns the newest page. Use at most one of before, after, around; the anchor must be a root message of this channel.
// @ID listMessages
// @Tags Messages
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param limit query int false "Page size. Values below 1 or non-numeric fall back to the default; larger values are clamped to 100." minimum(1) maximum(100) default(50)
// @Param before query string false "Messages older than this message." Format(uuid)
// @Param after query string false "Messages newer than this message." Format(uuid)
// @Param around query string false "Messages around (and including) this message." Format(uuid)
// @Success 200 {array} Message "A page of messages."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/channels/{channelID}/messages [get]
func (h *Handler) listMessages(w http.ResponseWriter, r *http.Request) error {
	chID, err := h.validateListChannel(r)
	if err != nil {
		return err
	}
	q, err := h.parseChannelMessageQuery(r, chID)
	if err != nil {
		return err
	}
	msgs, err := GetChannelMessages(r.Context(), h.DB, chID, q)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, msgs)
	return nil
}

// createMessage handles POST /api/channels/{channelID}/messages.
//
// @Summary Post a message
// @Description Works in text and voice channels (a voice channel's chat). Resolves mentions and broadcasts message_create.
// @ID createMessage
// @Tags Messages
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param request body CreateMessageRequest true "Request body."
// @Success 201 {object} Message "Created message."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/channels/{channelID}/messages [post]
func (h *Handler) createMessage(w http.ResponseWriter, r *http.Request) error {
	user := auth.UserFrom(r.Context())
	chID, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return err
	}
	ctx, err := h.sendContext(r, chID, perm.SendMessages)
	if err != nil {
		return err
	}
	if _, err := ChannelForMessages(ctx, h.DB, chID); err != nil {
		return err
	}
	var req CreateMessageRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	content, err := ValidateContent(req.Content, false)
	if err != nil {
		return err
	}
	if err := ValidateTarget(ctx, h.DB, chID, req.ParentID, req.ReplyToID); err != nil {
		return err
	}
	id, err := InsertMessage(ctx, h.DB, h.Online, NewMessage{
		ChannelID: chID, UserID: user.ID, Content: content, ParentID: req.ParentID, ReplyToID: req.ReplyToID,
	}, nil)
	if err != nil {
		return err
	}
	return PublishMessage(w, r, h.DB, h.Events, id)
}

// messageInChannel loads messageID and checks it belongs to the channel named in
// the path, so URLs cannot mix channels.
func (h *Handler) messageInChannel(r *http.Request) (uuid.UUID, *messageRef, error) {
	chID, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return uuid.Nil, nil, err
	}
	msgID, err := httpx.PathUUID(r, "messageID")
	if err != nil {
		return uuid.Nil, nil, err
	}
	ref, err := loadMessageRef(r.Context(), h.DB, msgID)
	if err != nil {
		return uuid.Nil, nil, err
	}
	if ref.ChannelID != chID {
		return uuid.Nil, nil, errMessageNotFound
	}
	return msgID, ref, nil
}

// editMessage handles PUT /api/channels/{channelID}/messages/{messageID}.
//
// @Summary Edit own message
// @Description Only the author may edit. The content may be empty when the message has an attachment. Broadcasts message_update.
// @ID editMessage
// @Tags Messages
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param messageID path string true "Message ID." Format(uuid)
// @Param request body EditMessageRequest true "Request body."
// @Success 200 {object} Message "Updated message."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/channels/{channelID}/messages/{messageID} [put]
func (h *Handler) editMessage(w http.ResponseWriter, r *http.Request) error {
	return h.editMessageWith(w, r, h.sendContext)
}

// editMessageWith is editMessage with the mention context of the caller's kind
// (member or bot) supplied by ctxFor.
func (h *Handler) editMessageWith(w http.ResponseWriter, r *http.Request, ctxFor func(*http.Request, uuid.UUID, perm.Set) (context.Context, error)) error {
	msgID, ref, err := h.messageInChannel(r)
	if err != nil {
		return err
	}
	ctx, err := ctxFor(r, ref.ChannelID, perm.SendMessages)
	if err != nil {
		return err
	}
	var req EditMessageRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	// Empty is allowed for a message with an attachment; EditMessage checks.
	content, err := ValidateContent(req.Content, true)
	if err != nil {
		return err
	}
	userID := auth.UserFrom(r.Context()).ID
	before := h.snapshotMessage(r.Context(), msgID, false)
	if err := EditMessage(ctx, h.DB, h.Online, msgID, userID, content); err != nil {
		return err
	}
	h.auditEdit(r.Context(), msgID, before, content)
	msg, err := GetMessage(r.Context(), h.DB, msgID)
	if err != nil {
		return err
	}
	h.Events.BroadcastChannel(msg.ChannelID, "message_update", msg)
	httpx.WriteJSON(w, http.StatusOK, msg)
	return nil
}

// deleteMessage handles DELETE /api/channels/{channelID}/messages/{messageID}.
//
// @Summary Delete a message
// @Description The author, or a member with the Manage messages permission in the channel (the Owner always). Also deletes the message's thread replies and attachments. A moderator may send a JSON body {"reason": "..."} (up to 200 characters); the reason goes to the audit log and to the author, who is told through a moderation_notice event. Broadcasts message_delete.
// @ID deleteMessage
// @Tags Messages
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param messageID path string true "Message ID." Format(uuid)
// @Param request body DeleteMessageRequest false "Optional body with a reason."
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/channels/{channelID}/messages/{messageID} [delete]
func (h *Handler) deleteMessage(w http.ResponseWriter, r *http.Request) error {
	msgID, ref, err := h.messageInChannel(r)
	if err != nil {
		return err
	}
	user := auth.UserFrom(r.Context())
	p, err := h.channelPerms(r, ref.ChannelID)
	if err != nil {
		return err
	}
	if !p.Has(perm.ViewChannel) {
		return perm.ErrChannelHidden
	}
	reason, err := deleteReason(r)
	if err != nil {
		return err
	}
	snap := h.snapshotMessage(r.Context(), msgID, true)
	keys, err := DeleteMessageAs(r.Context(), h.DB, msgID, user, p.Has(perm.ManageMessages))
	if err != nil {
		return err
	}
	h.auditDelete(r.Context(), user, msgID, snap, reason)
	h.tellAuthor(snap, user, reason)
	h.deleteObjects(r.Context(), keys)
	h.Events.BroadcastChannel(ref.ChannelID, "message_delete", map[string]any{"id": msgID, "channel_id": ref.ChannelID, "parent_id": ref.ParentID})
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// CreateMessageRequest is the body of POST /api/channels/{channelID}/messages.
type CreateMessageRequest struct {
	Content string `json:"content" minLength:"1" maxLength:"4000"`
	// ParentID must be a root message of the same channel (thread reply).
	ParentID *uuid.UUID `json:"parent_id" format:"uuid" binding:"optional" extensions:"x-nullable"`
	// ReplyToID must be a message of the same conversation (quoted reply).
	ReplyToID *uuid.UUID `json:"reply_to_id" format:"uuid" binding:"optional" extensions:"x-nullable"`
}

// EditMessageRequest is the body of PUT /api/channels/{channelID}/messages/{messageID}.
type EditMessageRequest struct {
	Content string `json:"content" minLength:"1" maxLength:"4000"`
}
