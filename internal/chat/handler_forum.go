package chat

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// CreateForumTagRequest is the body of POST /api/channels/{channelID}/tags.
type CreateForumTagRequest struct {
	Name  string `json:"name" minLength:"1" maxLength:"20"`
	Emoji string `json:"emoji,omitempty" binding:"optional" maxLength:"32"`
}

// UpdateForumTagRequest is the body of PATCH /api/channels/{channelID}/tags/{tagID}.
type UpdateForumTagRequest struct {
	Name  *string `json:"name,omitempty" binding:"optional" minLength:"1" maxLength:"20"`
	Emoji *string `json:"emoji,omitempty" binding:"optional" maxLength:"32"`
}

// CreatePostRequest is the body of POST /api/channels/{channelID}/posts.
type CreatePostRequest struct {
	Title   string      `json:"title" minLength:"1" maxLength:"100"`
	Content string      `json:"content" minLength:"1" maxLength:"4000"`
	TagIDs  []uuid.UUID `json:"tag_ids,omitempty" binding:"optional"`
}

// UpdatePostRequest is the body of PATCH /api/channels/{channelID}/posts/{messageID}.
type UpdatePostRequest struct {
	Title    *string      `json:"title,omitempty" binding:"optional" minLength:"1" maxLength:"100"`
	TagIDs   *[]uuid.UUID `json:"tag_ids,omitempty" binding:"optional"`
	IsLocked *bool        `json:"is_locked,omitempty" binding:"optional"`
}

func (h *Handler) mountForum(r chi.Router) {
	r.Get("/channels/{channelID}/tags", httpx.Handle(h.listForumTags))
	r.Post("/channels/{channelID}/tags", httpx.Handle(h.createForumTag))
	r.Patch("/channels/{channelID}/tags/{tagID}", httpx.Handle(h.updateForumTag))
	r.Delete("/channels/{channelID}/tags/{tagID}", httpx.Handle(h.deleteForumTag))
	r.Get("/channels/{channelID}/posts", httpx.Handle(h.listForumPosts))
	r.Post("/channels/{channelID}/posts", httpx.Handle(h.createForumPost))
	r.Patch("/channels/{channelID}/posts/{messageID}", httpx.Handle(h.updateForumPost))
}

func (h *Handler) requireForumChannel(r *http.Request, chID uuid.UUID) (*ChannelInfo, error) {
	ch, err := LoadChannel(r.Context(), h.DB, chID)
	if err != nil {
		return nil, err
	}
	if ch.Type != ChannelTypeForum {
		return nil, httpx.ErrInvalidInput("channel is not a forum channel")
	}
	return ch, nil
}

// listForumTags handles GET /api/channels/{channelID}/tags.
//
// @Summary List forum tags
// @Description Admin-defined tags in a forum channel. Requires View channel.
// @ID listForumTags
// @Tags Channels
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Success 200 {array} ForumTag "Tags."
// @Failure 401 {object} httpx.ErrorResponse "UNAUTHORIZED"
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN"
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND"
// @Router /api/channels/{channelID}/tags [get]
func (h *Handler) listForumTags(w http.ResponseWriter, r *http.Request) error {
	chID, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return err
	}
	if err := h.require(r, chID, perm.ViewChannel); err != nil {
		return notFoundUnlessFailure(err)
	}
	if _, err := h.requireForumChannel(r, chID); err != nil {
		return err
	}
	tags, err := ListForumTags(r.Context(), h.DB, chID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, tags)
	return nil
}

