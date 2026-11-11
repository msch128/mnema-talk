package forum

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/automod"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// UpdateForumSettingsRequest is the body of PATCH /api/channels/{channelID}/forum-settings.
type UpdateForumSettingsRequest struct {
	RequireTag *bool   `json:"require_tag,omitempty" binding:"optional"`
	Guidelines *string `json:"guidelines,omitempty" binding:"optional" maxLength:"1000"`
}

// ReorderForumTagsRequest is the body of PUT /api/channels/{channelID}/tags/order.
type ReorderForumTagsRequest struct {
	// TagIDs lists every tag of the forum once, in the new order.
	TagIDs []uuid.UUID `json:"tag_ids" format:"uuid"`
}

// UnreadForumPosts is the posts of a forum with something the caller has not read.
type UnreadForumPosts struct {
	PostIDs []uuid.UUID `json:"post_ids" format:"uuid"`
}

// PostRead is the answer to opening a post: where the caller had read to.
type PostRead struct {
	// PreviousReadAt is the read marker before this call (the day the caller
	// joined for a post never opened). Replies of others after it are unread.
	PreviousReadAt time.Time `json:"previous_read_at" format:"date-time"`
}

func (a *API) MountExtras(r chi.Router) {
	r.Get("/channels/{channelID}/forum-settings", httpx.Handle(a.ServeGetForumSettings))
	r.Patch("/channels/{channelID}/forum-settings", httpx.Handle(a.ServeUpdateForumSettings))
	r.Put("/channels/{channelID}/tags/order", httpx.Handle(a.ServeReorderForumTags))
	r.Get("/channels/{channelID}/posts/unread", httpx.Handle(a.ListUnreadForumPosts))
	r.Put("/channels/{channelID}/posts/{messageID}/read", httpx.Handle(a.MarkForumPostRead))
}

// ServeGetForumSettings handles GET /api/channels/{channelID}/forum-settings.
//
// @Summary Get a forum's settings
// @Description Whether posts need a tag and the guidelines shown above the new-post form. Requires View channel.
// @ID getForumSettings
// @Tags Channels
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Success 200 {object} forum.ForumSettings "Settings."
// @Failure 401 {object} httpx.ErrorResponse "UNAUTHORIZED"
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN"
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND"
// @Router /api/channels/{channelID}/forum-settings [get]
func (a *API) ServeGetForumSettings(w http.ResponseWriter, r *http.Request) error {
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
	s, err := GetForumSettings(r.Context(), a.DB, chID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, s)
	return nil
}

// ServeUpdateForumSettings handles PATCH /api/channels/{channelID}/forum-settings.
//
// @Summary Update a forum's settings
// @Description Changes the fields sent: whether posts need a tag, and the guidelines text (at most 1000 characters; empty clears it). Requires Manage channels.
// @ID updateForumSettings
// @Tags Admin
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param request body forum.UpdateForumSettingsRequest true "Request body."
// @Success 200 {object} forum.ForumSettings "Updated settings."
// @Failure 400 {object} httpx.ErrorResponse "INVALID_INPUT"
// @Failure 401 {object} httpx.ErrorResponse "UNAUTHORIZED"
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN"
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND"
// @Router /api/channels/{channelID}/forum-settings [patch]
func (a *API) ServeUpdateForumSettings(w http.ResponseWriter, r *http.Request) error {
	chID, err := a.forumManageTarget(r)
	if err != nil {
		return err
	}
	var req UpdateForumSettingsRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	var pending *automod.Pending
	if req.Guidelines != nil {
		if pending, err = a.screenTag(r, chID, *req.Guidelines); err != nil {
			return err
		}
	}
	s, before, err := UpdateForumSettings(r.Context(), a.DB, chID, ForumSettingsPatch(req))
	if err != nil {
		return err
	}
	pending.Commit(r.Context(), uuid.Nil)
	a.Audit.RecordOrLog(r.Context(), audit.Entry{
		Action: "forum.settings_update", TargetType: "channel", TargetID: chID.String(), ChannelID: &chID,
		Details: map[string]any{
			"channel_id": chID, "require_tag": s.RequireTag, "previous_require_tag": before.RequireTag,
			"guidelines_changed": s.Guidelines != before.Guidelines,
		},
	})
	a.Events.BroadcastChannel(chID, events.Untyped(events.ForumTagsChanged, map[string]any{"channel_id": chID}))
	httpx.WriteJSON(w, http.StatusOK, s)
	return nil
}

