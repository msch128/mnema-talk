package chat

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// Handler serves channels, messages, threads and reactions.
type Handler struct {
	DB     *db.Pool
	Events events.Publisher
	// Objects deletes the media of removed messages and channels; nil skips it.
	Objects ObjectDeleter
	// Online resolves @here; nil means nobody is online.
	Online OnlineSource
	// Voice empties the voice room of a deleted channel. When nil, Online is
	// used if it implements VoiceRooms (the WebSocket hub does).
	Voice VoiceRooms
}

// VoiceRooms acts on live voice rooms (implemented by the WebSocket hub).
type VoiceRooms interface {
	// CloseVoiceChannel disconnects everyone from the channel's voice room.
	CloseVoiceChannel(channelID uuid.UUID)
}

func (h *Handler) voiceRooms() VoiceRooms {
	if h.Voice != nil {
		return h.Voice
	}
	v, _ := h.Online.(VoiceRooms)
	return v
}

// ObjectDeleter removes stored media objects (implemented by media.Store).
type ObjectDeleter interface {
	DeleteBatch(ctx context.Context, keys []string) error
}

// deleteObjects removes media objects after their rows are gone. A failure
// only leaves an orphaned object behind, so it is logged, not returned.
func (h *Handler) deleteObjects(ctx context.Context, keys []string) {
	if h.Objects == nil || len(keys) == 0 {
		return
	}
	if err := h.Objects.DeleteBatch(ctx, keys); err != nil {
		slog.Warn("delete media objects", "count", len(keys), "err", err)
	}
}

// Mount registers the member-facing routes (RequireUser must already apply).
func (h *Handler) Mount(r chi.Router) {
	r.Get("/members", httpx.Handle(h.listMembers))
	r.Get("/channels", httpx.Handle(h.listChannels))
	r.Get("/channels/{channelID}/messages", httpx.Handle(h.listMessages))
	r.Post("/channels/{channelID}/messages", httpx.Handle(h.createMessage))
	r.Put("/channels/{channelID}/messages/{messageID}", httpx.Handle(h.editMessage))
	r.Delete("/channels/{channelID}/messages/{messageID}", httpx.Handle(h.deleteMessage))
	r.Post("/messages/{messageID}/reactions", httpx.Handle(h.toggleReaction))
	r.Get("/messages/{messageID}/thread", httpx.Handle(h.getThread))
	r.Get("/read-state", httpx.Handle(h.readState))
	r.Get("/search", httpx.Handle(h.search))
	r.Post("/channels/{channelID}/read", httpx.Handle(h.markRead))
	r.Post("/channels/{channelID}/unread", httpx.Handle(h.markUnread))
	r.Put("/channels/{channelID}/notifications", httpx.Handle(h.setNotifyLevel))
}

// MountAdmin registers channel and category management (RequireAdmin applies).
func (h *Handler) MountAdmin(r chi.Router) {
	r.Post("/categories", httpx.Handle(h.createCategory))
	r.Delete("/categories/{id}", httpx.Handle(h.deleteCategory))
	r.Post("/channels", httpx.Handle(h.createChannel))
	r.Delete("/channels/{id}", httpx.Handle(h.deleteChannel))
	r.Patch("/channels/{id}", httpx.Handle(h.updateChannel))
	r.Patch("/categories/{id}", httpx.Handle(h.renameCategory))
	r.Put("/layout", httpx.Handle(h.applyLayout))
}

// listMembers handles GET /api/members.
//
// @Summary List members
// @Description Every account, admins first, then by display name, with voice_seconds and message_count. Presence and locale are not filled.
// @ID listMembers
// @Tags Users
// @Produce json
// @Security cookieAuth
// @Success 200 {array} auth.User "Members."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/members [get]
func (h *Handler) listMembers(w http.ResponseWriter, r *http.Request) error {
	members, err := GetAllMembers(r.Context(), h.DB)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, members)
	return nil
}