// createForumTag handles POST /api/channels/{channelID}/tags.
//
// @Summary Create a forum tag
// @Description Adds a new tag to the forum channel (up to 20). Requires Manage channels.
// @ID createForumTag
// @Tags Admin
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param request body CreateForumTagRequest true "Request body."
// @Success 201 {object} ForumTag "Created tag."
// @Failure 400 {object} httpx.ErrorResponse "INVALID_INPUT"
// @Failure 401 {object} httpx.ErrorResponse "UNAUTHORIZED"
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN"
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND"
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT"
// @Router /api/channels/{channelID}/tags [post]
func (h *Handler) createForumTag(w http.ResponseWriter, r *http.Request) error {
	chID, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return err
	}
	if err := h.require(r, chID, perm.ManageChannels); err != nil {
		return err
	}
	if _, err := h.requireForumChannel(r, chID); err != nil {
		return err
	}
	var req CreateForumTagRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if err := h.screenTag(r, chID, req.Name, req.Emoji); err != nil {
		return err
	}
	tag, err := CreateForumTag(r.Context(), h.DB, chID, req.Name, req.Emoji)
	if err != nil {
		return err
	}
	h.auditForumTag(r, "forum.tag_create", chID, tag, nil)
	h.Events.BroadcastChannel(chID, events.ForumTagsChanged, map[string]any{"channel_id": chID})
	httpx.WriteJSON(w, http.StatusCreated, tag)
	return nil
}

// updateForumTag handles PATCH /api/channels/{channelID}/tags/{tagID}.
//
// @Summary Update a forum tag
// @Description Updates tag name or emoji. Requires Manage channels.
// @ID updateForumTag
// @Tags Admin
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param tagID path string true "Tag ID." Format(uuid)
// @Param request body UpdateForumTagRequest true "Request body."
// @Success 200 {object} ForumTag "Updated tag."
// @Failure 400 {object} httpx.ErrorResponse "INVALID_INPUT"
// @Failure 401 {object} httpx.ErrorResponse "UNAUTHORIZED"
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN"
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND"
// @Router /api/channels/{channelID}/tags/{tagID} [patch]
func (h *Handler) updateForumTag(w http.ResponseWriter, r *http.Request) error {
	chID, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return err
	}
	tagID, err := httpx.PathUUID(r, "tagID")
	if err != nil {
		return err
	}
	if err := h.require(r, chID, perm.ManageChannels); err != nil {
		return err
	}
	if _, err := h.requireForumChannel(r, chID); err != nil {
		return err
	}
	var req UpdateForumTagRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if err := h.screenTag(r, chID, deref(req.Name), deref(req.Emoji)); err != nil {
		return err
	}
	tag, before, err := UpdateForumTag(r.Context(), h.DB, chID, tagID, req.Name, req.Emoji)
	if err != nil {
		return err
	}
	h.auditForumTag(r, "forum.tag_update", chID, tag, before)
	h.Events.BroadcastChannel(chID, events.ForumTagsChanged, map[string]any{"channel_id": chID})
	httpx.WriteJSON(w, http.StatusOK, tag)
	return nil
}

