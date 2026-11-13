package botapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Gateway intent bit for the member list (Discord's GUILD_MEMBERS).
const intentGuildMembers = 1 << 1

// MountBot registers the bot API routes (the bot authentication middleware
// must already apply). They reuse the member handlers, so the permission
// checks are the same ones, decided for the bot's roles and overwrites.
// send wraps the routes that post messages (the per-channel send limit).
func (a *API) MountBot(r chi.Router, send func(http.Handler) http.Handler) {
	a.send = send
	r.Get("/channels", httpx.HandleDiscord(a.botListChannels))
	r.Get("/channels/{channelID}/messages", httpx.HandleDiscord(a.botListMessages))
	r.Get("/channels/{channelID}/messages/{messageID}", httpx.HandleDiscord(a.botGetMessage))
	r.With(send).Post("/channels/{channelID}/messages", httpx.HandleDiscord(a.botCreateMessage))
	r.Patch("/channels/{channelID}/messages/{messageID}", httpx.HandleDiscord(a.botEditMessage))
	r.Delete("/channels/{channelID}/messages/{messageID}", httpx.HandleDiscord(a.botDeleteMessage))
	r.Put("/channels/{channelID}/messages/{messageID}/reactions/{emoji}/@me", httpx.HandleDiscord(a.botAddReaction))
	r.Delete("/channels/{channelID}/messages/{messageID}/reactions/{emoji}/@me", httpx.HandleDiscord(a.botRemoveReaction))
	r.Get("/members", httpx.HandleDiscord(a.botListMembers))
	r.Get("/members/{userID}", httpx.HandleDiscord(a.botGetMember))
}

// botListChannels handles GET /api/bot/v1/channels.
//
// @Summary List the bot's channels
// @Description The categories and channels the bot may view, through its roles and overwrites. A bot without a role sees nothing. A category shows when its own permissions allow it or any of its channels is visible.
// @ID botListChannels
// @Tags Bot API
// @Produce json
// @Security botAuth
// @Success 200 {object} channels.ChannelHierarchy "Visible categories and channels."
// @Failure 401 {object} httpx.DiscordError "Missing or invalid token (0)."
// @Failure 429 {object} httpx.DiscordRateLimit "Rate limited; see retry_after and Retry-After."
// @Failure 500 {object} httpx.DiscordError "Server failure."
// @Router /api/bot/v1/channels [get]
func (a *API) botListChannels(w http.ResponseWriter, r *http.Request) error {
	return a.Channels.ListChannels(w, r)
}

// botListMessages handles GET /api/bot/v1/channels/{channelID}/messages.
//
// @Summary List channel messages
// @Description Top-level messages only, oldest first within the page. Without an anchor returns the newest page. Use at most one of before, after, around. Needs View channels in the channel; a channel the bot cannot view answers 404 like a missing one.
// @ID botListMessages
// @Tags Bot API
// @Produce json
// @Security botAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param limit query int false "Page size (1 to 100)." minimum(1) maximum(100) default(50)
// @Param before query string false "Messages older than this message." Format(uuid)
// @Param after query string false "Messages newer than this message." Format(uuid)
// @Param around query string false "Messages around (and including) this message." Format(uuid)
// @Success 200 {array} core.Message "A page of messages."
// @Failure 400 {object} httpx.DiscordError "Invalid form body (50035)."
// @Failure 401 {object} httpx.DiscordError "Missing or invalid token (0)."
// @Failure 404 {object} httpx.DiscordError "Unknown Channel (10003) or Unknown Message (10008)."
// @Failure 429 {object} httpx.DiscordRateLimit "Rate limited; see retry_after and Retry-After."
// @Failure 500 {object} httpx.DiscordError "Server failure."
// @Router /api/bot/v1/channels/{channelID}/messages [get]
func (a *API) botListMessages(w http.ResponseWriter, r *http.Request) error {
	return a.Messages.ListMessages(w, r)
}

// botGetMessage handles GET /api/bot/v1/channels/{channelID}/messages/{messageID}.
//
// @Summary Get a message
// @Description Needs View channels in the channel.
// @ID botGetMessage
// @Tags Bot API
// @Produce json
// @Security botAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param messageID path string true "Message ID." Format(uuid)
// @Success 200 {object} core.Message "The message."
// @Failure 400 {object} httpx.DiscordError "Invalid form body (50035)."
// @Failure 401 {object} httpx.DiscordError "Missing or invalid token (0)."
// @Failure 404 {object} httpx.DiscordError "Unknown Channel (10003) or Unknown Message (10008)."
// @Failure 429 {object} httpx.DiscordRateLimit "Rate limited; see retry_after and Retry-After."
// @Failure 500 {object} httpx.DiscordError "Server failure."
// @Router /api/bot/v1/channels/{channelID}/messages/{messageID} [get]
func (a *API) botGetMessage(w http.ResponseWriter, r *http.Request) error {
	msgID, ref, err := a.Messages.MessageInChannel(r)
	if err != nil {
		return err
	}
	if err := a.Guard.Require(r, ref.ChannelID, perm.ViewChannel); err != nil {
		return core.NotFoundUnlessFailure(err)
	}
	msg, err := messages.GetMessage(r.Context(), a.DB, msgID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, msg)
	return nil
}

// BotAllowedMentions is Discord's allowed_mentions object.
type BotAllowedMentions struct {
	// Parse lists the kinds of mentions that notify: "users" and "everyone"
	// (@all and @here, which also need the Mention everyone permission).
	// "roles" lets @Role notify the role's members (the role must be
	// mentionable, or the bot must hold Mention everyone).
	Parse []string `json:"parse"`
	// Users are user IDs (UUIDs) notified although "users" is not in Parse.
	Users []string `json:"users"`
	// Roles are role IDs (UUIDs) notified although "roles" is not in Parse.
	Roles []string `json:"roles"`
}

// BotMessageReference is Discord's message_reference.
type BotMessageReference struct {
	// MessageID is the message to quote; it must be in the same channel.
	MessageID uuid.UUID `json:"message_id" format:"uuid"`
}

// BotCreateMessageRequest is the body of posting a message as a bot: the
// subset of Discord's create-message payload Mnema Talk uses. Unknown fields
// (tts, flags, ...) are accepted and ignored.
type BotCreateMessageRequest struct {
	// Content is the message text (Markdown), at most 4000 characters.
	Content string `json:"content" maxLength:"4000"`
	// Embeds are rich cards (at most 10).
	Embeds []core.Embed `json:"embeds"`
	// Components are buttons and select menus in up to 5 action rows
	// (doc/bots.md, Message components).
	Components []core.ActionRow `json:"components" binding:"optional"`
	// AllowedMentions limits who is notified. Default: members mentioned by
	// @username are, @all and @here are not.
	AllowedMentions *BotAllowedMentions `json:"allowed_mentions"`
	// MessageReference quotes a message of the same channel.
	MessageReference *BotMessageReference `json:"message_reference"`
	// ParentID posts into the thread of a root message of the channel.
	ParentID *uuid.UUID `json:"parent_id" format:"uuid" binding:"optional" extensions:"x-nullable"`
}

type rawBotMessage struct {
	Content          string               `json:"content"`
	Embeds           []messages.InEmbed   `json:"embeds"`
	Components       []core.ActionRow     `json:"components"`
	AllowedMentions  *BotAllowedMentions  `json:"allowed_mentions"`
	MessageReference *BotMessageReference `json:"message_reference"`
	ParentID         *uuid.UUID           `json:"parent_id"`
}

// ChannelPermsOf is what user may do in a channel, with the snapshot it was
// decided on.
func ChannelPermsOf(ctx context.Context, ps *perm.Service, user *auth.User, chID uuid.UUID) (*perm.Snapshot, perm.Set, error) {
	snap, err := ps.ForChannel(ctx, chID)
	if err != nil {
		return nil, 0, err
	}
	return snap, snap.Channel(user.Subject(), chID), nil
}

// CheckBotSend requires that user may post in the channel and that it exists.
// Posting by a bot and a bot's reply to a command both pass through it.
func CheckBotSend(ctx context.Context, ps *perm.Service, p *db.Pool, user *auth.User, chID uuid.UUID) (*perm.Snapshot, perm.Set, error) {
	snap, set, err := ChannelPermsOf(ctx, ps, user, chID)
	if err != nil {
		return nil, 0, err
	}
	if err := perm.CheckChannel(set, perm.SendMessages); err != nil {
		return nil, 0, err
	}
	if _, err := messages.ChannelForMessages(ctx, p, chID); err != nil {
		return nil, 0, err
	}
	return snap, set, nil
}

// SendContext carries what the bot's permissions in the channel (p, from
// CheckBotSend) mean for a message it posts there: like a member, a bot that
// may manage messages is not held back by slow mode or a locked post.
func SendContext(ctx context.Context, p perm.Set) context.Context {
	return messages.WithSlowModeExempt(ctx, p.Has(perm.ManageMessages))
}

func decodeRawBotMessage(w http.ResponseWriter, r *http.Request) (*rawBotMessage, error) {
	var req rawBotMessage
	httpx.SetJSONReadDeadline(w)
	r.Body = http.MaxBytesReader(w, r.Body, httpx.DefaultMaxBody)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return nil, httpx.ErrPayloadTooLarge("request body too large")
		}
		return nil, httpx.ErrInvalidInput("the body is not a valid JSON object")
	}
	return &req, nil
}

