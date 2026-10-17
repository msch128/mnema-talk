package bot

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

const (
	// interactionTTL is how long a bot may answer an interaction.
	interactionTTL = 15 * time.Minute
	// maxResponses bounds the messages one interaction can produce.
	maxResponses = 5
	// maxPending bounds the interactions kept in memory.
	maxPending = 10000
	// invokesPerMinute bounds how often one member can run commands.
	invokesPerMinute = 20
	// flagEphemeral in a response's flags makes it visible to the invoker only.
	flagEphemeral = 64
)

// Interaction types, with Discord's numbers.
const (
	InteractionApplicationCommand = 2
	// Callback types.
	CallbackMessage  = 4
	CallbackDeferred = 5
)

// Deliverer hands an interaction to a connected bot (the gateway).
type Deliverer interface {
	// DeliverInteraction sends INTERACTION_CREATE to one live session of the
	// bot and reports whether there was one.
	DeliverInteraction(botID uuid.UUID, d json.RawMessage) bool
}

type interaction struct {
	hash      [32]byte
	botID     uuid.UUID
	botUser   uuid.UUID
	botName   string
	avatar    string
	channel   uuid.UUID
	invoker   uuid.UUID
	created   time.Time
	responses int
	called    bool
}

type interactionStore struct {
	mu  sync.Mutex
	m   map[uuid.UUID]*interaction
	now func() time.Time
}

func (s *interactionStore) add(id uuid.UUID, in *interaction) (token string, ok bool) {
	raw := make([]byte, 24)
	_, _ = rand.Read(raw)
	token = base64.RawURLEncoding.EncodeToString(raw)
	in.hash = sha256.Sum256([]byte(token))
	in.created = s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.m) >= maxPending {
		for k, v := range s.m {
			if in.created.Sub(v.created) > interactionTTL {
				delete(s.m, k)
			}
		}
		if len(s.m) >= maxPending {
			return "", false
		}
	}
	s.m[id] = in
	return token, true
}

var errUnknownInteraction = httpx.ErrNotFound("unknown interaction")

// claim finds an interaction for its bot and token and counts one response.
func (s *interactionStore) claim(id uuid.UUID, token string, botID uuid.UUID, callback bool) (*interaction, error) {
	sum := sha256.Sum256([]byte(token))
	s.mu.Lock()
	defer s.mu.Unlock()
	in := s.m[id]
	if in == nil || in.botID != botID || subtle.ConstantTimeCompare(in.hash[:], sum[:]) != 1 {
		return nil, errUnknownInteraction
	}
	if s.now().Sub(in.created) > interactionTTL {
		delete(s.m, id)
		return nil, errUnknownInteraction
	}
	if callback && in.called {
		return nil, httpx.ErrConflict("the interaction already has a response")
	}
	if !callback && !in.called {
		return nil, httpx.ErrConflict("respond to the interaction first")
	}
	if in.responses >= maxResponses {
		return nil, httpx.ErrConflict(fmt.Sprintf("an interaction takes at most %d responses", maxResponses))
	}
	if callback {
		in.called = true
	}
	in.responses++
	return in, nil
}

// Commands serves slash commands: the bot API to register them and answer
// interactions, and the member routes to list and run them.
type Commands struct {
	DB     *db.Pool
	Perms  *perm.Service
	Events events.Publisher
	Online chat.OnlineSource
	// Deliver sends interactions to bots; required.
	Deliver Deliverer

	store   *interactionStore
	invokes *httpx.RateLimiter
}

// NewCommands returns Commands with its state.
func NewCommands(c Commands) *Commands {
	c.store = &interactionStore{m: map[uuid.UUID]*interaction{}, now: time.Now}
	c.invokes = httpx.NewRateLimiter(invokesPerMinute, time.Minute).Named("bot_invoke")
	return &c
}

// MountMember registers the routes for members (RequireUser applies).
func (c *Commands) MountMember(r chi.Router) {
	r.Get("/channels/{channelID}/commands", httpx.Handle(c.listForChannel))
	r.Post("/channels/{channelID}/interactions", httpx.Handle(c.invoke))
}

