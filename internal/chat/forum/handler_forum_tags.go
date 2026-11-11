package forum

import (
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/automod"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// ServeListForumTags handles GET /api/channels/{channelID}/tags.
//
// @Summary List forum tags
// @Description The tags of a forum channel in the forum's own order. Requires View channel.
// @ID listForumTags
// @Tags Channels
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Success 200 {array} core.ForumTag "Tags."
// @Failure 401 {object} httpx.ErrorResponse "UNAUTHORIZED"
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN"
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND"
// @Router /api/channels/{channelID}/tags [get]
func (a *API) ServeListForumTags(w http.ResponseWriter, r *http.Request) error {
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
	tags, err := ListForumTags(r.Context(), a.DB, chID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, tags)
	return nil
}

// ServeCreateForumTag handles POST /api/channels/{channelID}/tags.
//
// @Summary Create a forum tag
// @Description Adds a new tag at the end of the forum channel's tag order (up to 20). Requires Manage channels.
// @ID createForumTag
// @Tags Admin
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param request body forum.CreateForumTagRequest true "Request body."
// @Success 201 {object} core.ForumTag "Created tag."
// @Failure 400 {object} httpx.ErrorResponse "INVALID_INPUT"
// @Failure 401 {object} httpx.ErrorResponse "UNAUTHORIZED"
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN"
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND"
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT"
// @Router /api/channels/{channelID}/tags [post]
func (a *API) ServeCreateForumTag(w http.ResponseWriter, r *http.Request) error {
	chID, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return err
	}
	if err := a.Guard.Require(r, chID, perm.ManageChannels); err != nil {
		return err
	}
	if _, err := a.requireForumChannel(r, chID); err != nil {
		return err
	}
	var req CreateForumTagRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	pending, err := a.screenTag(r, chID, req.Name, req.Emoji)
	if err != nil {
		return err
	}
	tag, err := CreateForumTagSpec(r.Context(), a.DB, chID, TagSpec(req))
	if err != nil {
		return err
	}
	pending.Commit(r.Context(), uuid.Nil)
	a.auditForumTag(r, "forum.tag_create", chID, tag, nil)
	a.Events.BroadcastChannel(chID, events.ForumTagsChanged, map[string]any{"channel_id": chID})
	httpx.WriteJSON(w, http.StatusCreated, tag)
	return nil
}

// ServeUpdateForumTag handles PATCH /api/channels/{channelID}/tags/{tagID}.
//
// @Summary Update a forum tag
// @Description Updates a tag's name, emoji, colour or moderated flag (only the fields sent change; an empty colour or emoji clears it). Requires Manage channels.
// @ID updateForumTag
// @Tags Admin
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param tagID path string true "Tag ID." Format(uuid)
// @Param request body forum.UpdateForumTagRequest true "Request body."
// @Success 200 {object} core.ForumTag "Updated tag."
// @Failure 400 {object} httpx.ErrorResponse "INVALID_INPUT"
// @Failure 401 {object} httpx.ErrorResponse "UNAUTHORIZED"
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN"
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND"
// @Router /api/channels/{channelID}/tags/{tagID} [patch]
func (a *API) ServeUpdateForumTag(w http.ResponseWriter, r *http.Request) error {
	chID, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return err
	}
	tagID, err := httpx.PathUUID(r, "tagID")
	if err != nil {
		return err
	}
	if err := a.Guard.Require(r, chID, perm.ManageChannels); err != nil {
		return err
	}
	if _, err := a.requireForumChannel(r, chID); err != nil {
		return err
	}
	var req UpdateForumTagRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	pending, err := a.screenTag(r, chID, messages.Deref(req.Name), messages.Deref(req.Emoji))
	if err != nil {
		return err
	}
	tag, before, err := UpdateForumTag(r.Context(), a.DB, chID, tagID, TagPatch(req))
	if err != nil {
		return err
	}
	pending.Commit(r.Context(), uuid.Nil)
	a.auditForumTag(r, "forum.tag_update", chID, tag, before)
	a.Events.BroadcastChannel(chID, events.ForumTagsChanged, map[string]any{"channel_id": chID})
	httpx.WriteJSON(w, http.StatusOK, tag)
	return nil
}

// ServeDeleteForumTag handles DELETE /api/channels/{channelID}/tags/{tagID}.
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
func (a *API) ServeDeleteForumTag(w http.ResponseWriter, r *http.Request) error {
	chID, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return err
	}
	tagID, err := httpx.PathUUID(r, "tagID")
	if err != nil {
		return err
	}
	if err := a.Guard.Require(r, chID, perm.ManageChannels); err != nil {
		return err
	}
	if _, err := a.requireForumChannel(r, chID); err != nil {
		return err
	}
	tag, err := DeleteForumTag(r.Context(), a.DB, chID, tagID)
	if err != nil {
		return err
	}
	a.auditForumTag(r, "forum.tag_delete", chID, tag, nil)
	a.Events.BroadcastChannel(chID, events.ForumTagsChanged, map[string]any{"channel_id": chID})
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// screenTag applies the automod rules to a tag's name and emoji; a tag is text
// every member of the forum reads.
// A flag rule's hits are returned pending: the caller commits them once the
// text is stored.
func (a *API) screenTag(r *http.Request, chID uuid.UUID, texts ...string) (*automod.Pending, error) {
	return messages.ScreenText(r.Context(), a.DB, chID, auth.UserFrom(r.Context()).ID, uuid.Nil, strings.Join(texts, "\n"))
}

// auditForumTag records a tag change; before is the tag as it was (updates only).
func (a *API) auditForumTag(r *http.Request, action string, chID uuid.UUID, tag, before *core.ForumTag) {
	details := map[string]any{"channel_id": chID, "name": tag.Name, "emoji": tag.Emoji, "color": tag.Color, "moderated": tag.Moderated}
	if before != nil {
		details["previous_name"] = before.Name
		details["previous_emoji"] = before.Emoji
		details["previous_color"] = before.Color
		details["previous_moderated"] = before.Moderated
	}
	a.Audit.RecordOrLog(r.Context(), audit.Entry{
		Action: action, TargetType: "forum_tag", TargetID: tag.ID.String(), TargetName: tag.Name,
		ChannelID: &chID, Details: details,
	})
}
