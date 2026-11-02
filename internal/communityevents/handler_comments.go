package communityevents

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

func (h *Handler) mountComments(r chi.Router) {
	r.Get("/events/{id}/comments", httpx.Handle(h.listComments))
	r.Put("/events/{id}/comments/mute", httpx.Handle(h.muteComments))
	writes := r.With()
	if h.CommentLimit != nil {
		writes = r.With(h.CommentLimit.By(auth.UserKey))
	}
	writes.Post("/events/{id}/comments", httpx.Handle(h.createComment))
	writes.Patch("/events/{id}/comments/{commentID}", httpx.Handle(h.updateComment))
	r.Delete("/events/{id}/comments/{commentID}", httpx.Handle(h.deleteComment))
}

// broadcastComment tells everyone who may see the event that a comment changed.
func (h *Handler) broadcastComment(ev *CommunityEvent, p CommentPayload) {
	if h.Events == nil {
		return
	}
	if ev.ChannelID != nil {
		h.Events.BroadcastChannel(*ev.ChannelID, events.EventComment, p)
		return
	}
	h.Events.Broadcast(events.EventComment, p)
}

// canModerateComments says whether the caller may delete other members' comments.
func (h *Handler) canModerateComments(r *http.Request) bool {
	user := auth.UserFrom(r.Context())
	if h.Perms == nil {
		return user.Role == "admin"
	}
	snap, err := h.Perms.Snapshot(r.Context())
	if err != nil {
		return false
	}
	g := snap.Global(user.Subject())
	return g.Has(perm.ManageEvents) || g.Has(perm.ManageMessages)
}

// @Summary List the comments of an event
// @Description A page of the event's comments, oldest first. Without before it is the newest page; before names the oldest comment already loaded and returns the page ahead of it. has_more says older comments exist; muted says the caller switched the notices of this event's comments off. If the event is in a Talk, requires ViewChannel.
// @ID listCommunityEventComments
// @Tags Events
// @Produce json
// @Security cookieAuth
// @Param id path string true "Event ID" Format(uuid)
// @Param before query string false "Comment ID: return the comments before it." Format(uuid)
// @Param limit query int false "Page size, 1 to 100." default(50)
// @Success 200 {object} CommentList
// @Failure 400 {object} httpx.ErrorResponse "INVALID_INPUT"
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND"
// @Router /api/events/{id}/comments [get]
func (h *Handler) listComments(w http.ResponseWriter, r *http.Request) error {
	ev, err := h.loadViewable(r)
	if err != nil {
		return err
	}
	var before *uuid.UUID
	if raw := r.URL.Query().Get("before"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return httpx.ErrInvalidInput("before must be a UUID")
		}
		before = &id
	}
	limit := httpx.QueryLimit(r, "limit", DefaultCommentPage, MaxCommentPage)
	list, err := h.Store.ListComments(r.Context(), ev.ID, before, limit, auth.UserFrom(r.Context()).ID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, list)
	return nil
}

// @Summary Comment on an event
// @Description Adds a comment of 1 to 2000 characters (no replies; an event keeps at most 500). Everyone who can view the event may comment. Announces the comment to everyone who may see the event (event_comment) and tells the event's creator and attendees, who did not mute it, by event_comment_notice and Web Push. Rate limited per member.
// @ID createCommunityEventComment
// @Tags Events
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "Event ID" Format(uuid)
// @Param body body CommentRequest true "The comment"
// @Success 201 {object} EventComment
// @Failure 400 {object} httpx.ErrorResponse "INVALID_INPUT"
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND"
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the event holds 500 comments already."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED"
// @Router /api/events/{id}/comments [post]
func (h *Handler) createComment(w http.ResponseWriter, r *http.Request) error {
	ev, err := h.loadViewable(r)
	if err != nil {
		return err
	}
	var req CommentRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	user := auth.UserFrom(r.Context())
	c, err := h.Store.CreateComment(r.Context(), ev.ID, user.ID, req.Content)
	if err != nil {
		return err
	}
	h.broadcastComment(ev, CommentPayload{Action: CommentCreated, EventID: ev.ID, CommentID: c.ID, Comment: &c})
	h.notifyComment(r.Context(), ev, c)
	httpx.WriteJSON(w, http.StatusCreated, c)
	return nil
}

