package bot

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// ResponseData is the message of an interaction response.
type ResponseData struct {
	Content string         `json:"content" maxLength:"4000"`
	Embeds  []chat.InEmbed `json:"embeds"`
	// Flags: 64 makes the response ephemeral, visible only to the member who ran
	// the command (never stored).
	Flags int `json:"flags" enums:"0,64"`
}

// CallbackRequest is the body of the first response to an interaction.
type CallbackRequest struct {
	// Type is 4 (reply with a message) or 5 (acknowledge now, answer later
	// with follow-ups).
	Type int           `json:"type" enums:"4,5"`
	Data *ResponseData `json:"data"`
}

// callback handles POST /api/bot/v1/interactions/{interactionID}/{token}/callback.
//
// @Summary Respond to an interaction
// @Description The first response to an interaction a member created by running a command. Type 4 posts a message in the channel as the bot (or, with flags 64, shows it to the member only); type 5 only acknowledges, and follow-ups answer. An interaction can be answered for 15 minutes, with at most 5 responses in total. The bot needs Send messages in the channel at that moment.
// @ID botInteractionCallback
// @Tags Bot API
// @Accept json
// @Security botAuth
// @Param interactionID path string true "Interaction ID." Format(uuid)
// @Param token path string true "Interaction token from the INTERACTION_CREATE event."
// @Param body body CallbackRequest true "The response."
// @Success 204 "Accepted."
// @Failure 400 {object} httpx.DiscordError "Invalid form body (50035) or empty message (50006)."
// @Failure 401 {object} httpx.DiscordError "Missing or invalid token (0)."
// @Failure 403 {object} httpx.DiscordError "Missing Permissions (50013)."
// @Failure 404 {object} httpx.DiscordError "Unknown interaction (10062)."
// @Failure 409 {object} httpx.DiscordError "Already answered, or too many responses."
// @Failure 429 {object} httpx.DiscordRateLimit "Rate limited; see retry_after and Retry-After."
// @Router /api/bot/v1/interactions/{interactionID}/{token}/callback [post]
func (c *Commands) callback(w http.ResponseWriter, r *http.Request) error {
	var req CallbackRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	var msg *message
	switch req.Type {
	case CallbackDeferred:
	case CallbackMessage:
		if req.Data == nil {
			return httpx.ErrInvalidInput("data is required for type 4")
		}
		var err error
		if msg, err = validate(*req.Data); err != nil {
			return err
		}
	default:
		return httpx.ErrInvalidInput("type must be 4 or 5")
	}
	in, err := c.claimFrom(r, true)
	if err != nil {
		return err
	}
	if msg != nil {
		if _, err := c.respond(r, in, *msg); err != nil {
			return err
		}
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// followup handles POST /api/bot/v1/interactions/{interactionID}/{token}/followups.
//
// @Summary Send a follow-up to an interaction
// @Description After the first response: another message for the same interaction (public, or ephemeral with flags 64). Counts towards the 5 responses.
// @ID botInteractionFollowup
// @Tags Bot API
// @Accept json
// @Produce json
// @Security botAuth
// @Param interactionID path string true "Interaction ID." Format(uuid)
// @Param token path string true "Interaction token from the INTERACTION_CREATE event."
// @Param body body ResponseData true "The message."
// @Success 201 {object} chat.Message "The posted message."
// @Success 204 "Shown to the member only."
// @Failure 400 {object} httpx.DiscordError "Invalid form body (50035) or empty message (50006)."
// @Failure 401 {object} httpx.DiscordError "Missing or invalid token (0)."
// @Failure 403 {object} httpx.DiscordError "Missing Permissions (50013)."
// @Failure 404 {object} httpx.DiscordError "Unknown interaction (10062)."
// @Failure 409 {object} httpx.DiscordError "Respond first, or too many responses."
// @Failure 429 {object} httpx.DiscordRateLimit "Rate limited; see retry_after and Retry-After."
// @Router /api/bot/v1/interactions/{interactionID}/{token}/followups [post]
func (c *Commands) followup(w http.ResponseWriter, r *http.Request) error {
	var data ResponseData
	if err := httpx.DecodeJSON(r, &data); err != nil {
		return err
	}
	m, err := validate(data)
	if err != nil {
		return err
	}
	in, err := c.claimFrom(r, false)
	if err != nil {
		return err
	}
	msg, err := c.respond(r, in, *m)
	if err != nil {
		return err
	}
	if msg == nil {
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
	httpx.WriteJSON(w, http.StatusCreated, msg)
	return nil
}

func (c *Commands) claimFrom(r *http.Request, callback bool) (*interaction, error) {
	id, err := httpx.PathUUID(r, "interactionID")
	if err != nil {
		return nil, err
	}
	return c.store.claim(id, chi.URLParam(r, "token"), auth.BotFrom(r.Context()).BotID, callback)
}

// message is a validated response.
type message struct {
	content   string
	embeds    []chat.Embed
	ephemeral bool
}

// validate checks the content and embeds of a response.
func validate(d ResponseData) (*message, error) {
	content, err := chat.ValidateContent(d.Content, true)
	if err != nil {
		return nil, err
	}
	embeds, err := chat.SanitizeEmbeds(d.Embeds)
	if err != nil {
		return nil, httpx.ErrInvalidInput(err.Error())
	}
	if content == "" && len(embeds) == 0 {
		return nil, httpx.ErrInvalidInput("Cannot send an empty message")
	}
	return &message{content: content, embeds: embeds, ephemeral: d.Flags&flagEphemeral != 0}, nil
}

// respond posts the message of a response, or shows it to the invoker only.
func (c *Commands) respond(r *http.Request, in *interaction, m message) (*chat.Message, error) {
	ctx := r.Context()
	snap, err := c.Perms.ForChannel(ctx, in.channel)
	if err != nil {
		return nil, err
	}
	if err := perm.CheckChannel(snap.Channel(auth.UserFrom(ctx).Subject(), in.channel), perm.SendMessages); err != nil {
		return nil, err
	}
	if m.ephemeral {
		c.Events.SendToUsers([]uuid.UUID{in.invoker}, "interaction_response", map[string]any{
			"channel_id": in.channel, "bot_id": in.botID, "bot_name": in.botName, "bot_avatar_url": in.avatar,
			"content": m.content, "embeds": m.embeds, "created_at": time.Now().UTC(),
		})
		return nil, nil
	}
	// A command response never mentions everyone; members are notified only
	// when they can see the channel.
	ctx = chat.WithMentionPolicy(ctx, chat.MentionPolicy{
		NoEveryone: true,
		CanView:    func(id uuid.UUID) bool { return snap.Channel(perm.Subject{ID: id}, in.channel).Has(perm.ViewChannel) },
	})
	id, err := chat.InsertMessage(ctx, c.DB, c.Online, chat.NewMessage{ChannelID: in.channel, UserID: in.botUser, Content: m.content, Embeds: m.embeds}, nil)
	if err != nil {
		return nil, err
	}
	msg, err := chat.GetMessage(ctx, c.DB, id)
	if err != nil {
		return nil, err
	}
	c.Events.BroadcastChannel(msg.ChannelID, "message_create", msg)
	return msg, nil
}
