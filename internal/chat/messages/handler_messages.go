package messages

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/automod"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events/wire"
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
// @Description Works in text and voice channels (a voice channel's chat). Resolves mentions and broadcasts message_create. With attachment_ids the message carries those staged files (up to 10, at most 100 MB together); the text may then be empty. With lifetime_seconds the server deletes the message (as its author's delete would) that long after storing it; the message carries expires_at. A lifetime is refused (400) when lifetimes are off, beyond the community's ceiling, and for the root of a forum post.
// @ID createMessage
// @Tags Messages
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param request body messages.CreateMessageRequest true "Request body."
// @Success 201 {object} core.Message "Created message."
// @Success 200 {object} core.Message "The message already created with this nonce (a retried send)."
// @Failure 410 {object} httpx.ErrorResponse "MESSAGE_DELETED: the message sent with this nonce was deleted since; it is not posted again."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation, or the nonce was already used by this member in another conversation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Send messages permission in the channel, or cross-origin request rejected. RULES_NOT_ACCEPTED: the member has not accepted the community rules yet."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/channels/{channelID}/messages [post]
func (a *API) ServeCreateMessage(w http.ResponseWriter, r *http.Request) error {
	chID, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return err
	}
	var req CreateMessageRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	posted, err := a.Posts.Post(r.Context(), MemberActor(auth.UserFrom(r.Context())), PostCommand{
		ChannelID: chID, Content: req.Content, ParentID: req.ParentID, ReplyToID: req.ReplyToID,
		AttachmentIDs: req.AttachmentIDs, Nonce: req.Nonce, LifetimeSeconds: req.lifetime(),
	})
	if err != nil {
		return err
	}
	WritePosted(w, posted)
	return nil
}

// WritePosted answers a post: 201 with the new message, or 200 with the one a
// retried nonce had already created.
func WritePosted(w http.ResponseWriter, posted *Posted) {
	switch {
	case posted.Message == nil: // see Posted.Message: nothing to show
	case posted.Replayed:
		httpx.WriteJSON(w, http.StatusOK, posted.Message)
	default:
		httpx.WriteJSON(w, http.StatusCreated, posted.Message)
	}
}

// MessageInChannel loads messageID and checks it belongs to the channel named in
// the path, so URLs cannot mix channels. The channel is asked first: one the
// caller may not view answers like one that does not exist, whatever message
// is named with it.
func (a *API) MessageInChannel(r *http.Request) (uuid.UUID, *core.MessageRef, error) {
	chID, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return uuid.Nil, nil, err
	}
	msgID, err := httpx.PathUUID(r, "messageID")
	if err != nil {
		return uuid.Nil, nil, err
	}
	if err := a.Guard.Require(r, chID, perm.ViewChannel); err != nil {
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
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: you are not the author, the message is in a direct message conversation of two, or the chat of its Talk, that cannot be written in any more (a block, or the other account disabled; the same answer as a refused new message), or cross-origin request rejected."
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
	audited := func(tx pgx.Tx) error { return a.auditEdit(r.Context(), tx, msgID) }
	msg, mentioned, err := editMessage(ctx, a.DB, a.Online, msgID, userID, textEdit{content: &content}, audited)
	if err != nil {
		return err
	}
	a.Events.BroadcastChannel(msg.ChannelID, (*core.MessageUpdate)(msg))
	// Only the mentions changed: the members named before or after may count it differently now.
	a.Recount.Tell(msg.ChannelID, &msg.CreatedAt, append(mentioned, msg.Mentions...))
	httpx.WriteJSON(w, http.StatusOK, msg)
	return nil
}

// BotEdit is a bot's edit of its own message (EditBotMessage).
type BotEdit struct {
	// Content is the new text, checked with core.ValidateContent (it may be
	// empty); nil keeps the text, which is then not marked edited.
	Content *string
	// InTx stores the rest of the edit (embeds, components) in the edit's
	// transaction, after the text and before message_update is recorded. It
	// refuses a message that would show nothing.
	InTx func(tx pgx.Tx, messageID uuid.UUID) error
}

