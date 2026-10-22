package chat

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Gateway intent bit for the member list (Discord's GUILD_MEMBERS).
const intentGuildMembers = 1 << 1

// MountBot registers the bot API routes (the bot authentication middleware
// must already apply). They reuse the member handlers, so the permission
// checks are the same ones, decided for the bot's roles and overwrites.
// send wraps the routes that post messages (the per-channel send limit).
func (h *Handler) MountBot(r chi.Router, send func(http.Handler) http.Handler) {
	r.Get("/channels", httpx.HandleDiscord(h.botListChannels))
	r.Get("/channels/{channelID}/messages", httpx.HandleDiscord(h.botListMessages))
	r.Get("/channels/{channelID}/messages/{messageID}", httpx.HandleDiscord(h.botGetMessage))
	r.With(send).Post("/channels/{channelID}/messages", httpx.HandleDiscord(h.botCreateMessage))
	r.Patch("/channels/{channelID}/messages/{messageID}", httpx.HandleDiscord(h.botEditMessage))
	r.Delete("/channels/{channelID}/messages/{messageID}", httpx.HandleDiscord(h.botDeleteMessage))
	r.Put("/channels/{channelID}/messages/{messageID}/reactions/{emoji}/@me", httpx.HandleDiscord(h.botAddReaction))
	r.Delete("/channels/{channelID}/messages/{messageID}/reactions/{emoji}/@me", httpx.HandleDiscord(h.botRemoveReaction))
	r.Get("/members", httpx.HandleDiscord(h.botListMembers))
	r.Get("/members/{userID}", httpx.HandleDiscord(h.botGetMember))
}

// botListChannels handles GET /api/bot/v1/channels.
//
// @Summary List the bot's channels
// @Description The categories and channels the bot may view, through its roles and overwrites. A bot without a role sees nothing. A category shows when its own permissions allow it or any of its channels is visible.
// @ID botListChannels
// @Tags Bot API
// @Produce json
// @Security botAuth
// @Success 200 {object} ChannelHierarchy "Visible categories and channels."
// @Failure 401 {object} httpx.DiscordError "Missing or invalid token (0)."
// @Failure 429 {object} httpx.DiscordRateLimit "Rate limited; see retry_after and Retry-After."
// @Failure 500 {object} httpx.DiscordError "Server failure."
// @Router /api/bot/v1/channels [get]
func (h *Handler) botListChannels(w http.ResponseWriter, r *http.Request) error {
	return h.listChannels(w, r)
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
// @Success 200 {array} Message "A page of messages."
// @Failure 400 {object} httpx.DiscordError "Invalid form body (50035)."
// @Failure 401 {object} httpx.DiscordError "Missing or invalid token (0)."
// @Failure 404 {object} httpx.DiscordError "Unknown Channel (10003) or Unknown Message (10008)."
// @Failure 429 {object} httpx.DiscordRateLimit "Rate limited; see retry_after and Retry-After."
// @Failure 500 {object} httpx.DiscordError "Server failure."
// @Router /api/bot/v1/channels/{channelID}/messages [get]
func (h *Handler) botListMessages(w http.ResponseWriter, r *http.Request) error {
	return h.listMessages(w, r)
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
// @Success 200 {object} Message "The message."
// @Failure 400 {object} httpx.DiscordError "Invalid form body (50035)."
// @Failure 401 {object} httpx.DiscordError "Missing or invalid token (0)."
// @Failure 404 {object} httpx.DiscordError "Unknown Channel (10003) or Unknown Message (10008)."
// @Failure 429 {object} httpx.DiscordRateLimit "Rate limited; see retry_after and Retry-After."
// @Failure 500 {object} httpx.DiscordError "Server failure."
// @Router /api/bot/v1/channels/{channelID}/messages/{messageID} [get]
func (h *Handler) botGetMessage(w http.ResponseWriter, r *http.Request) error {
	msgID, ref, err := h.messageInChannel(r)
	if err != nil {
		return err
	}
	if err := h.require(r, ref.ChannelID, perm.ViewChannel); err != nil {
		return notFoundUnlessFailure(err)
	}
	msg, err := GetMessage(r.Context(), h.DB, msgID)
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
	// "roles" is accepted and ignored.
	Parse []string `json:"parse"`
	// Users are user IDs (UUIDs) notified although "users" is not in Parse.
	Users []string `json:"users"`
	Roles []string `json:"roles"`
}

// BotMessageReference is Discord's message_reference.
type BotMessageReference struct {
	// MessageID is the message to quote; it must be in the same channel.
	MessageID uuid.UUID `json:"message_id" format:"uuid"`
}

// BotCreateMessageRequest is the body of posting a message as a bot: the
// subset of Discord's create-message payload Mnema Talk uses. Unknown fields
// (tts, components, flags, ...) are accepted and ignored.
type BotCreateMessageRequest struct {
	// Content is the message text (Markdown), at most 4000 characters.
	Content string `json:"content" maxLength:"4000"`
	// Embeds are rich cards (at most 10).
	Embeds []Embed `json:"embeds"`
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
	Embeds           []InEmbed            `json:"embeds"`
	AllowedMentions  *BotAllowedMentions  `json:"allowed_mentions"`
	MessageReference *BotMessageReference `json:"message_reference"`
	ParentID         *uuid.UUID           `json:"parent_id"`
}

func (h *Handler) checkBotChannelSend(ctx context.Context, user *auth.User, chID uuid.UUID) (*perm.Snapshot, perm.Set, error) {
	snap, err := h.Perms.ForChannel(ctx, chID)
	if err != nil {
		return nil, 0, err
	}
	p := snap.Channel(user.Subject(), chID)
	if err := perm.CheckChannel(p, perm.SendMessages); err != nil {
		return nil, 0, err
	}
	if _, err := ChannelForMessages(ctx, h.DB, chID); err != nil {
		return nil, 0, err
	}
	return snap, p, nil
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

func validateBotPayload(req *rawBotMessage) (string, []Embed, *uuid.UUID, error) {
	content, err := ValidateContent(req.Content, true)
	if err != nil {
		return "", nil, nil, err
	}
	embeds, err := SanitizeEmbeds(req.Embeds)
	if err != nil {
		return "", nil, nil, httpx.ErrInvalidInput(err.Error())
	}
	if content == "" && len(embeds) == 0 {
		return "", nil, nil, httpx.ErrInvalidInput("Cannot send an empty message")
	}
	var reply *uuid.UUID
	if req.MessageReference != nil {
		reply = &req.MessageReference.MessageID
	}
	return content, embeds, reply, nil
}

// botCreateMessage handles POST /api/bot/v1/channels/{channelID}/messages.
//
// @Summary Post a message
// @Description Posts as the bot (author name, avatar and the BOT badge). Needs View channels and Send messages in the channel. At least one of content or embeds. @all and @here notify only when the bot may mention everyone and allowed_mentions.parse asks for "everyone". Limit: 5 messages per 5 seconds per channel, on top of 50 requests per second per bot. For files use the upload route.
// @ID botCreateMessage
// @Tags Bot API
// @Accept json
// @Produce json
// @Security botAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param payload body BotCreateMessageRequest true "Message to post."
// @Success 201 {object} Message "The created message."
// @Failure 400 {object} httpx.DiscordError "Invalid form body (50035) or empty message (50006)."
// @Failure 401 {object} httpx.DiscordError "Missing or invalid token (0)."
// @Failure 403 {object} httpx.DiscordError "Missing Permissions (50013)."
// @Failure 404 {object} httpx.DiscordError "Unknown Channel (10003)."
// @Failure 429 {object} httpx.DiscordRateLimit "Rate limited; see retry_after and Retry-After."
// @Failure 500 {object} httpx.DiscordError "Server failure."
// @Router /api/bot/v1/channels/{channelID}/messages [post]
func (h *Handler) botCreateMessage(w http.ResponseWriter, r *http.Request) error {
	user := auth.UserFrom(r.Context())
	chID, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return err
	}
	snap, p, err := h.checkBotChannelSend(r.Context(), user, chID)
	if err != nil {
		return err
	}
	req, err := decodeRawBotMessage(w, r)
	if err != nil {
		return err
	}
	content, embeds, reply, err := validateBotPayload(req)
	if err != nil {
		return err
	}
	if err := ValidateTarget(r.Context(), h.DB, chID, req.ParentID, reply); err != nil {
		return err
	}
	ctx := WithMentionPolicy(r.Context(), botMentionPolicy(snap, chID, p, req.AllowedMentions))
	id, err := InsertMessage(ctx, h.DB, h.Online, NewMessage{
		ChannelID: chID, UserID: user.ID, Content: content, ParentID: req.ParentID, ReplyToID: reply, Embeds: embeds,
	}, nil)
	if err != nil {
		return err
	}
	return PublishMessage(w, r, h.DB, h.Events, id)
}

func parseAllowedUsers(rawIDs []string) []uuid.UUID {
	var ids []uuid.UUID
	for _, raw := range rawIDs {
		if id, err := uuid.Parse(raw); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

func applyAllowedMentions(policy *MentionPolicy, am *BotAllowedMentions) {
	if am == nil {
		return
	}
	policy.NoEveryone = !slices.Contains(am.Parse, "everyone")
	if !slices.Contains(am.Parse, "users") {
		policy.NoUsers = true
		policy.AllowUsers = parseAllowedUsers(am.Users)
	}
}

// botMentionPolicy is the mention policy of a bot's message: what the bot may
// do in the channel (p), narrowed by its allowed_mentions. The request can
// only narrow the permissions, never widen them.
func botMentionPolicy(snap *perm.Snapshot, ch uuid.UUID, p perm.Set, am *BotAllowedMentions) MentionPolicy {
	policy := MentionPolicy{
		NoEveryone: true,
		CanView:    func(id uuid.UUID) bool { return snap.Channel(perm.Subject{ID: id}, ch).Has(perm.ViewChannel) },
	}
	applyAllowedMentions(&policy, am)
	policy.NoEveryone = policy.NoEveryone || !p.Has(perm.MentionEveryone)
	return policy
}

// botEditMessage handles PATCH /api/bot/v1/channels/{channelID}/messages/{messageID}.
//
// @Summary Edit the bot's own message
// @Description Only the bot's own messages; needs Send messages in the channel. The content may be empty when the message has an attachment.
// @ID botEditMessage
// @Tags Bot API
// @Accept json
// @Produce json
// @Security botAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param messageID path string true "Message ID." Format(uuid)
// @Param request body EditMessageRequest true "New content."
// @Success 200 {object} Message "The updated message."
// @Failure 400 {object} httpx.DiscordError "Invalid form body (50035)."
// @Failure 401 {object} httpx.DiscordError "Missing or invalid token (0)."
// @Failure 403 {object} httpx.DiscordError "Missing Permissions (50013), or not the bot's own message."
// @Failure 404 {object} httpx.DiscordError "Unknown Channel (10003) or Unknown Message (10008)."
// @Failure 429 {object} httpx.DiscordRateLimit "Rate limited; see retry_after and Retry-After."
// @Failure 500 {object} httpx.DiscordError "Server failure."
// @Router /api/bot/v1/channels/{channelID}/messages/{messageID} [patch]
func (h *Handler) botEditMessage(w http.ResponseWriter, r *http.Request) error {
	return h.editMessageWith(w, r, h.botEditContext)
}

// botEditContext is the mention context of a bot's edit: the policy of its
// sends (botMentionPolicy), so an edit never reaches more than a send does.
func (h *Handler) botEditContext(r *http.Request, ch uuid.UUID, need perm.Set) (context.Context, error) {
	snap, err := h.Perms.ForChannel(r.Context(), ch)
	if err != nil {
		return nil, err
	}
	p := snap.Channel(auth.UserFrom(r.Context()).Subject(), ch)
	if err := snap.Deny(auth.UserFrom(r.Context()).Subject(), p, need); err != nil {
		return nil, err
	}
	return WithMentionPolicy(r.Context(), botMentionPolicy(snap, ch, p, nil)), nil
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
func (h *Handler) botDeleteMessage(w http.ResponseWriter, r *http.Request) error {
	return h.deleteMessage(w, r)
}
