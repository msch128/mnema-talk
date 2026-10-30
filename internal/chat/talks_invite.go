package chat

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// A Talk invitation is an ordinary message in a direct message conversation
// whose embed carries the Talk's id (Embed.TalkID). The card is only a link:
// whoever clicks Join joins through the same checks as for any Talk, so an
// invitation never lets anyone into a Talk they could not join.

// InviteToTalkRequest is the body of POST /api/talks/{id}/invites.
type InviteToTalkRequest struct {
	// ConversationID is the direct message conversation to post the card into.
	ConversationID uuid.UUID `json:"conversation_id" format:"uuid"`
}

// dmSendContext checks that the caller may send in the direct message
// conversation dm and returns the context to post with.
func (h *Handler) dmSendContext(r *http.Request, dm uuid.UUID) (context.Context, error) {
	ch, err := LoadChannel(r.Context(), h.DB, dm)
	if err != nil || ch.Type != channelKindDM {
		return nil, errChannelNotFound
	}
	return h.sendContext(r, dm, perm.SendMessages)
}

// postTalkInvite posts the invitation card for talk into dm as user.
func (h *Handler) postTalkInvite(ctx context.Context, dm, user, talk uuid.UUID) (*Message, error) {
	var name string
	err := h.DB.QueryRow(ctx, `SELECT name FROM channels WHERE id = $1 AND type = 'voice'`, talk).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errChannelNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load talk for invite: %w", err)
	}
	id, err := InsertMessage(ctx, h.DB, h.Online, NewMessage{
		ChannelID: dm, UserID: user, Embeds: []Embed{{Title: name, TalkID: &talk}},
	}, nil)
	if err != nil {
		return nil, err
	}
	return GetMessage(ctx, h.DB, id)
}

// inviteName is the Talk's name on the invitation card msg.
func inviteName(msg *Message) string {
	if len(msg.Embeds) == 0 {
		return ""
	}
	return msg.Embeds[0].Title
}

// inviteToTalk handles POST /api/talks/{id}/invites.
//
// @Summary Invite a conversation to a Talk
// @Description Posts an invitation card for the Talk into a direct message conversation of the caller ("Bring into Talk"). The caller needs to see and join the Talk and to send in the conversation. The card grants nothing: members who click Join go through the normal permission checks, and a member who cannot join the Talk sees that. Broadcasts message_create.
// @ID inviteToTalk
// @Tags Channels
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "Talk ID." Format(uuid)
// @Param request body InviteToTalkRequest true "Request body."
// @Success 201 {object} Message "The invitation message."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: the caller cannot join the Talk or send in the conversation, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the Talk or the conversation does not exist, or the caller cannot see it."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/talks/{id}/invites [post]
func (h *Handler) inviteToTalk(w http.ResponseWriter, r *http.Request) error {
	talk, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	var req InviteToTalkRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if err := h.require(r, talk, perm.ViewChannel|perm.Connect); err != nil {
		return err
	}
	ctx, err := h.dmSendContext(r, req.ConversationID)
	if err != nil {
		return err
	}
	msg, err := h.postTalkInvite(ctx, req.ConversationID, auth.UserFrom(ctx).ID, talk)
	if err != nil {
		return err
	}
	h.Events.BroadcastChannel(req.ConversationID, events.MessageCreate, msg)
	h.Calls.Begin(req.ConversationID, talk, auth.UserFrom(ctx).ID, msg.ID, inviteName(msg))
	httpx.WriteJSON(w, http.StatusCreated, msg)
	return nil
}
