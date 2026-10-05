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
}

// MountAdmin registers channel and category management (RequireAdmin applies).
func (h *Handler) MountAdmin(r chi.Router) {
	r.Post("/categories", httpx.Handle(h.createCategory))
	r.Delete("/categories/{id}", httpx.Handle(h.deleteCategory))
	r.Post("/channels", httpx.Handle(h.createChannel))
	r.Delete("/channels/{id}", httpx.Handle(h.deleteChannel))
}

func (h *Handler) listMembers(w http.ResponseWriter, r *http.Request) error {
	members, err := GetAllMembers(r.Context(), h.DB)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, members)
	return nil
}

func (h *Handler) listChannels(w http.ResponseWriter, r *http.Request) error {
	cats, uncat, err := GetServerHierarchy(r.Context(), h.DB)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"categories": cats, "uncategorized": uncat})
	return nil
}

func (h *Handler) listMessages(w http.ResponseWriter, r *http.Request) error {
	chID, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return err
	}
	if _, err := LoadChannel(r.Context(), h.DB, chID); err != nil {
		return err
	}
	q := HistoryQuery{Limit: httpx.QueryLimit(r, "limit", 50, 100)}
	anchors := 0
	for name, dst := range map[string]**uuid.UUID{"before": &q.Before, "after": &q.After, "around": &q.Around} {
		raw := r.URL.Query().Get(name)
		if raw == "" {
			continue
		}
		id, err := uuid.Parse(raw)
		if err != nil {
			return httpx.ErrInvalidInput(name + " must be a message id")
		}
		*dst = &id
		anchors++
		// The anchor must be a root message of this channel.
		ref, err := loadMessageRef(r.Context(), h.DB, id)
		if err != nil || ref.ChannelID != chID || ref.ParentID != nil {
			return errMessageNotFound
		}
	}
	if anchors > 1 {
		return httpx.ErrInvalidInput("use only one of before, after or around")
	}
	msgs, err := GetChannelMessages(r.Context(), h.DB, chID, q)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, msgs)
	return nil
}

func (h *Handler) createMessage(w http.ResponseWriter, r *http.Request) error {
	user := auth.UserFrom(r.Context())
	chID, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return err
	}
	ch, err := LoadChannel(r.Context(), h.DB, chID)
	if err != nil {
		return err
	}
	if ch.Type == ChannelTypeVoice {
		return httpx.ErrInvalidInput("voice channels have no text chat")
	}
	var req struct {
		Content   string     `json:"content"`
		ParentID  *uuid.UUID `json:"parent_id"`
		ReplyToID *uuid.UUID `json:"reply_to_id"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	content, err := ValidateContent(req.Content, false)
	if err != nil {
		return err
	}
	if err := ValidateParent(r.Context(), h.DB, chID, req.ParentID); err != nil {
		return err
	}
	if err := ValidateReplyTarget(r.Context(), h.DB, chID, req.ParentID, req.ReplyToID); err != nil {
		return err
	}
	id, err := CreateMessage(r.Context(), h.DB, NewMessage{
		ChannelID: chID, UserID: user.ID, Content: content, ParentID: req.ParentID, ReplyToID: req.ReplyToID,
	})
	if err != nil {
		return err
	}
	msg, err := GetMessage(r.Context(), h.DB, id)
	if err != nil {
		return err
	}
	h.Events.Broadcast("message_create", msg)
	httpx.WriteJSON(w, http.StatusCreated, msg)
	return nil
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

func (h *Handler) editMessage(w http.ResponseWriter, r *http.Request) error {
	msgID, _, err := h.messageInChannel(r)
	if err != nil {
		return err
	}
	var req struct {
		Content string `json:"content"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	content, err := ValidateContent(req.Content, false)
	if err != nil {
		return err
	}
	if err := EditMessage(r.Context(), h.DB, msgID, auth.UserFrom(r.Context()).ID, content); err != nil {
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

func (h *Handler) toggleReaction(w http.ResponseWriter, r *http.Request) error {
	user := auth.UserFrom(r.Context())
	msgID, err := httpx.PathUUID(r, "messageID")
	if err != nil {
		return err
	}
	if _, err := loadMessageRef(r.Context(), h.DB, msgID); err != nil {
		return err
	}
	var req struct {
		Emoji string `json:"emoji"`
	}
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
	payload := map[string]any{"message_id": msgID, "reactions": reactions}
	h.Events.Broadcast("message_reaction", payload)
	httpx.WriteJSON(w, http.StatusOK, payload)
	return nil
}

func (h *Handler) getThread(w http.ResponseWriter, r *http.Request) error {
	msgID, err := httpx.PathUUID(r, "messageID")
	if err != nil {
		return err
	}
	if _, err := loadMessageRef(r.Context(), h.DB, msgID); err != nil {
		return err
	}
	root, err := GetMessage(r.Context(), h.DB, msgID)
	if err != nil {
		return err
	}
	replies, err := GetThreadReplies(r.Context(), h.DB, msgID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"root": root, "replies": replies})
	return nil
}

func (h *Handler) createCategory(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Name      string `json:"name"`
		SortOrder int    `json:"sort_order"`
	}
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

func (h *Handler) createChannel(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		CategoryID *uuid.UUID  `json:"category_id"`
		Name       string      `json:"name"`
		Type       ChannelType `json:"type"`
		Topic      string      `json:"topic"`
		SortOrder  int         `json:"sort_order"`
	}
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

func (h *Handler) deleteChannel(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	keys, err := DeleteChannel(r.Context(), h.DB, id)
	if err != nil {
		return err
	}
	h.deleteObjects(r.Context(), keys)
	h.Events.Broadcast("channels_changed", nil)
	w.WriteHeader(http.StatusNoContent)
	return nil
}
