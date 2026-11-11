package bot

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/botapi"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/mentions"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// ResponseData is the message of an interaction response.
type ResponseData struct {
	Content string             `json:"content" maxLength:"4000"`
	Embeds  []messages.InEmbed `json:"embeds"`
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

func parseCallbackMessage(req CallbackRequest) (*message, error) {
	switch req.Type {
	case CallbackDeferred:
		return nil, nil
	case CallbackMessage:
		if req.Data == nil {
			return nil, httpx.ErrInvalidInput("data is required for type 4")
		}
		return validate(*req.Data)
	default:
		return nil, httpx.ErrInvalidInput("type must be 4 or 5")
	}
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
	msg, err := parseCallbackMessage(req)
	if err != nil {
		return err
	}
	in, err := c.claimFrom(r, true)
	if err != nil {
		return err
	}
	if msg != nil {
		if _, err := c.respond(r, in, *msg); err != nil {
			c.store.release(in.id, true)
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
// @Success 201 {object} core.Message "The posted message."
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
		c.store.release(in.id, false)
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
	embeds    []core.Embed
	ephemeral bool
}

// validate checks the content and embeds of a response.
func validate(d ResponseData) (*message, error) {
	content, embeds, err := botapi.ValidateBotMessage(d.Content, d.Embeds)
	if err != nil {
		return nil, err
	}
	return &message{content: content, embeds: embeds, ephemeral: d.Flags&flagEphemeral != 0}, nil
}

// respond posts the message of a response, or shows it to the invoker only.
func (c *Commands) respond(r *http.Request, in *interaction, m message) (*core.Message, error) {
	ctx := r.Context()
	snap, p, err := botapi.CheckBotSend(ctx, c.Perms, c.DB, auth.UserFrom(ctx), in.channel)
	if err != nil {
		return nil, err
	}
	if m.ephemeral {
		c.Events.SendToUsers([]uuid.UUID{in.invoker}, InteractionResponse{
			ChannelID: in.channel, BotID: in.botID, BotName: in.botName, BotAvatarURL: in.avatar,
			Content: m.content, Embeds: m.embeds, CreatedAt: time.Now().UTC(),
		})
		return nil, nil
	}
	// A command response never mentions everyone; members are notified only
	// when they can see the channel.
	ctx = mentions.WithMentionPolicy(botapi.SendContext(ctx, p), mentions.MentionPolicy{
		NoEveryone: true,
		CanView:    func(id uuid.UUID) bool { return snap.Channel(perm.Subject{ID: id}, in.channel).Has(perm.ViewChannel) },
	})
	id, err := messages.InsertMessage(ctx, c.DB, c.Online, messages.NewMessage{ChannelID: in.channel, UserID: in.botUser, Content: m.content, Embeds: m.embeds}, nil)
	if err != nil {
		return nil, err
	}
	msg, err := messages.GetMessage(ctx, c.DB, id)
	if err != nil {
		return nil, err
	}
	c.Events.BroadcastChannel(msg.ChannelID, (*core.MessageCreate)(msg))
	return msg, nil
}

// InteractionResponse is a bot's reply to a slash command that only the member
// who ran it sees (interaction_response); it is never stored. The fields are in
// the alphabetical order of their JSON keys, which must not change.
type InteractionResponse struct {
	BotAvatarURL string       `json:"bot_avatar_url"`
	BotID        uuid.UUID    `json:"bot_id" format:"uuid"`
	BotName      string       `json:"bot_name"`
	ChannelID    uuid.UUID    `json:"channel_id" format:"uuid"`
	Content      string       `json:"content"`
	CreatedAt    time.Time    `json:"created_at" format:"date-time"`
	Embeds       []core.Embed `json:"embeds" extensions:"x-nullable"`
}

func (InteractionResponse) EventName() string { return events.InteractionResponse }
