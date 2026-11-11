package forum

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// CreateForumTagRequest is the body of POST /api/channels/{channelID}/tags.
type CreateForumTagRequest struct {
	Name  string `json:"name" minLength:"1" maxLength:"20"`
	Emoji string `json:"emoji,omitempty" binding:"optional" maxLength:"32"`
	// Color is a palette name (gray, red, orange, yellow, green, teal, blue, purple, pink); omitted means none.
	Color string `json:"color,omitempty" binding:"optional" enums:"gray,red,orange,yellow,green,teal,blue,purple,pink"`
	// Moderated: only members with Manage messages apply or remove the tag.
	Moderated bool `json:"moderated,omitempty" binding:"optional"`
}

// UpdateForumTagRequest is the body of PATCH /api/channels/{channelID}/tags/{tagID}.
type UpdateForumTagRequest struct {
	Name  *string `json:"name,omitempty" binding:"optional" minLength:"1" maxLength:"20"`
	Emoji *string `json:"emoji,omitempty" binding:"optional" maxLength:"32"`
	// Color is a palette name; an empty string clears it.
	Color     *string `json:"color,omitempty" binding:"optional" enums:",gray,red,orange,yellow,green,teal,blue,purple,pink"`
	Moderated *bool   `json:"moderated,omitempty" binding:"optional"`
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
	// IsResolved marks the post as solved (author or Manage messages) or reopens it.
	IsResolved *bool `json:"is_resolved,omitempty" binding:"optional"`
}

func (a *API) Mount(r chi.Router) {
	r.Get("/channels/{channelID}/tags", httpx.Handle(a.ServeListForumTags))
	r.Post("/channels/{channelID}/tags", httpx.Handle(a.ServeCreateForumTag))
	r.Patch("/channels/{channelID}/tags/{tagID}", httpx.Handle(a.ServeUpdateForumTag))
	r.Delete("/channels/{channelID}/tags/{tagID}", httpx.Handle(a.ServeDeleteForumTag))
	r.Get("/channels/{channelID}/posts", httpx.Handle(a.ServeListForumPosts))
	r.Post("/channels/{channelID}/posts", httpx.Handle(a.ServeCreateForumPost))
	r.Patch("/channels/{channelID}/posts/{messageID}", httpx.Handle(a.ServeUpdateForumPost))
	a.MountExtras(r)
}

func (a *API) requireForumChannel(r *http.Request, chID uuid.UUID) (*core.ChannelInfo, error) {
	ch, err := core.LoadChannel(r.Context(), a.DB, chID)
	if err != nil {
		return nil, err
	}
	if ch.Type != core.ChannelTypeForum {
		return nil, httpx.ErrInvalidInput("channel is not a forum channel")
	}
	return ch, nil
}

// ServeListForumPosts handles GET /api/channels/{channelID}/posts.
//
// @Summary List forum posts
// @Description Root posts in a forum channel, ordered by latest activity or creation. Supports tag filtering, text search and pagination. Pinned posts come first and only on the first page (no before); the pages behind it hold unpinned posts. Requires View channel.
// @ID listForumPosts
// @Tags Channels
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param sort query string false "Sort order: 'latest_activity' (default; 'activity' is accepted as an alias) or 'newest' ('creation' is accepted as an alias). Other values are rejected." Enums(latest_activity, newest)
// @Param tag_id query string false "Filter by tag ID." Format(uuid)
// @Param q query string false "Keep posts whose title, first message or a reply contains this text (at most 100 characters)."
// @Param limit query int false "Page size." default(50)
// @Param before query string false "Posts older than this post." Format(uuid)
// @Param before_activity query string false "With latest_activity order and before: the last_activity_at the client saw for the before post. Paging then continues exactly behind that position even when the post has got or lost a reply since." Format(date-time)
// @Success 200 {array} core.Message "Posts."
// @Failure 400 {object} httpx.ErrorResponse "INVALID_INPUT"
// @Failure 401 {object} httpx.ErrorResponse "UNAUTHORIZED"
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN"
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the channel, or the before post (deleted since the previous page), does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests, or the search (q) took too long."
// @Router /api/channels/{channelID}/posts [get]
func (a *API) ServeListForumPosts(w http.ResponseWriter, r *http.Request) error {
	chID, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return err
	}
	if err := a.Guard.Require(r, chID, perm.ViewChannel); err != nil {
		return core.NotFoundUnlessFailure(err)
	}
	if _, err := a.requireForumChannel(r, chID); err != nil {
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
	q := core.HistoryQuery{Limit: httpx.QueryLimit(r, "limit", 50, 100)}
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
	posts, err := ListForumPostsFiltered(r.Context(), a.DB, chID, PostFilter{Sort: sortMode, TagID: tagID, Search: r.URL.Query().Get("q")}, q)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, posts)
	return nil
}