// forumManageTarget is the forum of a request that needs Manage channels.
func (a *API) forumManageTarget(r *http.Request) (uuid.UUID, error) {
	chID, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return uuid.Nil, err
	}
	if err := a.Guard.Require(r, chID, perm.ManageChannels); err != nil {
		return uuid.Nil, err
	}
	if _, err := a.requireForumChannel(r, chID); err != nil {
		return uuid.Nil, err
	}
	return chID, nil
}

// ServeReorderForumTags handles PUT /api/channels/{channelID}/tags/order.
//
// @Summary Reorder a forum's tags
// @Description Sets the order of the tags. The body names every tag of the forum exactly once. Requires Manage channels.
// @ID reorderForumTags
// @Tags Admin
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param request body forum.ReorderForumTagsRequest true "Request body."
// @Success 200 {array} core.ForumTag "The tags in their new order."
// @Failure 400 {object} httpx.ErrorResponse "INVALID_INPUT"
// @Failure 401 {object} httpx.ErrorResponse "UNAUTHORIZED"
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN"
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND"
// @Router /api/channels/{channelID}/tags/order [put]
func (a *API) ServeReorderForumTags(w http.ResponseWriter, r *http.Request) error {
	chID, err := a.forumManageTarget(r)
	if err != nil {
		return err
	}
	var req ReorderForumTagsRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	tags, err := ReorderForumTags(r.Context(), a.DB, chID, req.TagIDs)
	if err != nil {
		return err
	}
	a.Audit.RecordOrLog(r.Context(), audit.Entry{
		Action: "forum.tag_reorder", TargetType: "channel", TargetID: chID.String(), ChannelID: &chID,
		Details: map[string]any{"channel_id": chID, "tag_ids": req.TagIDs},
	})
	a.Events.BroadcastChannel(chID, events.Untyped(events.ForumTagsChanged, map[string]any{"channel_id": chID}))
	httpx.WriteJSON(w, http.StatusOK, tags)
	return nil
}

// ListUnreadForumPosts handles GET /api/channels/{channelID}/posts/unread.
//
// @Summary List a forum's unread posts
// @Description The posts (up to 200, newest activity first) with a post or reply of someone else the caller has not read since they last opened the post. Requires View channel.
// @ID listUnreadForumPosts
// @Tags Channels
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Success 200 {object} forum.UnreadForumPosts "Unread posts."
// @Failure 401 {object} httpx.ErrorResponse "UNAUTHORIZED"
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN"
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND"
// @Router /api/channels/{channelID}/posts/unread [get]
func (a *API) ListUnreadForumPosts(w http.ResponseWriter, r *http.Request) error {
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
	ids, err := UnreadPostIDs(r.Context(), a.DB, chID, auth.UserFrom(r.Context()).ID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, UnreadForumPosts{PostIDs: ids})
	return nil
}

// MarkForumPostRead handles PUT /api/channels/{channelID}/posts/{messageID}/read.
//
// @Summary Mark a forum post read
// @Description Sets the caller's read marker of the post to now and returns the marker it had, which is where the first unread reply starts. Never moves backwards. Requires View channel.
// @ID markForumPostRead
// @Tags Channels
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param messageID path string true "Post ID." Format(uuid)
// @Success 200 {object} forum.PostRead "The marker before."
// @Failure 401 {object} httpx.ErrorResponse "UNAUTHORIZED"
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN"
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND"
// @Router /api/channels/{channelID}/posts/{messageID}/read [put]
func (a *API) MarkForumPostRead(w http.ResponseWriter, r *http.Request) error {
	chID, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return err
	}
	postID, err := httpx.PathUUID(r, "messageID")
	if err != nil {
		return err
	}
	if err := a.Guard.Require(r, chID, perm.ViewChannel); err != nil {
		return core.NotFoundUnlessFailure(err)
	}
	if _, err := a.requireForumChannel(r, chID); err != nil {
		return err
	}
	if _, _, err := getForumPostAuthor(r.Context(), a.DB, chID, postID, false); err != nil {
		return err
	}
	user := auth.UserFrom(r.Context())
	previous, err := MarkPostRead(r.Context(), a.DB, user.ID, postID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, PostRead{PreviousReadAt: previous})
	return nil
}