// MountBot registers the bot API routes (the bot middleware applies).
func (c *Commands) MountBot(r chi.Router) {
	r.Get("/commands", httpx.HandleDiscord(c.botList))
	r.Put("/commands", httpx.HandleDiscord(c.botReplace))
	r.Post("/commands", httpx.HandleDiscord(c.botCreate))
	r.Delete("/commands/{commandID}", httpx.HandleDiscord(c.botDelete))
	r.Post("/interactions/{interactionID}/{token}/callback", httpx.HandleDiscord(c.callback))
	r.Post("/interactions/{interactionID}/{token}/followups", httpx.HandleDiscord(c.followup))
}

// ChannelCommand is a command as the composer lists it.
type ChannelCommand struct {
	Command
	BotName      string `json:"bot_name"`
	BotAvatarURL string `json:"bot_avatar_url,omitempty" binding:"optional"`
}

// listForChannel handles GET /api/channels/{channelID}/commands.
//
// @Summary List the slash commands of a channel
// @Description The commands of bots that can view and send in the channel (through their roles and overwrites), not disabled by an admin, for the composer's autocomplete. Needs View channels.
// @ID listChannelCommands
// @Tags Commands
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Success 200 {array} ChannelCommand "Commands by name."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such channel, or it is hidden from the caller."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/channels/{channelID}/commands [get]
func (c *Commands) listForChannel(w http.ResponseWriter, r *http.Request) error {
	ch, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return err
	}
	snap, err := c.Perms.ForChannel(r.Context(), ch)
	if err != nil {
		return err
	}
	if err := perm.CheckChannel(snap.Channel(auth.UserFrom(r.Context()).Subject(), ch), perm.ViewChannel); err != nil {
		return err
	}
	type found struct {
		Command
		BotUser uuid.UUID
		BotName string
		Avatar  *string
	}
	var list []found
	rows, err := c.DB.Query(r.Context(), `
		SELECT c.id, c.bot_id, c.name, c.description, c.options, c.disabled, c.created_at, b.user_id, b.name, u.avatar_s3_key
		FROM bot_commands c JOIN bots b ON b.id = c.bot_id JOIN users u ON u.id = b.user_id
		WHERE NOT c.disabled AND NOT b.disabled ORDER BY c.name, b.name`)
	if err == nil {
		list, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (found, error) {
			var x found
			err := r.Scan(&x.ID, &x.BotID, &x.Name, &x.Description, &x.Options, &x.Disabled, &x.CreatedAt, &x.BotUser, &x.BotName, &x.Avatar)
			return x, err
		})
	}
	if err != nil {
		return fmt.Errorf("list channel commands: %w", err)
	}
	out := []ChannelCommand{}
	for _, f := range list {
		if snap.Channel(perm.Subject{ID: f.BotUser}, ch).Has(perm.ViewChannel | perm.SendMessages) {
			out = append(out, ChannelCommand{Command: f.Command, BotName: f.BotName, BotAvatarURL: auth.AvatarURL(f.Avatar)})
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

// InvokeRequest is the body of running a command.
type InvokeRequest struct {
	CommandID uuid.UUID `json:"command_id" format:"uuid"`
	// Options maps option names to values: a string, a whole number, a
	// boolean, or the ID of a member or channel.
	Options map[string]any `json:"options"`
}

// Invoked is the answer of running a command.
type Invoked struct {
	// InteractionID identifies the run; the bot answers it.
	InteractionID uuid.UUID `json:"interaction_id" format:"uuid"`
}

// invoke handles POST /api/channels/{channelID}/interactions.
//
// @Summary Run a slash command
// @Description Sends an interaction to the command's bot over the gateway; the bot answers with a message in the channel, or one only the caller sees (an ephemeral response, delivered as the interaction_response WebSocket event). Needs Send messages in the channel; the bot must still be able to view and send there. 409 when the bot is not connected. At most 20 a minute per member.
// @ID invokeCommand
// @Tags Commands
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param body body InvokeRequest true "The command and its options."
// @Success 202 {object} Invoked "Delivered to the bot."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): unknown or ill-typed options."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing permission."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such channel or command, or the command is not available here."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the bot is not connected."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/channels/{channelID}/interactions [post]
func (c *Commands) invoke(w http.ResponseWriter, r *http.Request) error {
	user := auth.UserFrom(r.Context())
	ch, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return err
	}
	snap, err := c.Perms.ForChannel(r.Context(), ch)
	if err != nil {
		return err
	}
	if err := perm.CheckChannel(snap.Channel(user.Subject(), ch), perm.SendMessages); err != nil {
		return err
	}
	var req InvokeRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	cmd, botUser, botDisabled, err := getCommand(r.Context(), c.DB, req.CommandID)
	if err != nil {
		return err
	}
	if cmd.Disabled || botDisabled || !snap.Channel(perm.Subject{ID: botUser}, ch).Has(perm.ViewChannel|perm.SendMessages) {
		return errCommandNotFound
	}
	if ok, retry := c.invokes.Allow(user.ID.String()); !ok {
		w.Header().Set("Retry-After", fmt.Sprint(int(retry.Seconds())+1))
		return httpx.ErrRateLimited()
	}
	opts, err := c.checkOptions(r, snap, user, ch, cmd, req.Options)
	if err != nil {
		return err
	}
	var name string
	var avatar *string
	if err := c.DB.QueryRow(r.Context(), `SELECT b.name, u.avatar_s3_key FROM bots b JOIN users u ON u.id = b.user_id WHERE b.id = $1`, cmd.BotID).Scan(&name, &avatar); err != nil {
		return err
	}
	id := uuid.New()
	token, ok := c.store.add(id, &interaction{botID: cmd.BotID, botUser: botUser, botName: name, avatar: auth.AvatarURL(avatar), channel: ch, invoker: user.ID})
	if !ok {
		return httpx.ErrRateLimited()
	}
	payload, _ := json.Marshal(map[string]any{
		"id": id, "token": token, "type": InteractionApplicationCommand, "channel_id": ch, "application_id": cmd.BotID,
		"data":   map[string]any{"id": cmd.ID, "name": cmd.Name, "options": opts},
		"member": map[string]any{"user": map[string]any{"id": user.ID, "username": user.Username, "display_name": user.DisplayName, "avatar_url": user.AvatarURL}},
	})
	if !c.Deliver.DeliverInteraction(cmd.BotID, payload) {
		c.store.mu.Lock()
		delete(c.store.m, id)
		c.store.mu.Unlock()
		return httpx.ErrConflict("the bot is not connected")
	}
	httpx.WriteJSON(w, http.StatusAccepted, Invoked{InteractionID: id})
	return nil
}

// checkOptions validates the values against the command's options.
func (c *Commands) checkOptions(r *http.Request, snap *perm.Snapshot, user *auth.User, ch uuid.UUID, cmd Command, given map[string]any) ([]map[string]any, error) {
	declared := map[string]Option{}
	for _, o := range cmd.Options {
		declared[o.Name] = o
	}
	for name := range given {
		if _, ok := declared[name]; !ok {
			return nil, httpx.ErrInvalidInput("unknown option: " + name)
		}
	}
	out := []map[string]any{}
	for _, o := range cmd.Options {
		v, present := given[o.Name]
		if !present || v == nil {
			if o.Required {
				return nil, httpx.ErrInvalidInput("missing option: " + o.Name)
			}
			continue
		}
		val, err := c.checkValue(r, snap, user, ch, o, v)
		if err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"name": o.Name, "type": o.Type, "value": val})
	}
	return out, nil
}

