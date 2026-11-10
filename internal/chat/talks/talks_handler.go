package talks

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/automod"
	"github.com/msch128/mnema-talk/internal/chat/channels"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Mount registers the Create-a-Talk routes. Without a TalkManager the
// feature is off and the routes answer 404.
func (a *API) Mount(r chi.Router) {
	if a.Talks == nil {
		return
	}
	limited := r
	if a.TalkLimit != nil {
		limited = r.With(a.TalkLimit.By(auth.UserKey))
	}
	limited.Patch("/talks/{id}", httpx.Handle(a.updateTalk))
	limited.Post("/talks/{id}/invites", httpx.Handle(a.inviteToTalk))
	limited.Post("/dms/{id}/talk", httpx.Handle(a.StartDMTalk))
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
// @Param request body talks.UpdateTalkRequest true "Request body."
// @Success 200 {object} core.Channel "Updated Talk."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: the caller does not lead this Talk, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the Talk does not exist, is not a temporary Talk, or the caller cannot see it."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/talks/{id} [patch]
func (a *API) updateTalk(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	var req UpdateTalkRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if err := a.Guard.Require(r, id, perm.ViewChannel); err != nil {
		return err
	}
	if _, ok, err := TalkLeader(r.Context(), a.DB, id); err != nil {
		return err
	} else if !ok {
		return errNotTempTalk
	}
	if !a.Talks.IsLeader(id, auth.UserFrom(r.Context()).ID) {
		return errNotTalkLeader
	}
	// A name is text every member reads: the automod rules apply as to a message.
	var pending *automod.Pending
	if req.Name != nil {
		if pending, err = messages.ScreenText(r.Context(), a.DB, id, auth.UserFrom(r.Context()).ID, uuid.Nil, *req.Name); err != nil {
			return err
		}
	}
	ch, err := channels.EditChannel(r.Context(), a.DB, id, req.Name, nil, req.UserLimit, nil, nil, nil)
	if err != nil {
		return err
	}
	pending.Commit(r.Context(), uuid.Nil)
	if err := channels.FillTalkFields(r.Context(), a.DB, ch); err != nil {
		return err
	}
	// Fail closed: when the lookup fails the update goes unaudited, because
	// auditing a conversation's Talk would leak who called whom.
	if isDM, err := IsDMTalk(r.Context(), a.DB, id); err != nil {
		slog.WarnContext(r.Context(), "talk update not audited: DM lookup failed", "channel", id, "err", err)
	} else if !isDM {
		a.Audit.RecordOrLog(r.Context(), audit.Entry{
			Action: "channel.update", TargetType: "channel", TargetID: id.String(), TargetName: ch.Name,
			ChannelID: &id, Details: map[string]any{"temporary": true, "user_limit": ch.UserLimit},
		})
	}
	a.Channels.ChannelsChanged()
	httpx.WriteJSON(w, http.StatusOK, ch)
	return nil
}

// StartTalkResponse is the answer of POST /api/dms/{id}/talk.
type StartTalkResponse struct {
	// ChannelID is the new temporary Talk; the caller joins it like any Talk.
	ChannelID uuid.UUID `json:"channel_id" format:"uuid"`
	// Message is the invitation card posted into the conversation.
	Message *core.Message `json:"message"`
}

// StartDMTalk handles POST /api/dms/{id}/talk.
//
// @Summary Start a Talk from a direct message conversation
// @Description Opens a temporary Talk that belongs to the conversation (no Create-a-Talk lobby is needed): only the conversation's members can see or join it, the Owner included, and it is removed when it is empty. The caller leads it. An invitation card is posted into the conversation and the other members are rung (dm_call_ring). The caller joins the Talk like any other (WebSocket voice_join). Counts against the caller's limit of 5 new Talks per 10 minutes (429). Broadcasts channels_changed and message_create.
// @ID startDMTalk
// @Tags Channels
// @Produce json
// @Security cookieAuth
// @Param id path string true "Conversation ID." Format(uuid)
// @Success 201 {object} talks.StartTalkResponse "The new Talk and the invitation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: the caller cannot send in this conversation (not a member, blocked), or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the conversation does not exist or the caller is not in it."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests, or too many new Talks."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/dms/{id}/talk [post]
func (a *API) StartDMTalk(w http.ResponseWriter, r *http.Request) error {
	dm, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	ctx, err := a.dmSendContext(r, dm)
	if err != nil {
		return err
	}
	user := auth.UserFrom(ctx)
	global, err := a.Guard.Perms.Global(ctx, user.Subject())
	if err != nil {
		return err
	}
	if !global.Has(perm.Connect) {
		return errCannotConnect
	}
	// The call of a conversation follows the speak wait of a new member: no ringing one they cannot join.
	if err := a.Guard.Perms.RequireCall(ctx, user.Subject()); err != nil {
		return err
	}
	talk, err := a.Talks.OpenForDM(ctx, user.ID, dm)
	if err != nil {
		return err
	}
	msg, err := a.postTalkInvite(ctx, dm, user.ID, talk.ID)
	if err != nil {
		return err
	}
	a.Events.BroadcastChannel(dm, events.MessageCreate, msg)
	if a.Calls != nil {
		a.Calls.Begin(dm, talk.ID, user.ID, msg.ID, inviteName(msg))
	}
	httpx.WriteJSON(w, http.StatusCreated, StartTalkResponse{ChannelID: talk.ID, Message: msg})
	return nil
}