// deleteForumTag handles DELETE /api/channels/{channelID}/tags/{tagID}.
//
// @Summary Delete a forum tag
// @Description Removes a tag from the forum channel. Requires Manage channels.
// @ID deleteForumTag
// @Tags Admin
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param tagID path string true "Tag ID." Format(uuid)
// @Success 204 "Success, no content."
// @Failure 401 {object} httpx.ErrorResponse "UNAUTHORIZED"
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN"
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND"
// @Router /api/channels/{channelID}/tags/{tagID} [delete]
func (h *Handler) deleteForumTag(w http.ResponseWriter, r *http.Request) error {
	chID, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return err
	}
	tagID, err := httpx.PathUUID(r, "tagID")
	if err != nil {
		return err
	}
	if err := h.require(r, chID, perm.ManageChannels); err != nil {
		return err
	}
	if _, err := h.requireForumChannel(r, chID); err != nil {
		return err
	}
	tag, err := DeleteForumTag(r.Context(), h.DB, chID, tagID)
	if err != nil {
		return err
	}
	h.auditForumTag(r, "forum.tag_delete", chID, tag, nil)
	h.Events.BroadcastChannel(chID, events.ForumTagsChanged, map[string]any{"channel_id": chID})
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// listForumPosts handles GET /api/channels/{channelID}/posts.
//
// @Summary List forum posts
// @Description Root posts in a forum channel, ordered by latest activity or creation. Supports tag filtering and pagination. Requires View channel.
// @ID listForumPosts
// @Tags Channels
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param sort query string false "Sort order: 'latest_activity' (default; 'activity' is accepted as an alias) or 'newest' ('creation' is accepted as an alias). Other values are rejected." Enums(latest_activity, newest)
// @Param tag_id query string false "Filter by tag ID." Format(uuid)
// @Param limit query int false "Page size." default(50)
// @Param before query string false "Posts older than this post." Format(uuid)
// @Param before_activity query string false "With latest_activity order and before: the last_activity_at the client saw for the before post. Paging then continues exactly behind that position even when the post has got or lost a reply since." Format(date-time)
// @Success 200 {array} Message "Posts."
// @Failure 400 {object} httpx.ErrorResponse "INVALID_INPUT"
// @Failure 401 {object} httpx.ErrorResponse "UNAUTHORIZED"
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN"
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the channel, or the before post (deleted since the previous page), does not exist."
// @Router /api/channels/{channelID}/posts [get]
func (h *Handler) listForumPosts(w http.ResponseWriter, r *http.Request) error {
	chID, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return err
	}
	if err := h.require(r, chID, perm.ViewChannel); err != nil {
		return notFoundUnlessFailure(err)
	}
	if _, err := h.requireForumChannel(r, chID); err != nil {
		return err
	}
	sortMode := r.URL.Query().Get("sort")
	var tagID *uuid.UUID
	if rawTag := r.URL.Query().Get("tag_id"); rawTag != "" {
		id, err := uuid.Parse(rawTag)
		if err != nil {
			return httpx.ErrInvalidInput("tag_id must be a valid UUID")
		}
		tagID = &id
	}
	q := HistoryQuery{Limit: httpx.QueryLimit(r, "limit", 50, 100)}
	if rawBefore := r.URL.Query().Get("before"); rawBefore != "" {
		id, err := uuid.Parse(rawBefore)
		if err != nil {
			return httpx.ErrInvalidInput("before must be a valid UUID")
		}
		q.Before = &id
	}
	if rawActivity := r.URL.Query().Get("before_activity"); rawActivity != "" {
		at, err := time.Parse(time.RFC3339Nano, rawActivity)
		if err != nil {
			return httpx.ErrInvalidInput("before_activity must be an RFC 3339 timestamp")
		}
		q.BeforeActivity = &at
	}
	posts, err := ListForumPosts(r.Context(), h.DB, chID, sortMode, tagID, q)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, posts)
	return nil
}

// createForumPost handles POST /api/channels/{channelID}/posts.
//
// @Summary Create a forum post
// @Description Creates a new post (thread root) with title, content and optional tags. Requires Send messages.
// @ID createForumPost
// @Tags Messages
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param request body CreatePostRequest true "Request body."
// @Success 201 {object} Message "Created post."
// @Failure 400 {object} httpx.ErrorResponse "INVALID_INPUT"
// @Failure 401 {object} httpx.ErrorResponse "UNAUTHORIZED"
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN"
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND"
// @Router /api/channels/{channelID}/posts [post]
func (h *Handler) createForumPost(w http.ResponseWriter, r *http.Request) error {
	user := auth.UserFrom(r.Context())
	chID, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return err
	}
	ctx, err := h.sendContext(r, chID, perm.SendMessages)
	if err != nil {
		return err
	}
	if _, err := h.requireForumChannel(r, chID); err != nil {
		return err
	}
	var req CreatePostRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	postID, err := CreateForumPost(ctx, h.DB, h.Online, chID, user.ID, req.Title, req.Content, req.TagIDs)
	if err != nil {
		return err
	}
	return PublishMessage(w, r, h.DB, h.Events, postID)
}

