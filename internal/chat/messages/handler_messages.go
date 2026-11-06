package messages

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

func (a *API) validateListChannel(r *http.Request) (uuid.UUID, error) {
	chID, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return uuid.Nil, err
	}
	if err := a.Guard.Require(r, chID, perm.ViewChannel); err != nil {
		return uuid.Nil, err
	}
	if _, err := core.LoadChannel(r.Context(), a.DB, chID); err != nil {
		return uuid.Nil, err
	}
	return chID, nil
}

func (a *API) parseChannelMessageQuery(r *http.Request, chID uuid.UUID) (core.HistoryQuery, error) {
	// The anchor must be a root message of this channel.
	return a.parseHistoryQuery(r, []string{"before", "after", "around"}, func(ref *core.MessageRef) bool {
		return ref.ChannelID == chID && ref.ParentID == nil
	})
}

// ListMessages handles GET /api/channels/{channelID}/messages.
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
// @Success 200 {array} core.Message "A page of messages."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/channels/{channelID}/messages [get]
func (a *API) ListMessages(w http.ResponseWriter, r *http.Request) error {
	chID, err := a.validateListChannel(r)
	if err != nil {
		return err
	}
	q, err := a.parseChannelMessageQuery(r, chID)
	if err != nil {
		return err
	}
	msgs, err := GetChannelMessages(r.Context(), a.DB, chID, q)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, msgs)
	return nil
}

// ServeCreateMessage handles POST /api/channels/{channelID}/messages.
//
// @Summary Post a message
// @Description Works in text and voice channels (a voice channel's chat). Resolves mentions and broadcasts message_create. With attachment_ids the message carries those staged files (up to 10, at most 100 MB together); the text may then be empty.
// @ID createMessage
// @Tags Messages
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param request body messages.CreateMessageRequest true "Request body."
// @Success 201 {object} core.Message "Created message."
// @Success 200 {object} core.Message "The message already created with this nonce (a retried send)."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Send messages permission in the channel, or cross-origin request rejected. RULES_NOT_ACCEPTED: the member has not accepted the community rules yet."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/channels/{channelID}/messages [post]
func (a *API) ServeCreateMessage(w http.ResponseWriter, r *http.Request) error {
	user := auth.UserFrom(r.Context())
	chID, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return err
	}
	var req CreateMessageRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	need := perm.SendMessages
	if len(req.AttachmentIDs) > 0 {
		need |= perm.AttachFiles
	}
	ctx, err := a.ServeSendContext(r, chID, need)
	if err != nil {
		return err
	}
	if replayed, err := a.replayNonce(w, r, user.ID, chID, req.Nonce); replayed || err != nil {
		return err
	}
	if _, err := ChannelForMessages(ctx, a.DB, chID); err != nil {
		return err
	}
	if err := validateAttachmentIDs(req.AttachmentIDs); err != nil {
		return err
	}
	content, err := core.ValidateContent(req.Content, len(req.AttachmentIDs) > 0)
	if err != nil {
		return err
	}
	if err := ValidateTarget(ctx, a.DB, chID, req.ParentID, req.ReplyToID); err != nil {
		return err
	}
	var claim func(pgx.Tx, uuid.UUID) error
	if len(req.AttachmentIDs) > 0 {
		claim = func(tx pgx.Tx, msgID uuid.UUID) error {
			return claimAttachments(ctx, tx, msgID, user.ID, chID, req.AttachmentIDs)
		}
	}
	id, err := InsertMessage(ctx, a.DB, a.Online, NewMessage{
		ChannelID: chID, UserID: user.ID, Content: content, ParentID: req.ParentID, ReplyToID: req.ReplyToID,
		HasAttachment: len(req.AttachmentIDs) > 0, Nonce: req.Nonce,
	}, claim)
	if IsNonceConflict(err) {
		// A concurrent send of the same nonce won the race.
		_, err = a.replayNonce(w, r, user.ID, chID, req.Nonce)
		return err
	}
	if err != nil {
		return err
	}
	return PublishMessageNonce(w, r, a.DB, a.Events, id, req.Nonce)
}

// MessageInChannel loads messageID and checks it belongs to the channel named in
// the path, so URLs cannot mix channels.
func (a *API) MessageInChannel(r *http.Request) (uuid.UUID, *core.MessageRef, error) {
	chID, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return uuid.Nil, nil, err
	}
	msgID, err := httpx.PathUUID(r, "messageID")
	if err != nil {
		return uuid.Nil, nil, err
	}
	ref, err := core.LoadMessageRef(r.Context(), a.DB, msgID)
	if err != nil {
		return uuid.Nil, nil, err
	}
	if ref.ChannelID != chID {
		return uuid.Nil, nil, core.ErrMessageNotFound
	}
	return msgID, ref, nil
}

