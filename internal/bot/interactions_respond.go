package bot

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/botapi"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/mentions"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// ResponseData is the message of an interaction response or a follow-up.
type ResponseData struct {
	Content string             `json:"content" maxLength:"4000"`
	Embeds  []messages.InEmbed `json:"embeds"`
	// Components are buttons and select menus (doc/bots.md, Message components).
	Components []core.ActionRow `json:"components" binding:"optional"`
	// Flags: 64 makes the response ephemeral, visible only to the member who ran
	// the command (never stored).
	Flags int `json:"flags" enums:"0,64"`
	// AllowedMentions narrows whom the message notifies, as on a post.
	AllowedMentions *botapi.BotAllowedMentions `json:"allowed_mentions" binding:"optional"`
}

// CallbackData is the data of a callback: the message of type 4, the update
// of type 7 (a field left out stays as it is), the flags of type 5, the
// modal of type 9.
type CallbackData struct {
	Content    *string             `json:"content" maxLength:"4000" binding:"optional"`
	Embeds     *[]messages.InEmbed `json:"embeds" binding:"optional"`
	Components *[]core.ActionRow   `json:"components" binding:"optional"`
	// Flags: 64 makes the message of type 4, or the answer that follows type 5, ephemeral.
	Flags int `json:"flags" enums:"0,64" binding:"optional"`
	// CustomID and Title belong to a modal (type 9); its components are rows of text inputs.
	CustomID string `json:"custom_id" maxLength:"100" binding:"optional"`
	Title    string `json:"title" maxLength:"45" binding:"optional"`
	// AllowedMentions narrows whom the message of type 4, or the new text of
	// type 7, notifies, as on a post.
	AllowedMentions *botapi.BotAllowedMentions `json:"allowed_mentions" binding:"optional"`
}

// CallbackRequest is the body of the first response to an interaction.
type CallbackRequest struct {
	// Type is 4 (reply with a message), 5 (acknowledge now, answer later),
	// 6 (acknowledge a click), 7 (update the message the component is on) or
	// 9 (open a modal); which answer which interaction is in doc/bots.md.
	Type int           `json:"type" enums:"4,5,6,7,9"`
	Data *CallbackData `json:"data"`
}

// response converts the data of type 4 to the message it posts.
func (d *CallbackData) response() ResponseData {
	var out ResponseData
	if d == nil {
		return out
	}
	if d.Content != nil {
		out.Content = *d.Content
	}
	if d.Embeds != nil {
		out.Embeds = *d.Embeds
	}
	if d.Components != nil {
		out.Components = *d.Components
	}
	out.Flags = d.Flags
	out.AllowedMentions = d.AllowedMentions
	return out
}

// answerFunc carries out a callback on the interaction it answers.
type answerFunc func(r *http.Request, in *interaction) error

// parseCallback checks the body of a callback before the interaction is
// looked up, and returns what answers it.
func (c *Commands) parseCallback(req CallbackRequest) (answerFunc, error) {
	switch req.Type {
	case CallbackMessage:
		if req.Data == nil {
			return nil, httpx.ErrInvalidInput("data is required for type 4")
		}
		m, err := validate(req.Data.response())
		if err != nil {
			return nil, err
		}
		return func(r *http.Request, in *interaction) error { return c.answerMessage(r, in, *m) }, nil
	case CallbackDeferred:
		later := req.Data != nil && req.Data.Flags&flagEphemeral != 0
		return func(_ *http.Request, in *interaction) error { return c.deferAnswer(in, CallbackDeferred, later) }, nil
	case CallbackDeferredUpdate:
		return func(_ *http.Request, in *interaction) error { return c.deferAnswer(in, CallbackDeferredUpdate, false) }, nil
	case CallbackUpdate:
		e, err := parseUpdate(req.Data)
		if err != nil {
			return nil, err
		}
		return func(r *http.Request, in *interaction) error { return c.answerUpdate(r, in, e) }, nil
	case CallbackModal:
		return c.parseModal(req.Data)
	}
	return nil, httpx.ErrInvalidInput("type must be 4, 5, 6, 7 or 9")
}