// ServeCreateForumPost handles POST /api/channels/{channelID}/posts.
//
// @Summary Create a forum post
// @Description Creates a new post (thread root) with title, content and optional tags. Requires Send messages.
// @ID createForumPost
// @Tags Messages
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param request body forum.CreatePostRequest true "Request body."
// @Success 201 {object} core.Message "Created post."
// @Failure 400 {object} httpx.ErrorResponse "INVALID_INPUT"
// @Failure 401 {object} httpx.ErrorResponse "UNAUTHORIZED"
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN"
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND"
// @Router /api/channels/{channelID}/posts [post]
func (a *API) ServeCreateForumPost(w http.ResponseWriter, r *http.Request) error {
	user := auth.UserFrom(r.Context())
	chID, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return err
	}
	ctx, err := a.Messages.ServeSendContext(r, chID, perm.SendMessages)
	if err != nil {
		return err
	}
	if _, err := a.requireForumChannel(r, chID); err != nil {
		return err
	}
	var req CreatePostRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	canManage, err := a.canManageMessages(r, user, chID)
	if err != nil {
		return err
	}
	postID, err := CreateForumPost(ctx, a.DB, a.Online, chID, user.ID, canManage, req.Title, req.Content, req.TagIDs)
	if err != nil {
		return err
	}
	return messages.PublishMessage(w, r, a.DB, a.Events, postID)
}

// ServeUpdateForumPost handles PATCH /api/channels/{channelID}/posts/{messageID}.
//
// @Summary Update a forum post
// @Description Updates a post's title, tags, solved mark or lock status. The author or Manage messages for title, tags and the solved mark (not on a locked post, unless Manage messages); Manage messages for lock status and for applying or removing moderated tags. A forum that requires a tag refuses a post without one.
// @ID updateForumPost
// @Tags Messages
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param messageID path string true "Message ID." Format(uuid)
// @Param request body forum.UpdatePostRequest true "Request body."
// @Success 200 {object} core.Message "Updated post."
// @Failure 400 {object} httpx.ErrorResponse "INVALID_INPUT"
// @Failure 401 {object} httpx.ErrorResponse "UNAUTHORIZED"
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN"
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND"
// @Router /api/channels/{channelID}/posts/{messageID} [patch]
func (a *API) ServeUpdateForumPost(w http.ResponseWriter, r *http.Request) error {
	user := auth.UserFrom(r.Context())
	chID, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return err
	}
	msgID, err := httpx.PathUUID(r, "messageID")
	if err != nil {
		return err
	}
	if err := a.Guard.Require(r, chID, perm.ViewChannel); err != nil {
		return core.NotFoundUnlessFailure(err)
	}
	if _, err := a.requireForumChannel(r, chID); err != nil {
		return err
	}
	var req UpdatePostRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	canManage, err := a.canManageMessages(r, user, chID)
	if err != nil {
		return err
	}
	ctx, before, err := a.postEditContext(r, chID, msgID, canManage, req)
	if err != nil {
		return err
	}
	msg, err := UpdateForumPost(ctx, a.DB, chID, msgID, PostActor{ID: user.ID, CanManage: canManage}, PostPatch(req))
	if err != nil {
		return err
	}
	if canManage && msg.UserID != user.ID {
		a.auditForumPost(ctx, chID, msg, before, req)
	}
	a.Events.BroadcastChannel(chID, events.Untyped(events.MessageUpdate, msg))
	httpx.WriteJSON(w, http.StatusOK, msg)
	return nil
}

// postEditContext is the context a post edit runs under, and the post as it was
// for the audit entry of a moderator's edit. An author who may no longer write
// here (timeout, lost Send messages) does not get to rename or retag the post
// either.
func (a *API) postEditContext(r *http.Request, chID, msgID uuid.UUID, canManage bool, req UpdatePostRequest) (ctx context.Context, before *core.Message, err error) {
	ctx = r.Context()
	if !canManage && (req.Title != nil || req.TagIDs != nil || req.IsResolved != nil) {
		if ctx, err = a.Messages.ServeSendContext(r, chID, perm.SendMessages); err != nil {
			return nil, nil, err
		}
	}
	if canManage && a.Audit != nil {
		before, _ = messages.GetMessage(ctx, a.DB, msgID) // detail only: a miss leaves the entry without before values
	}
	return ctx, before, nil
}

// canManageMessages says whether user holds Manage messages in the channel.
func (a *API) canManageMessages(r *http.Request, user *auth.User, chID uuid.UUID) (bool, error) {
	return CanManageMessages(r.Context(), a.Guard.Perms, user, chID)
}

// CanManageMessages is canManageMessages for callers outside the chat handler.
func CanManageMessages(ctx context.Context, perms *perm.Service, user *auth.User, chID uuid.UUID) (bool, error) {
	p, err := perms.Channel(ctx, user.Subject(), chID)
	if err != nil {
		return false, err
	}
	return p.Has(perm.ManageMessages), nil
}

// auditForumPost records a moderator's change to another member's post: the
// fields the request set, with the values before and after.
func (a *API) auditForumPost(ctx context.Context, chID uuid.UUID, msg, before *core.Message, req UpdatePostRequest) {
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
	if req.IsResolved != nil {
		details["is_resolved"] = msg.IsResolved
	}
	if req.TagIDs != nil {
		details["tag_ids"] = *req.TagIDs
		if before != nil {
			details["previous_tag_ids"] = tagIDsOf(before.Tags)
		}
	}
	a.Audit.RecordOrLog(ctx, audit.Entry{
		Action: "forum.post_update", TargetType: "message", TargetID: msg.ID.String(), ChannelID: &chID, Details: details,
	})
}

func tagIDsOf(tags []core.ForumTag) []uuid.UUID {
	ids := make([]uuid.UUID, len(tags))
	for i, t := range tags {
		ids[i] = t.ID
	}
	return ids
}