// @Summary Edit a comment
// @Description Changes the text of the caller's own comment (shown as edited). Rate limited per member.
// @ID updateCommunityEventComment
// @Tags Events
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "Event ID" Format(uuid)
// @Param commentID path string true "Comment ID" Format(uuid)
// @Param body body CommentRequest true "The new text"
// @Success 200 {object} EventComment
// @Failure 400 {object} httpx.ErrorResponse "INVALID_INPUT"
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not the author."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND"
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED"
// @Router /api/events/{id}/comments/{commentID} [patch]
func (h *Handler) updateComment(w http.ResponseWriter, r *http.Request) error {
	ev, err := h.loadViewable(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathUUID(r, "commentID")
	if err != nil {
		return err
	}
	var req CommentRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	c, err := h.Store.UpdateComment(r.Context(), ev.ID, id, auth.UserFrom(r.Context()).ID, req.Content)
	if err != nil {
		return err
	}
	h.broadcastComment(ev, CommentPayload{Action: CommentUpdated, EventID: ev.ID, CommentID: c.ID, Comment: &c})
	httpx.WriteJSON(w, http.StatusOK, c)
	return nil
}

// @Summary Delete a comment
// @Description Removes a comment. The author may delete their own; members with Manage events or Manage messages may delete any (audited).
// @ID deleteCommunityEventComment
// @Tags Events
// @Security cookieAuth
// @Param id path string true "Event ID" Format(uuid)
// @Param commentID path string true "Comment ID" Format(uuid)
// @Success 204 "Deleted."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN"
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND"
// @Router /api/events/{id}/comments/{commentID} [delete]
func (h *Handler) deleteComment(w http.ResponseWriter, r *http.Request) error {
	ev, err := h.loadViewable(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathUUID(r, "commentID")
	if err != nil {
		return err
	}
	user := auth.UserFrom(r.Context())
	c, err := h.Store.GetComment(r.Context(), ev.ID, id)
	if err != nil {
		return err
	}
	if c.User.ID != user.ID && !h.canModerateComments(r) {
		return httpx.ErrForbidden("only the author and moderators delete a comment")
	}
	if _, err := h.Store.DeleteComment(r.Context(), ev.ID, id); err != nil {
		return err
	}
	if c.User.ID != user.ID {
		h.auditComment(r, ev, c)
	}
	h.broadcastComment(ev, CommentPayload{Action: CommentDeleted, EventID: ev.ID, CommentID: id})
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *Handler) auditComment(r *http.Request, ev *CommunityEvent, c EventComment) {
	if h.Audit == nil {
		return
	}
	action := "event.comment_delete"
	if ev.ChannelID != nil {
		action = "event.talk_comment_delete"
	}
	excerpt := strings.TrimSpace(c.Content)
	if len([]rune(excerpt)) > 80 {
		excerpt = string([]rune(excerpt)[:80])
	}
	h.Audit.RecordOrLog(r.Context(), audit.Entry{
		Action: action, TargetType: "event", TargetID: ev.ID.String(), TargetName: ev.Title, ChannelID: ev.ChannelID,
		Details: map[string]any{"title": ev.Title, "author_id": c.User.ID, "author_name": c.User.Username, "excerpt": excerpt},
	})
}

// @Summary Mute or unmute the notices of an event's comments
// @Description Sets whether the caller is told about new comments of this event (the creator and attendees are by default).
// @ID muteCommunityEventComments
// @Tags Events
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "Event ID" Format(uuid)
// @Param body body CommentMute true "muted"
// @Success 200 {object} CommentMute
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND"
// @Router /api/events/{id}/comments/mute [put]
func (h *Handler) muteComments(w http.ResponseWriter, r *http.Request) error {
	ev, err := h.loadViewable(r)
	if err != nil {
		return err
	}
	var req CommentMute
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if err := h.Store.SetCommentMute(r.Context(), ev.ID, auth.UserFrom(r.Context()).ID, req.Muted); err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, req)
	return nil
}