// EditBotMessage is the edit of the bot botUser's own message msgID, for the
// bot API's edits that carry more than text (components, the responses to
// interactions). ctx carries the bot's mention policy; the caller checked
// Send messages. It broadcasts message_update.
func (a *API) EditBotMessage(ctx context.Context, msgID, botUser uuid.UUID, e BotEdit) (*core.Message, error) {
	also := func(tx pgx.Tx) error {
		if e.InTx != nil {
			if err := e.InTx(tx, msgID); err != nil {
				return err
			}
		}
		if e.Content == nil {
			return nil // no new version of the text: nothing for the edit log
		}
		return a.auditEdit(ctx, tx, msgID)
	}
	msg, mentioned, err := editMessage(ctx, a.DB, a.Online, msgID, botUser, textEdit{content: e.Content, mayBeEmpty: true}, also)
	if err != nil {
		return nil, err
	}
	a.Events.BroadcastChannel(msg.ChannelID, (*core.MessageUpdate)(msg))
	a.Recount.Tell(msg.ChannelID, &msg.CreatedAt, append(mentioned, msg.Mentions...))
	return msg, nil
}

// textEdit is what an edit does to the text of a message.
type textEdit struct {
	// content is the new text; nil keeps it (a bot changing only its components).
	content *string
	// mayBeEmpty lets the text be emptied without an attachment: a bot's
	// message may be its embeds or components, which its own step checks.
	mayBeEmpty bool
}

// text is the new text, or "" when the edit keeps it.
func (e textEdit) text() string {
	if e.content == nil {
		return ""
	}
	return *e.content
}

// checkEditAutomod applies the automod rules to the new text of an edit. Only
// the author's own edit is checked; whoever else tries gets the refusal of the
// edit itself.
func checkEditAutomod(ctx context.Context, p *db.Pool, messageID, userID uuid.UUID, content string) (*automod.Pending, error) {
	ref, err := core.LoadMessageRef(ctx, p, messageID)
	if errors.Is(err, core.ErrMessageNotFound) {
		return nil, nil // the edit itself answers 404
	}
	if err != nil {
		return nil, err
	}
	if ref.AuthorID != userID || content == "" {
		return nil, nil
	}
	return automod.Of(p).Check(ctx, automod.Post{ChannelID: ref.ChannelID, UserID: userID, Content: content, MessageID: messageID})
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
	newest, err := a.deleteAs(r.Context(), user, msgID, p.Has(perm.ManageMessages), reason)
	if err != nil {
		return err
	}
	a.Events.BroadcastChannel(ref.ChannelID, wire.MessageDelete{ID: msgID, ChannelID: ref.ChannelID, ParentID: ref.ParentID})
	a.Recount.Tell(ref.ChannelID, &newest, nil)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// DeleteOwnMessage deletes a message of user's outside its route (a bot
// deleting its response to an interaction), with the same audit entry and
// events. The caller checked that user can view the channel.
func (a *API) DeleteOwnMessage(ctx context.Context, user *auth.User, msgID uuid.UUID) error {
	ref, err := core.LoadMessageRef(ctx, a.DB, msgID)
	if err != nil {
		return err
	}
	newest, err := a.deleteAs(ctx, user, msgID, false, "")
	if err != nil {
		return err
	}
	a.Events.BroadcastChannel(ref.ChannelID, wire.MessageDelete{ID: msgID, ChannelID: ref.ChannelID, ParentID: ref.ParentID})
	a.Recount.Tell(ref.ChannelID, &newest, nil)
	return nil
}

// deleteAs deletes the message, records the audit entry, tells the author
// and removes the message's files. It returns when the newest removed
// message was written.
func (a *API) deleteAs(ctx context.Context, user *auth.User, msgID uuid.UUID, canManage bool, reason string) (time.Time, error) {
	var snap *auditSnapshot
	keys, newest, err := deleteMessageAs(ctx, a.DB, msgID, user, canManage, func(tx pgx.Tx) (err error) {
		snap, err = a.auditDelete(ctx, tx, user, msgID, reason)
		return err
	})
	if err != nil {
		return newest, err
	}
	a.tellAuthor(snap, user, reason)
	core.DeleteObjects(ctx, a.Objects, keys)
	return newest, nil
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
	// LifetimeSeconds makes the server delete the message that long after
	// it is stored: 60 to 2592000 and at most the community's ceiling
	// (GET /api/message-settings). Fixed at sending. Needs the
	// messages.lifetime feature (doc/timed-messages.md).
	LifetimeSeconds *int `json:"lifetime_seconds,omitempty" minimum:"60" maximum:"2592000" binding:"optional"`
}

// lifetime is the chosen lifetime, 0 for none.
func (req CreateMessageRequest) lifetime() int { return LifetimeOf(req.LifetimeSeconds) }

// EditMessageRequest is the body of PUT /api/channels/{channelID}/messages/{messageID}.
type EditMessageRequest struct {
	Content string `json:"content" minLength:"1" maxLength:"4000"`
}
