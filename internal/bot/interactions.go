package bot

import (
	"encoding/json"
	"fmt"
	"net/http"
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