// updateForumPost handles PATCH /api/channels/{channelID}/posts/{messageID}.
//
// @Summary Update a forum post
// @Description Updates a post's title, tags or lock status. Author or Manage messages for title/tags; Manage messages for lock status.
// @ID updateForumPost
// @Tags Messages
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param messageID path string true "Message ID." Format(uuid)
// @Param request body UpdatePostRequest true "Request body."
// @Success 200 {object} Message "Updated post."
// @Failure 400 {object} httpx.ErrorResponse "INVALID_INPUT"
// @Failure 401 {object} httpx.ErrorResponse "UNAUTHORIZED"
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN"
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND"
// @Router /api/channels/{channelID}/posts/{messageID} [patch]
func (h *Handler) updateForumPost(w http.ResponseWriter, r *http.Request) error {
	user := auth.UserFrom(r.Context())
	chID, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return err
	}
	msgID, err := httpx.PathUUID(r, "messageID")
	if err != nil {
		return err
	}
	if err := h.require(r, chID, perm.ViewChannel); err != nil {
		return notFoundUnlessFailure(err)
	}
	if _, err := h.requireForumChannel(r, chID); err != nil {
		return err
	}
	var req UpdatePostRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	canManage := false
	if h.Perms != nil {
		p, err := h.Perms.Channel(r.Context(), user.Subject(), chID)
		if err != nil {
			return err
		}
		canManage = p.Has(perm.ManageMessages)
	} else if user.Role == "admin" {
		canManage = true
	}
	ctx := r.Context()
	if !canManage && (req.Title != nil || req.TagIDs != nil) {
		// An author who may no longer write here (timeout, lost Send messages)
		// does not get to rename or retag the post either.
		if ctx, err = h.sendContext(r, chID, perm.SendMessages); err != nil {
			return err
		}
	}
	var before *Message
	if canManage && h.Audit != nil {
		before, _ = GetMessage(ctx, h.DB, msgID) // detail only: a miss leaves the entry without before values
	}
	msg, err := UpdateForumPost(ctx, h.DB, chID, msgID, user.ID, canManage, req.Title, req.TagIDs, req.IsLocked)
	if err != nil {
		return err
	}
	if canManage && msg.UserID != user.ID {
		h.auditForumPost(ctx, chID, msg, before, req)
	}
	h.Events.BroadcastChannel(chID, events.MessageUpdate, msg)
	httpx.WriteJSON(w, http.StatusOK, msg)
	return nil
}

// screenTag applies the automod rules to a tag's name and emoji; a tag is text
// every member of the forum reads.
func (h *Handler) screenTag(r *http.Request, chID uuid.UUID, texts ...string) error {
	_, err := screenText(r.Context(), h.DB, chID, auth.UserFrom(r.Context()).ID, uuid.Nil, strings.Join(texts, "\n"))
	return err
}

// auditForumTag records a tag change; before is the tag as it was (updates only).
func (h *Handler) auditForumTag(r *http.Request, action string, chID uuid.UUID, tag, before *ForumTag) {
	details := map[string]any{"channel_id": chID, "name": tag.Name, "emoji": tag.Emoji}
	if before != nil {
		details["previous_name"] = before.Name
		details["previous_emoji"] = before.Emoji
	}
	h.Audit.RecordOrLog(r.Context(), audit.Entry{
		Action: action, TargetType: "forum_tag", TargetID: tag.ID.String(), TargetName: tag.Name,
		ChannelID: &chID, Details: details,
	})
}

// auditForumPost records a moderator's change to another member's post: the
// fields the request set, with the values before and after.
func (h *Handler) auditForumPost(ctx context.Context, chID uuid.UUID, msg, before *Message, req UpdatePostRequest) {
	details := map[string]any{"channel_id": chID, "author_id": msg.UserID, "author_name": msg.Username}
	if req.Title != nil {
		details["title"] = msg.Title
		if before != nil {
			details["previous_title"] = before.Title
		}
	}
	if req.IsLocked != nil {
		details["is_locked"] = msg.IsLocked
		if before != nil {
			details["previous_is_locked"] = before.IsLocked
		}
	}
	if req.TagIDs != nil {
		details["tag_ids"] = *req.TagIDs
	}
	h.Audit.RecordOrLog(ctx, audit.Entry{
		Action: "forum.post_update", TargetType: "message", TargetID: msg.ID.String(), ChannelID: &chID, Details: details,
	})
}