// listChannels handles GET /api/channels.
//
// @Summary List categories and channels
// @ID listChannels
// @Tags Channels
// @Produce json
// @Security cookieAuth
// @Success 200 {object} ChannelHierarchy "Channel hierarchy."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/channels [get]
func (h *Handler) listChannels(w http.ResponseWriter, r *http.Request) error {
	cats, uncat, err := GetServerHierarchy(r.Context(), h.DB)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, ChannelHierarchy{Categories: cats, Uncategorized: uncat})
	return nil
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
	chID, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return err
	}
	if _, err := LoadChannel(r.Context(), h.DB, chID); err != nil {
		return err
	}
	q := HistoryQuery{Limit: httpx.QueryLimit(r, "limit", 50, 100)}
	anchors, err := queryIDs(r, "a message id", map[string]**uuid.UUID{"before": &q.Before, "after": &q.After, "around": &q.Around})
	if err != nil {
		return err
	}
	if anchors > 1 {
		return httpx.ErrInvalidInput("use only one of before, after or around")
	}
	// The anchor must be a root message of this channel.
	if anchor := firstID(q.Before, q.After, q.Around); anchor != nil {
		ref, err := loadMessageRef(r.Context(), h.DB, *anchor)
		if err != nil || ref.ChannelID != chID || ref.ParentID != nil {
			return errMessageNotFound
		}
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
// @Description Not allowed in voice channels. Resolves mentions and broadcasts message_create.
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
	if _, err := TextChannel(r.Context(), h.DB, chID); err != nil {
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
	if err := ValidateTarget(r.Context(), h.DB, chID, req.ParentID, req.ReplyToID); err != nil {
		return err
	}
	id, err := InsertMessage(r.Context(), h.DB, h.Online, NewMessage{
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
	msgID, _, err := h.messageInChannel(r)
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
	if err := EditMessage(r.Context(), h.DB, h.Online, msgID, userID, content); err != nil {
		return err
	}
	msg, err := GetMessage(r.Context(), h.DB, msgID)
	if err != nil {
		return err
	}
	h.Events.Broadcast("message_update", msg)
	httpx.WriteJSON(w, http.StatusOK, msg)
	return nil
}

// deleteMessage handles DELETE /api/channels/{channelID}/messages/{messageID}.
//
// @Summary Delete a message
// @Description Author or admin. Also deletes the message's thread replies and attachments. Broadcasts message_delete.
// @ID deleteMessage
// @Tags Messages
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param messageID path string true "Message ID." Format(uuid)
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
	keys, err := DeleteMessage(r.Context(), h.DB, msgID, auth.UserFrom(r.Context()))
	if err != nil {
		return err
	}
	h.deleteObjects(r.Context(), keys)
	h.Events.Broadcast("message_delete", map[string]any{"id": msgID, "channel_id": ref.ChannelID, "parent_id": ref.ParentID})
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// toggleReaction handles POST /api/messages/{messageID}/reactions.
//
// @Summary Toggle a reaction
// @Description Adds the caller's reaction, or removes it if already present. A user may add at most 20 different emoji to one message, and a message carries at most 50 different emoji (409 CONFLICT beyond that). Broadcasts message_reaction.
// @ID toggleReaction
// @Tags Messages
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param messageID path string true "Message ID." Format(uuid)
// @Param request body ReactionRequest true "Request body."
// @Success 200 {object} ReactionResult "Resulting reactions."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: too many reactions on this message."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/messages/{messageID}/reactions [post]
func (h *Handler) toggleReaction(w http.ResponseWriter, r *http.Request) error {
	user := auth.UserFrom(r.Context())
	msgID, err := httpx.PathUUID(r, "messageID")
	if err != nil {
		return err
	}
	var req ReactionRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	emoji, err := ValidateEmoji(req.Emoji)
	if err != nil {
		return err
	}
	reactions, err := ToggleReaction(r.Context(), h.DB, msgID, user.ID, emoji)
	if err != nil {
		return err
	}
	payload := ReactionResult{MessageID: msgID, Reactions: reactions}
	h.Events.Broadcast("message_reaction", payload)
	httpx.WriteJSON(w, http.StatusOK, payload)
	return nil
}

// getThread handles GET /api/messages/{messageID}/thread.
//
// @Summary Get a thread
// @Description The root message plus a page of replies, oldest first. Use at most one of before, after (reply IDs). The message must be a root message; a reply has no thread (404).
// @ID getThread
// @Tags Messages
// @Produce json
// @Security cookieAuth
// @Param messageID path string true "Message ID." Format(uuid)
// @Param limit query int false "Page size. Values below 1 or non-numeric fall back to the default; larger values are clamped to 100." minimum(1) maximum(100) default(50)
// @Param before query string false "Replies older than this reply." Format(uuid)
// @Param after query string false "Replies newer than this reply." Format(uuid)
// @Success 200 {object} Thread "Thread."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/messages/{messageID}/thread [get]
func (h *Handler) getThread(w http.ResponseWriter, r *http.Request) error {
	msgID, err := httpx.PathUUID(r, "messageID")
	if err != nil {
		return err
	}
	root, err := GetMessage(r.Context(), h.DB, msgID)
	if err != nil {
		return err
	}
	// Only a root message has a thread; a reply is not one.
	if root.ParentID != nil {
		return errMessageNotFound
	}

	q := HistoryQuery{Limit: httpx.QueryLimit(r, "limit", 50, 100)}
	anchors, err := queryIDs(r, "a message id", map[string]**uuid.UUID{"before": &q.Before, "after": &q.After})
	if err != nil {
		return err
	}
	if anchors > 1 {
		return httpx.ErrInvalidInput("use only one of before or after")
	}
	if anchor := firstID(q.Before, q.After); anchor != nil {
		ref, err := loadMessageRef(r.Context(), h.DB, *anchor)
		if err != nil || ref.ParentID == nil || *ref.ParentID != msgID {
			return errMessageNotFound
		}
	}

	replies, err := GetThreadReplies(r.Context(), h.DB, msgID, q)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, Thread{Root: root, Replies: replies})
	return nil
}

// createCategory handles POST /api/admin/categories.
//
// @Summary Create a category
// @Description Requires role admin (403 otherwise).
// @ID createCategory
// @Tags Admin
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body CreateCategoryRequest true "Request body."
// @Success 201 {object} Category "Created category."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/categories [post]
func (h *Handler) createCategory(w http.ResponseWriter, r *http.Request) error {
	var req CreateCategoryRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	cat, err := CreateCategory(r.Context(), h.DB, req.Name, req.SortOrder)
	if err != nil {
		return err
	}
	h.Events.Broadcast("channels_changed", nil)
	httpx.WriteJSON(w, http.StatusCreated, cat)
	return nil
}

// deleteCategory handles DELETE /api/admin/categories/{id}.
//
// @Summary Delete a category
// @Description Its channels become uncategorized. Requires role admin (403 otherwise).
// @ID deleteCategory
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Param id path string true "Resource ID." Format(uuid)
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/categories/{id} [delete]
func (h *Handler) deleteCategory(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	if err := DeleteCategory(r.Context(), h.DB, id); err != nil {
		return err
	}
	h.Events.Broadcast("channels_changed", nil)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// createChannel handles POST /api/admin/channels.
//
// @Summary Create a channel
// @Description Requires role admin (403 otherwise).
// @ID createChannel
// @Tags Admin
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body CreateChannelRequest true "Request body."
// @Success 201 {object} Channel "Created channel."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/channels [post]
func (h *Handler) createChannel(w http.ResponseWriter, r *http.Request) error {
	var req CreateChannelRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if req.Type == "" {
		req.Type = ChannelTypeText
	}
	ch, err := CreateChannel(r.Context(), h.DB, req.CategoryID, req.Name, req.Type, req.Topic, req.SortOrder)
	if err != nil {
		return err
	}
	h.Events.Broadcast("channels_changed", nil)
	httpx.WriteJSON(w, http.StatusCreated, ch)
	return nil
}

// deleteChannel handles DELETE /api/admin/channels/{id}.
//
// @Summary Delete a channel
// @Description Cascades to its messages and attachments; everyone in a deleted voice channel is disconnected from it. Requires role admin (403 otherwise).
// @ID deleteChannel
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Param id path string true "Resource ID." Format(uuid)
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/channels/{id} [delete]
func (h *Handler) deleteChannel(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	keys, err := DeleteChannel(r.Context(), h.DB, id)
	if err != nil {
		return err
	}
	// Nobody may stay connected to the voice room of a channel that is gone.
	if v := h.voiceRooms(); v != nil {
		v.CloseVoiceChannel(id)
	}
	h.deleteObjects(r.Context(), keys)
	h.Events.Broadcast("channels_changed", nil)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// readState handles GET /api/read-state.
//
// @Summary Unread summary of all text channels
// @ID getReadState
// @Tags Channels
// @Produce json
// @Security cookieAuth
// @Success 200 {array} ReadState "One entry per text channel."
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
	httpx.WriteJSON(w, http.StatusOK, states)
	return nil
}

// textChannelParam returns the {channelID} of an existing text channel.
func (h *Handler) textChannelParam(r *http.Request) (uuid.UUID, error) {
	chID, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return chID, err
	}
	ch, err := LoadChannel(r.Context(), h.DB, chID)
	if err != nil {
		return chID, err
	}
	if ch.Type != ChannelTypeText {
		return chID, httpx.ErrInvalidInput("only text channels have a read state")
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
// @Description Moves the read marker forward to the given message, or to now when the body is omitted. Never moves backwards. Text channels only. Notifies the user's other sessions.
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
	chID, err := h.textChannelParam(r)
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
	chID, err := h.textChannelParam(r)
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

// setNotifyLevel handles PUT /api/channels/{channelID}/notifications.
//
// @Summary Set channel notification level
// @ID setChannelNotifications
// @Tags Channels
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param request body SetNotifyLevelRequest true "Request body."
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/channels/{channelID}/notifications [put]
func (h *Handler) setNotifyLevel(w http.ResponseWriter, r *http.Request) error {
	chID, err := h.textChannelParam(r)
	if err != nil {
		return err
	}
	var req SetNotifyLevelRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	user := auth.UserFrom(r.Context())
	if err := SetNotifyLevel(r.Context(), h.DB, user.ID, chID, req.Level); err != nil {
		return err
	}
	h.publishReadState(user.ID, chID, map[string]any{"notify_level": req.Level})
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// search handles GET /api/search.
//
// @Summary Search messages
// @Description Text channels only, newest first. Every term (max 8) must occur case-insensitively. At least one of q, channel_id, author_id or has is required.
// @ID searchMessages
// @Tags Messages
// @Produce json
// @Security cookieAuth
// @Param q query string false "Search terms, whitespace separated." maxlength(200)
// @Param channel_id query string false "Only messages of this channel." Format(uuid)
// @Param author_id query string false "Only messages of this author." Format(uuid)
// @Param has query string false "Only messages with a file, an image or a link." Enums(file,image,link)
// @Param before query string false "Page back: only messages older than this message." Format(uuid)
// @Param limit query int false "Page size. Values below 1 or non-numeric fall back to the default; larger values are clamped to 50." minimum(1) maximum(50) default(25)
// @Success 200 {object} SearchResult "One page of results."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/search [get]
func (h *Handler) search(w http.ResponseWriter, r *http.Request) error {
	v := r.URL.Query()
	q := SearchQuery{
		Text:  v.Get("q"),
		Has:   v.Get("has"),
		Limit: httpx.QueryLimit(r, "limit", 25, 50),
	}
	if _, err := queryIDs(r, "an id", map[string]**uuid.UUID{"channel_id": &q.ChannelID, "author_id": &q.AuthorID, "before": &q.Before}); err != nil {
		return err
	}
	msgs, more, err := Search(r.Context(), h.DB, q)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, SearchResult{Messages: msgs, HasMore: more})
	return nil
}

// updateChannel handles PATCH /api/admin/channels/{id}.
//
// @Summary Rename a channel or change its topic
// @Description Requires role admin (403 otherwise).
// @ID updateChannel
// @Tags Admin
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "Resource ID." Format(uuid)
// @Param request body UpdateChannelRequest true "Request body."
// @Success 200 {object} Channel "Updated channel."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/channels/{id} [patch]
func (h *Handler) updateChannel(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	var req UpdateChannelRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	ch, err := UpdateChannel(r.Context(), h.DB, id, req.Name, req.Topic)
	if err != nil {
		return err
	}
	h.Events.Broadcast("channels_changed", nil)
	httpx.WriteJSON(w, http.StatusOK, ch)
	return nil
}

// renameCategory handles PATCH /api/admin/categories/{id}.
//
// @Summary Rename a category
// @Description Requires role admin (403 otherwise).
// @ID renameCategory
// @Tags Admin
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "Resource ID." Format(uuid)
// @Param request body RenameCategoryRequest true "Request body."
// @Success 200 {object} RenamedCategory "Renamed category."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/categories/{id} [patch]
func (h *Handler) renameCategory(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	var req RenameCategoryRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if err := RenameCategory(r.Context(), h.DB, id, req.Name); err != nil {
		return err
	}
	h.Events.Broadcast("channels_changed", nil)
	httpx.WriteJSON(w, http.StatusOK, RenamedCategory{ID: id, Name: req.Name})
	return nil
}

// applyLayout handles PUT /api/admin/layout.
//
// @Summary Apply category order and channel placement
// @Description Applied in one transaction. Requires role admin (403 otherwise).
// @ID applyLayout
// @Tags Admin
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body LayoutRequest true "Request body."
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/layout [put]
func (h *Handler) applyLayout(w http.ResponseWriter, r *http.Request) error {
	var req LayoutRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if len(req.Categories)+len(req.Channels) > 500 {
		return httpx.ErrInvalidInput("too many entries")
	}
	if err := ApplyLayout(r.Context(), h.DB, req.Categories, req.Channels); err != nil {
		return err
	}
	h.Events.Broadcast("channels_changed", nil)
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

// ReactionRequest is the body of POST /api/messages/{messageID}/reactions.
type ReactionRequest struct {
	// Emoji has no whitespace, control characters, '<' or '>'.
	Emoji string `json:"emoji" maxLength:"32"`
}

// ReactionResult is the reaction summary after a toggle.
type ReactionResult struct {
	MessageID uuid.UUID         `json:"message_id" format:"uuid"`
	Reactions []ReactionSummary `json:"reactions"`
}

// Thread is a root message with a page of its replies.
type Thread struct {
	Root    *Message  `json:"root"`
	Replies []Message `json:"replies"`
}

// SearchResult is one page of search hits.
type SearchResult struct {
	Messages []Message `json:"messages"`
	HasMore  bool      `json:"has_more"`
}

// ChannelHierarchy is the response of GET /api/channels.
type ChannelHierarchy struct {
	Categories    []Category `json:"categories"`
	Uncategorized []Channel  `json:"uncategorized"`
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

// SetNotifyLevelRequest is the body of PUT /api/channels/{channelID}/notifications.
type SetNotifyLevelRequest struct {
	Level NotifyLevel `json:"level"`
}

// CreateCategoryRequest is the body of POST /api/admin/categories.
type CreateCategoryRequest struct {
	Name      string `json:"name" minLength:"1" maxLength:"64"`
	SortOrder int    `json:"sort_order" binding:"optional"`
}

// RenameCategoryRequest is the body of PATCH /api/admin/categories/{id}.
type RenameCategoryRequest struct {
	Name string `json:"name" minLength:"1" maxLength:"64"`
}

// RenamedCategory is the response of PATCH /api/admin/categories/{id}.
type RenamedCategory struct {
	ID   uuid.UUID `json:"id" format:"uuid"`
	Name string    `json:"name"`
}

// CreateChannelRequest is the body of POST /api/admin/channels.
type CreateChannelRequest struct {
	CategoryID *uuid.UUID  `json:"category_id" format:"uuid" binding:"optional" extensions:"x-nullable"`
	Name       string      `json:"name" minLength:"1" maxLength:"64"`
	Type       ChannelType `json:"type" default:"text" binding:"optional"`
	Topic      string      `json:"topic" maxLength:"255" binding:"optional"`
	SortOrder  int         `json:"sort_order" binding:"optional"`
}

// UpdateChannelRequest is the body of PATCH /api/admin/channels/{id}; omitted
// fields are kept.
type UpdateChannelRequest struct {
	Name  *string `json:"name" minLength:"1" maxLength:"64" binding:"optional"`
	Topic *string `json:"topic" maxLength:"255" binding:"optional"`
}

// LayoutRequest is the body of PUT /api/admin/layout: at most 500 entries in
// total; an unknown ID rejects the whole change.
type LayoutRequest struct {
	Categories []CategoryOrder    `json:"categories" binding:"optional"`
	Channels   []ChannelPlacement `json:"channels" binding:"optional"`
}
