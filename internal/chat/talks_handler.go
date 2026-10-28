package chat

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// mountTalks registers the Create-a-Talk routes. Without a TalkManager the
// feature is off and the routes answer 404.
func (h *Handler) mountTalks(r chi.Router) {
	if h.Talks == nil {
		return
	}
	limited := r
	if h.TalkLimit != nil {
		limited = r.With(h.TalkLimit.By(auth.UserKey))
	}
	limited.Patch("/talks/{id}", httpx.Handle(h.updateTalk))
	limited.Post("/talks/{id}/invites", httpx.Handle(h.inviteToTalk))
	limited.Post("/dms/{id}/talk", httpx.Handle(h.startDMTalk))
}

// UpdateTalkRequest is the body of PATCH /api/talks/{id}; omitted fields are
// kept.
type UpdateTalkRequest struct {
	Name *string `json:"name" minLength:"1" maxLength:"64" binding:"optional"`
	// UserLimit caps the members of the Talk at once; 0 removes the limit.
	UserLimit *int `json:"user_limit" minimum:"0" maximum:"999" binding:"optional"`
}

// updateTalk handles PATCH /api/talks/{id}.
//
// @Summary Rename a temporary Talk or set its member limit
// @Description Only the leader of a temporary Talk (opened from a Create-a-Talk lobby) may change it, and only its name and member limit; the Manage channels permission is not needed. A leader is the member who opened the Talk, and after they leave the member who has been in it longest. 403 for anyone else, 404 when the Talk is not a temporary Talk the caller can see. Broadcasts channels_changed.
// @ID updateTalk
// @Tags Channels
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "Resource ID." Format(uuid)
// @Param request body UpdateTalkRequest true "Request body."
// @Success 200 {object} Channel "Updated Talk."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: the caller does not lead this Talk, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the Talk does not exist, is not a temporary Talk, or the caller cannot see it."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/talks/{id} [patch]
func (h *Handler) updateTalk(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	var req UpdateTalkRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if err := h.require(r, id, perm.ViewChannel); err != nil {
		return err
	}
	if _, ok, err := TalkLeader(r.Context(), h.DB, id); err != nil {
		return err
	} else if !ok {
		return errNotTempTalk
	}
	if !h.Talks.IsLeader(id, auth.UserFrom(r.Context()).ID) {
		return errNotTalkLeader
	}
	ch, err := UpdateChannel(r.Context(), h.DB, id, req.Name, nil, req.UserLimit, nil)
	if err != nil {
		return err
	}
	if err := FillTalkFields(r.Context(), h.DB, ch); err != nil {
		return err
	}
	h.Audit.RecordOrLog(r.Context(), audit.Entry{
		Action: "channel.update", TargetType: "channel", TargetID: id.String(), TargetName: ch.Name,
		ChannelID: &id, Details: map[string]any{"temporary": true, "user_limit": ch.UserLimit},
	})
	h.channelsChanged()
	httpx.WriteJSON(w, http.StatusOK, ch)
	return nil
}

// StartTalkResponse is the answer of POST /api/dms/{id}/talk.
type StartTalkResponse struct {
	// ChannelID is the new temporary Talk; the caller joins it like any Talk.
	ChannelID uuid.UUID `json:"channel_id" format:"uuid"`
	// Message is the invitation card posted into the conversation.
	Message *Message `json:"message"`
}

// usableLobby reports whether the caller may see and join the lobby.
func (h *Handler) usableLobby(r *http.Request) func(uuid.UUID) bool {
	return func(id uuid.UUID) bool {
		if h.Perms == nil {
			return true
		}
		p, err := h.channelPerms(r, id)
		return err == nil && p.Has(perm.ViewChannel|perm.Connect)
	}
}

// startDMTalk handles POST /api/dms/{id}/talk.
//
// @Summary Start a Talk from a direct message conversation
// @Description Opens a temporary Talk from the first Create-a-Talk lobby the caller can see and join (in sidebar order), makes the caller its leader, and posts an invitation card into the conversation; the other members join with the card's Join button, which runs the ordinary permission checks. The caller joins the Talk like any other (WebSocket voice_join). 409 when the community has no lobby the caller can use, or the lobby has 50 Talks open. Counts against the caller's limit of 5 new Talks per 10 minutes (429). Broadcasts channels_changed and message_create.
// @ID startDMTalk
// @Tags Channels
// @Produce json
// @Security cookieAuth
// @Param id path string true "Conversation ID." Format(uuid)
// @Success 201 {object} StartTalkResponse "The new Talk and the invitation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: the caller cannot send in this conversation (not a member, blocked), or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the conversation does not exist or the caller is not in it."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: no lobby the caller can use, or too many open Talks."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests, or too many new Talks."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/dms/{id}/talk [post]
func (h *Handler) startDMTalk(w http.ResponseWriter, r *http.Request) error {
	dm, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	ctx, err := h.dmSendContext(r, dm)
	if err != nil {
		return err
	}
	lobby, err := FirstLobbyFor(ctx, h.DB, h.usableLobby(r))
	if err != nil {
		return err
	}
	user := auth.UserFrom(ctx)
	talk, err := h.Talks.Open(ctx, user.ID, lobby)
	if err != nil {
		return err
	}
	msg, err := h.postTalkInvite(ctx, dm, user.ID, talk.ID)
	if err != nil {
		return err
	}
	h.Events.BroadcastChannel(dm, events.MessageCreate, msg)
	httpx.WriteJSON(w, http.StatusCreated, StartTalkResponse{ChannelID: talk.ID, Message: msg})
	return nil
}