func (c *Commands) checkValue(r *http.Request, snap *perm.Snapshot, user *auth.User, ch uuid.UUID, o Option, v any) (any, error) {
	bad := func(what string) (any, error) {
		return nil, httpx.ErrInvalidInput(fmt.Sprintf("option %s must be %s", o.Name, what))
	}
	switch o.Type {
	case OptionString:
		s, ok := v.(string)
		if !ok {
			return bad("text")
		}
		return httpx.CleanText(o.Name, s, MaxOptionValue, o.Required)
	case OptionInteger:
		f, ok := v.(float64)
		if !ok || f != math.Trunc(f) || math.Abs(f) > 1<<53 {
			return bad("a whole number")
		}
		return int64(f), nil
	case OptionBoolean:
		b, ok := v.(bool)
		if !ok {
			return bad("true or false")
		}
		return b, nil
	case OptionUser, OptionChannel:
		s, ok := v.(string)
		id, err := uuid.Parse(s)
		if !ok || err != nil {
			return bad("an ID")
		}
		if o.Type == OptionChannel {
			if !snap.Channel(user.Subject(), id).Has(perm.ViewChannel) {
				return bad("a channel you can see")
			}
			return id, nil
		}
		var ok2 bool
		if err := c.DB.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1 AND NOT is_bot AND disabled_at IS NULL)`, id).Scan(&ok2); err != nil {
			return nil, err
		}
		if !ok2 || !snap.Channel(perm.Subject{ID: id}, ch).Has(perm.ViewChannel) {
			return bad("a member of this channel")
		}
		return id, nil
	}
	return bad("valid")
}

// BotCommands is a list of definitions.
type BotCommands []CommandInput

// botList handles GET /api/bot/v1/commands.
//
// @Summary List the bot's commands
// @ID botListCommands
// @Tags Bot API
// @Produce json
// @Security botAuth
// @Success 200 {array} Command "The bot's commands."
// @Failure 401 {object} httpx.DiscordError "Missing or invalid token (0)."
// @Failure 429 {object} httpx.DiscordRateLimit "Rate limited; see retry_after and Retry-After."
// @Router /api/bot/v1/commands [get]
func (c *Commands) botList(w http.ResponseWriter, r *http.Request) error {
	list, err := ListCommands(r.Context(), c.DB, auth.BotFrom(r.Context()).BotID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, list)
	return nil
}

func cleanAll(in BotCommands) (BotCommands, error) {
	out := make(BotCommands, len(in))
	for i, d := range in {
		var err error
		if out[i], err = d.Clean(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// botReplace handles PUT /api/bot/v1/commands.
//
// @Summary Replace the bot's commands
// @Description Bulk overwrite as in Discord: adds and updates the given commands by name and removes the others. An admin's disabled flag on a command stays. At most 50 commands, 10 options each. Members see the commands in every channel the bot can view and send in.
// @ID botReplaceCommands
// @Tags Bot API
// @Accept json
// @Produce json
// @Security botAuth
// @Param body body BotCommands true "The complete list of commands."
// @Success 200 {array} Command "The bot's commands."
// @Failure 400 {object} httpx.DiscordError "Invalid form body (50035)."
// @Failure 401 {object} httpx.DiscordError "Missing or invalid token (0)."
// @Failure 429 {object} httpx.DiscordRateLimit "Rate limited; see retry_after and Retry-After."
// @Router /api/bot/v1/commands [put]
func (c *Commands) botReplace(w http.ResponseWriter, r *http.Request) error {
	var in BotCommands
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	clean, err := cleanAll(in)
	if err != nil {
		return err
	}
	list, err := ReplaceCommands(r.Context(), c.DB, auth.BotFrom(r.Context()).BotID, clean)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, list)
	return nil
}

// botCreate handles POST /api/bot/v1/commands.
//
// @Summary Register a command
// @Description Creates the command, or updates the one with the same name (keeping an admin's disabled flag).
// @ID botCreateCommand
// @Tags Bot API
// @Accept json
// @Produce json
// @Security botAuth
// @Param body body CommandInput true "The command."
// @Success 200 {object} Command "The command."
// @Failure 400 {object} httpx.DiscordError "Invalid form body (50035)."
// @Failure 401 {object} httpx.DiscordError "Missing or invalid token (0)."
// @Failure 409 {object} httpx.DiscordError "The bot has the maximum number of commands."
// @Failure 429 {object} httpx.DiscordRateLimit "Rate limited; see retry_after and Retry-After."
// @Router /api/bot/v1/commands [post]
func (c *Commands) botCreate(w http.ResponseWriter, r *http.Request) error {
	var in CommandInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	in, err := in.Clean()
	if err != nil {
		return err
	}
	cmd, err := UpsertCommand(r.Context(), c.DB, auth.BotFrom(r.Context()).BotID, in)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, cmd)
	return nil
}

// botDelete handles DELETE /api/bot/v1/commands/{commandID}.
//
// @Summary Delete a command
// @ID botDeleteCommand
// @Tags Bot API
// @Security botAuth
// @Param commandID path string true "Command ID." Format(uuid)
// @Success 204 "Deleted."
// @Failure 400 {object} httpx.DiscordError "Invalid form body (50035)."
// @Failure 401 {object} httpx.DiscordError "Missing or invalid token (0)."
// @Failure 404 {object} httpx.DiscordError "Unknown command."
// @Failure 429 {object} httpx.DiscordRateLimit "Rate limited; see retry_after and Retry-After."
// @Router /api/bot/v1/commands/{commandID} [delete]
func (c *Commands) botDelete(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "commandID")
	if err != nil {
		return err
	}
	if err := DeleteCommand(r.Context(), c.DB, auth.BotFrom(r.Context()).BotID, id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

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