// ServeEditMessage handles PUT /api/channels/{channelID}/messages/{messageID}.
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
// @Param request body messages.EditMessageRequest true "Request body."
// @Success 200 {object} core.Message "Updated message."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: you are not the author, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/channels/{channelID}/messages/{messageID} [put]
func (a *API) ServeEditMessage(w http.ResponseWriter, r *http.Request) error {
	return a.EditMessageWith(w, r, a.ServeSendContext)
}

// EditMessageWith is editMessage with the mention context of the caller's kind
// (member or bot) supplied by ctxFor.
func (a *API) EditMessageWith(w http.ResponseWriter, r *http.Request, ctxFor func(*http.Request, uuid.UUID, perm.Set) (context.Context, error)) error {
	msgID, ref, err := a.MessageInChannel(r)
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
	content, err := core.ValidateContent(req.Content, true)
	if err != nil {
		return err
	}
	userID := auth.UserFrom(r.Context()).ID
	before := a.snapshotMessage(r.Context(), msgID, false)
	if err := EditMessage(ctx, a.DB, a.Online, msgID, userID, content); err != nil {
		return err
	}
	a.auditEdit(r.Context(), msgID, before, content)
	msg, err := GetMessage(r.Context(), a.DB, msgID)
	if err != nil {
		return err
	}
	a.Events.BroadcastChannel(msg.ChannelID, events.MessageUpdate, msg)
	httpx.WriteJSON(w, http.StatusOK, msg)
	return nil
}

// DeleteMessage handles DELETE /api/channels/{channelID}/messages/{messageID}.
//
// @Summary Delete a message
// @Description The author, or a member with the Manage messages permission in the channel (the Owner always). Also deletes the message's thread replies and attachments. A moderator may send a JSON body {"reason": "..."} (up to 200 characters); the reason goes to the audit log and to the author, who is told through a moderation_notice event. Broadcasts message_delete.
// @ID deleteMessage
// @Tags Messages
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param messageID path string true "Message ID." Format(uuid)
// @Param request body messages.DeleteMessageRequest false "Optional body with a reason."
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: you are neither the author nor hold Manage messages in the channel, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/channels/{channelID}/messages/{messageID} [delete]
func (a *API) DeleteMessage(w http.ResponseWriter, r *http.Request) error {
	msgID, ref, err := a.MessageInChannel(r)
	if err != nil {
		return err
	}
	user := auth.UserFrom(r.Context())
	p, err := a.Guard.ChannelPerms(r, ref.ChannelID)
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
	snap := a.snapshotMessage(r.Context(), msgID, true)
	keys, err := DeleteMessageAs(r.Context(), a.DB, msgID, user, p.Has(perm.ManageMessages))
	if err != nil {
		return err
	}
	a.auditDelete(r.Context(), user, msgID, snap, reason)
	a.tellAuthor(snap, user, reason)
	core.DeleteObjects(r.Context(), a.Objects, keys)
	a.Events.BroadcastChannel(ref.ChannelID, events.MessageDelete, map[string]any{"id": msgID, "channel_id": ref.ChannelID, "parent_id": ref.ParentID})
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// CreateMessageRequest is the body of POST /api/channels/{channelID}/messages.
type CreateMessageRequest struct {
	// Content is required unless attachment_ids is set.
	Content string `json:"content" maxLength:"4000"`
	// AttachmentIDs are files uploaded with POST /api/channels/{channelID}/attachments,
	// at most 10, shown in this order. Needs the Attach files permission; the
	// files must be the caller's, from this channel and not yet sent.
	AttachmentIDs []uuid.UUID `json:"attachment_ids" binding:"optional" format:"uuid" maxItems:"10"`
	// ParentID must be a root message of the same channel (thread reply).
	ParentID *uuid.UUID `json:"parent_id" format:"uuid" binding:"optional" extensions:"x-nullable"`
	// ReplyToID must be a message of the same conversation (quoted reply).
	ReplyToID *uuid.UUID `json:"reply_to_id" format:"uuid" binding:"optional" extensions:"x-nullable"`
	// Nonce is an optional random id the client makes per send. Sending the
	// same nonce again (a retry after a lost connection) returns the first
	// message with 200 instead of posting a second one.
	Nonce *uuid.UUID `json:"nonce,omitempty" format:"uuid" binding:"optional"`
}

// EditMessageRequest is the body of PUT /api/channels/{channelID}/messages/{messageID}.
type EditMessageRequest struct {
	Content string `json:"content" minLength:"1" maxLength:"4000"`
}