// callback handles POST /api/bot/v1/interactions/{interactionID}/{token}/callback.
//
// @Summary Respond to an interaction
// @Description The first response to an interaction: a command run, a component click or a modal submit. Type 4 posts a message in the channel as the bot (or, with flags 64, shows it to the member only); type 5 only acknowledges, and the answer follows with PATCH @original or follow-ups (with flags 64 it is ephemeral); type 6 acknowledges a click without changing anything; type 7 updates the message the component is on (absent fields stay); type 9 opens a modal (not for a modal submit). Types 6 and 7 need a component's message: a click, or a modal opened from one. An interaction can be answered for 15 minutes, with at most 5 messages in total. Posting or updating needs Send messages in the channel at that moment.
// @ID botInteractionCallback
// @Tags Bot API
// @Accept json
// @Security botAuth
// @Param interactionID path string true "Interaction ID." Format(uuid)
// @Param token path string true "Interaction token from the INTERACTION_CREATE event."
// @Param body body CallbackRequest true "The response."
// @Success 204 "Accepted."
// @Failure 400 {object} httpx.DiscordError "Invalid form body (50035), empty message (50006), or a callback type the interaction does not take."
// @Failure 401 {object} httpx.DiscordError "Missing or invalid token (0)."
// @Failure 403 {object} httpx.DiscordError "Missing Permissions (50013)."
// @Failure 404 {object} httpx.DiscordError "Unknown interaction (10062), or the message to update is gone (10008)."
// @Failure 409 {object} httpx.DiscordError "Already answered."
// @Failure 429 {object} httpx.DiscordRateLimit "Rate limited; see retry_after and Retry-After."
// @Router /api/bot/v1/interactions/{interactionID}/{token}/callback [post]
func (c *Commands) callback(w http.ResponseWriter, r *http.Request) error {
	var req CallbackRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	answer, err := c.parseCallback(req)
	if err != nil {
		return err
	}
	in, err := c.findFrom(r)
	if err != nil {
		return err
	}
	if req.Type == CallbackUpdate && req.Data != nil && !c.withinSendLimit(w, in, req.Data.AllowedMentions) {
		return nil // answered 429
	}
	in.op.Lock()
	defer in.op.Unlock()
	if err := answer(r, in); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// answerMessage is callback 4: the message becomes the original response.
func (c *Commands) answerMessage(r *http.Request, in *interaction, m message) error {
	if err := c.store.takeAnswer(in, CallbackMessage, false); err != nil {
		return err
	}
	_, posted, err := c.respond(r, in, m)
	if err != nil {
		c.store.undoAnswer(in)
		return err
	}
	in.original = posted
	return nil
}

// followup handles POST /api/bot/v1/interactions/{interactionID}/{token}/followups.
//
// @Summary Send a follow-up to an interaction
// @Description After the first response (not a modal): another message for the same interaction (public, or ephemeral with flags 64). Counts towards the 5 messages.
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
	in, err := c.findFrom(r)
	if err != nil {
		return err
	}
	if err := c.store.countResponse(in); err != nil {
		return err
	}
	msg, _, err := c.respond(r, in, *m)
	if err != nil {
		c.store.uncount(in)
		return err
	}
	if msg == nil {
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
	httpx.WriteJSON(w, http.StatusCreated, msg)
	return nil
}

func (c *Commands) findFrom(r *http.Request) (*interaction, error) {
	id, err := httpx.PathUUID(r, "interactionID")
	if err != nil {
		return nil, err
	}
	return c.store.find(id, chi.URLParam(r, "token"), auth.BotFrom(r.Context()).BotID)
}

// message is a validated response.
type message struct {
	content    string
	embeds     []core.Embed
	components []core.ActionRow
	ephemeral  bool
	allowed    *messages.AllowedMentions
}

// validate checks the content, embeds and components of a response.
func validate(d ResponseData) (*message, error) {
	comps, err := core.ValidateComponents(d.Components)
	if err != nil {
		return nil, err
	}
	content, embeds, err := botapi.ValidateBotMessage(d.Content, d.Embeds, len(comps) > 0)
	if err != nil {
		return nil, err
	}
	return &message{content: content, embeds: embeds, components: comps, ephemeral: d.Flags&flagEphemeral != 0,
		allowed: (*messages.AllowedMentions)(d.AllowedMentions)}, nil
}

// respond posts the message of a response, or shows it to the invoker only
// as a note. It returns the posted message (nil for a note) and what the
// response is: the message or the note.
func (c *Commands) respond(r *http.Request, in *interaction, m message) (*core.Message, source, error) {
	ctx := r.Context()
	snap, p, err := botapi.CheckBotSend(ctx, c.Perms, c.DB, auth.UserFrom(ctx), in.channel)
	if err != nil {
		return nil, source{}, err
	}
	if m.ephemeral {
		if err := c.invokerSees(ctx, in.channel, in.invoker); err != nil {
			return nil, source{}, err
		}
		n := note{id: uuid.New(), botID: in.botID, botUser: in.botUser, channel: in.channel, invoker: in.invoker,
			created: in.created, content: m.content, embeds: m.embeds, components: m.components}
		c.store.keepNote(n)
		c.showNote(in.botName, in.avatar, n, false)
		return nil, source{note: n.id}, nil
	}
	// A response notifies like a post of the bot: allowed_mentions narrows it,
	// @all, @here and roles only when it asks for them (and the bot may), and
	// only members who can see the channel.
	ctx = mentions.WithMentionPolicy(botapi.SendContext(ctx, p), messages.BotMentionPolicy(snap, in.channel, p, m.allowed))
	var also func(tx pgx.Tx, id uuid.UUID) error
	if len(m.components) > 0 {
		also = func(tx pgx.Tx, id uuid.UUID) error { return core.SaveComponents(ctx, tx, id, m.components) }
	}
	id, err := messages.InsertMessage(ctx, c.DB, c.Online, messages.NewMessage{ChannelID: in.channel, UserID: in.botUser, Content: m.content, Embeds: m.embeds}, also)
	if err != nil {
		return nil, source{}, err
	}
	msg, err := messages.GetMessage(ctx, c.DB, id)
	if err != nil {
		return nil, source{}, err
	}
	c.Events.BroadcastChannel(msg.ChannelID, (*core.MessageCreate)(msg))
	return msg, source{message: msg.ID}, nil
}

// showNote sends a note to its member's sessions: new, replaced (the same id)
// or, with deleted, taken away.
func (c *Commands) showNote(botName, avatar string, n note, deleted bool) {
	c.Events.SendToUsers([]uuid.UUID{n.invoker}, InteractionResponse{
		ID: n.id, ChannelID: n.channel, BotID: n.botID, BotName: botName, BotAvatarURL: avatar,
		Content: n.content, Embeds: n.embeds, Components: n.components, Deleted: deleted, CreatedAt: time.Now().UTC(),
	})
}

// InteractionResponse is a bot's reply to an interaction that only the member
// who caused it sees (interaction_response); it is never stored. A later one
// with the same id replaces the note, one with deleted takes it away. The
// fields are in the alphabetical order of their JSON keys, which must not
// change.
type InteractionResponse struct {
	BotAvatarURL string    `json:"bot_avatar_url"`
	BotID        uuid.UUID `json:"bot_id" format:"uuid"`
	BotName      string    `json:"bot_name"`
	ChannelID    uuid.UUID `json:"channel_id" format:"uuid"`
	// Components of the note; a click is POST /api/channels/{id}/ephemeral/{noteID}/components.
	Components []core.ActionRow `json:"components,omitempty" binding:"optional"`
	Content    string           `json:"content"`
	CreatedAt  time.Time        `json:"created_at" format:"date-time"`
	// Deleted: the bot deleted the note; drop it.
	Deleted bool         `json:"deleted,omitempty" binding:"optional"`
	Embeds  []core.Embed `json:"embeds" extensions:"x-nullable"`
	// ID identifies the note.
	ID uuid.UUID `json:"id" format:"uuid"`
}

func (InteractionResponse) EventName() string { return events.InteractionResponse }
