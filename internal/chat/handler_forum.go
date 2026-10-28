package chat

import (
	"context"
	"net/http"
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
	canManage, err := h.canManageMessages(r, user, chID)
	if err != nil {
		return err
	}
	ctx, before, err := h.postEditContext(r, chID, msgID, canManage, req)
	if err != nil {
		return err
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

// postEditContext is the context a post edit runs under, and the post as it was
// for the audit entry of a moderator's edit. An author who may no longer write
// here (timeout, lost Send messages) does not get to rename or retag the post
// either.
func (h *Handler) postEditContext(r *http.Request, chID, msgID uuid.UUID, canManage bool, req UpdatePostRequest) (ctx context.Context, before *Message, err error) {
	ctx = r.Context()
	if !canManage && (req.Title != nil || req.TagIDs != nil) {
		if ctx, err = h.sendContext(r, chID, perm.SendMessages); err != nil {
			return nil, nil, err
		}
	}
	if canManage && h.Audit != nil {
		before, _ = GetMessage(ctx, h.DB, msgID) // detail only: a miss leaves the entry without before values
	}
	return ctx, before, nil
}

// canManageMessages says whether user holds Manage messages in the channel (the
// Owner does when there is no permission service).
func (h *Handler) canManageMessages(r *http.Request, user *auth.User, chID uuid.UUID) (bool, error) {
	if h.Perms == nil {
		return user.Role == "admin", nil
	}
	p, err := h.Perms.Channel(r.Context(), user.Subject(), chID)
	if err != nil {
		return false, err
	}
	return p.Has(perm.ManageMessages), nil
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
