package bot

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/botapi"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// OriginalEdit is the body of PATCH .../messages/@original: each field left
// out stays as it is. After callback 5 it is the message that is posted.
type OriginalEdit struct {
	Content    *string             `json:"content" maxLength:"4000" binding:"optional"`
	Embeds     *[]messages.InEmbed `json:"embeds" binding:"optional"`
	Components *[]core.ActionRow   `json:"components" binding:"optional"`
	// AllowedMentions narrows whom the new text notifies, as on a post.
	AllowedMentions *botapi.BotAllowedMentions `json:"allowed_mentions" binding:"optional"`
	// Flags is not accepted: whether the answer is ephemeral is decided by the
	// first response. It is read only to refuse it with a clear message.
	Flags *int `json:"flags" swaggerignore:"true"`
}

var errFlagsOnOriginal = httpx.ErrInvalidInput("flags cannot be set on @original: the first response decides whether the answer is ephemeral (flags 64 on callback 4 or 5)")

var errNoOriginal = httpx.ErrNotFound("unknown message: the interaction has no original response")

// originalOf is what @original names now: the response posted by callback 4
// or by the first PATCH after callback 5, or, after callback 6 or 7, the
// message the component is on. post says a deferred answer (5) is not posted
// yet. The caller holds in.op.
func (c *Commands) originalOf(in *interaction) (target source, post bool, err error) {
	answer, _ := c.store.state(in)
	switch {
	case answer == 0:
		return source{}, false, httpx.ErrConflict("respond to the interaction first")
	case in.originalGone:
		return source{}, false, core.ErrMessageNotFound
	case in.original.set():
		return in.original, false, nil
	case answer == CallbackDeferred:
		return source{}, true, nil
	case (answer == CallbackDeferredUpdate || answer == CallbackUpdate) && in.source.set():
		return in.source, false, nil
	}
	return source{}, false, errNoOriginal
}

// editOriginal handles PATCH /api/bot/v1/interactions/{interactionID}/{token}/messages/@original.
//
// @Summary Edit the original response
// @Description Edits the original response of an interaction: the message of callback 4, or for a component answered with 6 or 7 the message the component is on. After callback 5 the first PATCH posts the answer (ephemeral when callback 5 had flags 64) and counts towards the 5 messages. Absent fields stay as they are. An ephemeral original is replaced in the member's view (204). Works for the 15 minutes of the interaction; needs Send messages in the channel.
// @ID botEditOriginalResponse
// @Tags Bot API
// @Accept json
// @Produce json
// @Security botAuth
// @Param interactionID path string true "Interaction ID." Format(uuid)
// @Param token path string true "Interaction token from the INTERACTION_CREATE event."
// @Param body body OriginalEdit true "The new content, embeds and components."
// @Success 200 {object} core.Message "The edited or posted message."
// @Success 204 "The member's ephemeral note was replaced, or posted."
// @Failure 400 {object} httpx.DiscordError "Invalid form body (50035) or empty message (50006)."
// @Failure 401 {object} httpx.DiscordError "Missing or invalid token (0)."
// @Failure 403 {object} httpx.DiscordError "Missing Permissions (50013)."
// @Failure 404 {object} httpx.DiscordError "Unknown interaction (10062), or no original response (10008)."
// @Failure 409 {object} httpx.DiscordError "Respond first, or too many responses."
// @Failure 429 {object} httpx.DiscordRateLimit "Rate limited; see retry_after and Retry-After."
// @Router /api/bot/v1/interactions/{interactionID}/{token}/messages/at_original [patch]
func (c *Commands) editOriginal(w http.ResponseWriter, r *http.Request) error {
	var req OriginalEdit
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if req.Flags != nil {
		return errFlagsOnOriginal
	}
	e, err := parseEdit(req.Content, req.Embeds, req.Components, req.AllowedMentions)
	if err != nil {
		return err
	}
	in, err := c.findFrom(r)
	if err != nil {
		return err
	}
	in.op.Lock()
	defer in.op.Unlock()
	target, post, err := c.originalOf(in)
	if err != nil {
		return err
	}
	var msg *core.Message
	if post {
		msg, err = c.postOriginal(r, in, req)
	} else {
		msg, err = c.updateTarget(r, in, target, e)
	}
	if err != nil {
		return err
	}
	if msg == nil {
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
	httpx.WriteJSON(w, http.StatusOK, msg)
	return nil
}

// postOriginal posts the answer that callback 5 announced: it becomes the
// original response and counts as one of the messages.
func (c *Commands) postOriginal(r *http.Request, in *interaction, req OriginalEdit) (*core.Message, error) {
	_, later := c.store.state(in)
	d := CallbackData{Content: req.Content, Embeds: req.Embeds, Components: req.Components, AllowedMentions: req.AllowedMentions}
	if later {
		d.Flags = flagEphemeral
	}
	m, err := validate(d.response())
	if err != nil {
		return nil, err
	}
	if err := c.store.countResponse(in); err != nil {
		return nil, err
	}
	msg, posted, err := c.respond(r, in, *m)
	if err != nil {
		c.store.uncount(in)
		return nil, err
	}
	in.original = posted
	return msg, nil
}

// deleteOriginal handles DELETE /api/bot/v1/interactions/{interactionID}/{token}/messages/@original.
//
// @Summary Delete the original response
// @Description Deletes the original response of an interaction (see the PATCH of @original): a message is deleted as by DELETE /channels/{id}/messages/{id}, an ephemeral note disappears from the member's view. Works for the 15 minutes of the interaction.
// @ID botDeleteOriginalResponse
// @Tags Bot API
// @Security botAuth
// @Param interactionID path string true "Interaction ID." Format(uuid)
// @Param token path string true "Interaction token from the INTERACTION_CREATE event."
// @Success 204 "Deleted."
// @Failure 401 {object} httpx.DiscordError "Missing or invalid token (0)."
// @Failure 403 {object} httpx.DiscordError "Missing Permissions (50013)."
// @Failure 404 {object} httpx.DiscordError "Unknown interaction (10062), or no original response (10008)."
// @Failure 409 {object} httpx.DiscordError "Respond first."
// @Failure 429 {object} httpx.DiscordRateLimit "Rate limited; see retry_after and Retry-After."
// @Router /api/bot/v1/interactions/{interactionID}/{token}/messages/at_original [delete]
func (c *Commands) deleteOriginal(w http.ResponseWriter, r *http.Request) error {
	in, err := c.findFrom(r)
	if err != nil {
		return err
	}
	in.op.Lock()
	defer in.op.Unlock()
	target, post, err := c.originalOf(in)
	if err != nil {
		return err
	}
	if post {
		return errNoOriginal
	}
	if err := c.deleteTarget(r, in, target); err != nil {
		return err
	}
	in.originalGone = true
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (c *Commands) deleteTarget(r *http.Request, in *interaction, target source) error {
	if target.message != uuid.Nil {
		if c.Messages == nil {
			return errNoMessages
		}
		return c.Messages.DeleteBotMessage(r.Context(), auth.UserFrom(r.Context()), in.channel, target.message)
	}
	n, ok := c.store.noteOf(target.note, in.botID)
	if !ok {
		return core.ErrMessageNotFound
	}
	c.store.dropNote(n.id)
	c.showNote(in.botName, in.avatar, n, true)
	return nil
}