// ValidateBotMessage checks the content and embeds a bot sends (a message or
// a response to a command): the same rules, one message needs one of them or
// components (hasComponents).
func ValidateBotMessage(rawContent string, rawEmbeds []messages.InEmbed, hasComponents bool) (string, []core.Embed, error) {
	return messages.ValidateBotMessage(rawContent, rawEmbeds, hasComponents)
}

// botCreateMessage handles POST /api/bot/v1/channels/{channelID}/messages.
//
// @Summary Post a message
// @Description Posts as the bot (author name, avatar and the BOT badge). Needs View channels and Send messages in the channel. At least one of content, embeds or components (buttons and select menus, stored with the message and part of message_create). @all and @here notify only when the bot may mention everyone and allowed_mentions.parse asks for "everyone". Limit: 5 messages per 5 seconds per channel, on top of 50 requests per second per bot. For files use the upload route.
// @ID botCreateMessage
// @Tags Bot API
// @Accept json
// @Produce json
// @Security botAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param payload body botapi.BotCreateMessageRequest true "Message to post."
// @Success 201 {object} core.Message "The created message."
// @Failure 400 {object} httpx.DiscordError "Invalid form body (50035) or empty message (50006)."
// @Failure 401 {object} httpx.DiscordError "Missing or invalid token (0)."
// @Failure 403 {object} httpx.DiscordError "Missing Permissions (50013)."
// @Failure 404 {object} httpx.DiscordError "Unknown Channel (10003)."
// @Failure 429 {object} httpx.DiscordRateLimit "Rate limited; see retry_after and Retry-After."
// @Failure 500 {object} httpx.DiscordError "Server failure."
// @Router /api/bot/v1/channels/{channelID}/messages [post]
func (a *API) botCreateMessage(w http.ResponseWriter, r *http.Request) error {
	actor := messages.BotActor(auth.UserFrom(r.Context()))
	chID, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return err
	}
	req, err := decodeRawBotMessage(w, r)
	if err != nil {
		// A bot that may not post here is told so, not what is wrong with its body.
		if refusal := a.Messages.Posts.Refusal(r.Context(), actor, chID); refusal != nil {
			return refusal
		}
		return err
	}
	comps, err := core.ValidateComponents(req.Components)
	if err != nil {
		if refusal := a.Messages.Posts.Refusal(r.Context(), actor, chID); refusal != nil {
			return refusal
		}
		return err
	}
	cmd := messages.PostCommand{
		ChannelID: chID, Content: req.Content, Embeds: req.Embeds, ParentID: req.ParentID,
		AllowedMentions: (*messages.AllowedMentions)(req.AllowedMentions), HasComponents: len(comps) > 0,
	}
	if len(comps) > 0 {
		cmd.InTx = func(tx pgx.Tx, id uuid.UUID) error { return core.SaveComponents(r.Context(), tx, id, comps) }
	}
	if req.MessageReference != nil {
		cmd.ReplyToID = &req.MessageReference.MessageID
	}
	posted, err := a.Messages.Posts.Post(r.Context(), actor, cmd)
	if err != nil {
		return err
	}
	messages.WritePosted(w, posted)
	return nil
}

// botDeleteMessage handles DELETE /api/bot/v1/channels/{channelID}/messages/{messageID}.
//
// @Summary Delete a message
// @Description The bot's own messages, or any message when the bot holds Manage messages in the channel. Also deletes the message's thread replies and attachments.
// @ID botDeleteMessage
// @Tags Bot API
// @Security botAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param messageID path string true "Message ID." Format(uuid)
// @Success 204 "Deleted."
// @Failure 400 {object} httpx.DiscordError "Invalid form body (50035)."
// @Failure 401 {object} httpx.DiscordError "Missing or invalid token (0)."
// @Failure 403 {object} httpx.DiscordError "Missing Permissions (50013)."
// @Failure 404 {object} httpx.DiscordError "Unknown Channel (10003) or Unknown Message (10008)."
// @Failure 429 {object} httpx.DiscordRateLimit "Rate limited; see retry_after and Retry-After."
// @Failure 500 {object} httpx.DiscordError "Server failure."
// @Router /api/bot/v1/channels/{channelID}/messages/{messageID} [delete]
func (a *API) botDeleteMessage(w http.ResponseWriter, r *http.Request) error {
	return a.Messages.DeleteMessage(w, r)
}
