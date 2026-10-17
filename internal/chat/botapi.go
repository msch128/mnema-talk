package chat

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
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
	snap, err := h.Perms.ForChannel(r.Context(), chID)
	if err != nil {
		return err
	}
	p := snap.Channel(user.Subject(), chID)
	if err := perm.CheckChannel(p, perm.SendMessages); err != nil {
		return err
	}
	if _, err := ChannelForMessages(r.Context(), h.DB, chID); err != nil {
		return err
	}
	var req rawBotMessage
	httpx.SetJSONReadDeadline(w)
	r.Body = http.MaxBytesReader(w, r.Body, httpx.DefaultMaxBody)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return httpx.ErrPayloadTooLarge("request body too large")
		}
		return httpx.ErrInvalidInput("the body is not a valid JSON object")
	}
	content, err := ValidateContent(req.Content, true)
	if err != nil {
		return err
	}
	embeds, err := SanitizeEmbeds(req.Embeds)
	if err != nil {
		return httpx.ErrInvalidInput(err.Error())
	}
	if content == "" && len(embeds) == 0 {
		return httpx.ErrInvalidInput("Cannot send an empty message")
	}
	var reply *uuid.UUID
	if req.MessageReference != nil {
		reply = &req.MessageReference.MessageID
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

// botMentionPolicy is the mention policy of a bot's message: what the bot may
// do in the channel (p), narrowed by its allowed_mentions. The request can
// only narrow the permissions, never widen them.
func botMentionPolicy(snap *perm.Snapshot, ch uuid.UUID, p perm.Set, am *BotAllowedMentions) MentionPolicy {
	policy := MentionPolicy{
		NoEveryone: true,
		CanView:    func(id uuid.UUID) bool { return snap.Channel(perm.Subject{ID: id}, ch).Has(perm.ViewChannel) },
	}
	if am != nil {
		policy.NoEveryone = !slices.Contains(am.Parse, "everyone")
		if !slices.Contains(am.Parse, "users") {
			policy.NoUsers = true
			for _, raw := range am.Users {
				if id, err := uuid.Parse(raw); err == nil {
					policy.AllowUsers = append(policy.AllowUsers, id)
				}
			}
		}
	}
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
	return h.editMessage(w, r)
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

// setBotReaction adds (want) or removes the bot's reaction, idempotently.
func (h *Handler) setBotReaction(w http.ResponseWriter, r *http.Request, want bool) error {
	user := auth.UserFrom(r.Context())
	msgID, ref, err := h.messageInChannel(r)
	if err != nil {
		return err
	}
	// The router hands over the segment decoded or not, depending on the bytes
	// in it: decode what still looks encoded.
	raw := chi.URLParam(r, "emoji")
	if decoded, err := url.PathUnescape(raw); err == nil {
		raw = decoded
	}
	emoji, err := ValidateEmoji(raw)
	if err != nil {
		return err
	}
	p, err := h.channelPerms(r, ref.ChannelID)
	if err != nil {
		return err
	}
	if !p.Has(perm.ViewChannel) {
		return errMessageNotFound
	}
	var has bool
	if err := h.DB.QueryRow(r.Context(),
		`SELECT EXISTS (SELECT 1 FROM message_reactions WHERE message_id = $1 AND user_id = $2 AND emoji = $3)`,
		msgID, user.ID, emoji).Scan(&has); err != nil {
		return err
	}
	if has != want {
		reactions, err := ToggleReactionAs(r.Context(), h.DB, msgID, user.ID, emoji, p.Has(perm.AddReactions))
		if err != nil {
			return err
		}
		h.Events.BroadcastChannel(ref.ChannelID, "message_reaction", ReactionResult{MessageID: msgID, ChannelID: ref.ChannelID, Reactions: reactions})
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// botAddReaction handles PUT /api/bot/v1/channels/{channelID}/messages/{messageID}/reactions/{emoji}/@me.
//
// @Summary Add the bot's reaction
// @Description Idempotent. The emoji is URL-encoded. Needs Add reactions in the channel. A user may add at most 20 different emoji to one message, and a message carries at most 50 different emoji (409).
// @ID botAddReaction
// @Tags Bot API
// @Security botAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param messageID path string true "Message ID." Format(uuid)
// @Param emoji path string true "The emoji, URL-encoded."
// @Success 204 "Added, or already there."
// @Failure 400 {object} httpx.DiscordError "Invalid form body (50035)."
// @Failure 401 {object} httpx.DiscordError "Missing or invalid token (0)."
// @Failure 403 {object} httpx.DiscordError "Missing Permissions (50013)."
// @Failure 404 {object} httpx.DiscordError "Unknown Channel (10003) or Unknown Message (10008)."
// @Failure 409 {object} httpx.DiscordError "Too many reactions."
// @Failure 429 {object} httpx.DiscordRateLimit "Rate limited; see retry_after and Retry-After."
// @Failure 500 {object} httpx.DiscordError "Server failure."
// @Router /api/bot/v1/channels/{channelID}/messages/{messageID}/reactions/{emoji}/at_me [put]
func (h *Handler) botAddReaction(w http.ResponseWriter, r *http.Request) error {
	return h.setBotReaction(w, r, true)
}

// botRemoveReaction handles DELETE /api/bot/v1/channels/{channelID}/messages/{messageID}/reactions/{emoji}/@me.
//
// @Summary Remove the bot's reaction
// @Description Idempotent. Taking a reaction back always works.
// @ID botRemoveReaction
// @Tags Bot API
// @Security botAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param messageID path string true "Message ID." Format(uuid)
// @Param emoji path string true "The emoji, URL-encoded."
// @Success 204 "Removed, or not there."
// @Failure 400 {object} httpx.DiscordError "Invalid form body (50035)."
// @Failure 401 {object} httpx.DiscordError "Missing or invalid token (0)."
// @Failure 404 {object} httpx.DiscordError "Unknown Channel (10003) or Unknown Message (10008)."
// @Failure 429 {object} httpx.DiscordRateLimit "Rate limited; see retry_after and Retry-After."
// @Failure 500 {object} httpx.DiscordError "Server failure."
// @Router /api/bot/v1/channels/{channelID}/messages/{messageID}/reactions/{emoji}/at_me [delete]
func (h *Handler) botRemoveReaction(w http.ResponseWriter, r *http.Request) error {
	return h.setBotReaction(w, r, false)
}

// BotMember is a member as a bot sees them.
type BotMember struct {
	ID          uuid.UUID `json:"id" format:"uuid"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	AvatarURL   string    `json:"avatar_url,omitempty" binding:"optional"`
}

var errBotMembers = httpx.ErrForbidden("Missing Access: the bot may not ask for the member list (intent GUILD_MEMBERS)")

// visibleMembers lists the members the bot may know about: those who can view
// at least one channel the bot can view, so the list never reveals people
// who are unrelated to the bot's channels.
func (h *Handler) visibleMembers(r *http.Request) ([]BotMember, error) {
	bot := auth.BotFrom(r.Context())
	if bot == nil || bot.Intents&intentGuildMembers == 0 {
		return nil, errBotMembers
	}
	members, err := GetAllMembers(r.Context(), h.DB)
	if err != nil {
		return nil, err
	}
	snap, err := h.Perms.Snapshot(r.Context())
	if err != nil {
		return nil, err
	}
	self := auth.UserFrom(r.Context()).Subject()
	mine := snap.ChannelPermissions(self)
	out := []BotMember{}
	for _, m := range members {
		sub := m.Subject()
		for ch := range mine {
			if snap.Channel(sub, ch).Has(perm.ViewChannel) {
				out = append(out, BotMember{ID: m.ID, Username: m.Username, DisplayName: m.DisplayName, AvatarURL: m.AvatarURL})
				break
			}
		}
	}
	return out, nil
}

// botListMembers handles GET /api/bot/v1/members.
//
// @Summary List members
// @Description Needs the GUILD_MEMBERS intent to be allowed for the bot (an admin sets it). Lists only the members who can view at least one channel the bot can view; no profile, presence or role data.
// @ID botListMembers
// @Tags Bot API
// @Produce json
// @Security botAuth
// @Success 200 {array} BotMember "Members."
// @Failure 401 {object} httpx.DiscordError "Missing or invalid token (0)."
// @Failure 403 {object} httpx.DiscordError "The bot is not allowed to read members (50013)."
// @Failure 429 {object} httpx.DiscordRateLimit "Rate limited; see retry_after and Retry-After."
// @Failure 500 {object} httpx.DiscordError "Server failure."
// @Router /api/bot/v1/members [get]
func (h *Handler) botListMembers(w http.ResponseWriter, r *http.Request) error {
	out, err := h.visibleMembers(r)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

// botGetMember handles GET /api/bot/v1/members/{userID}.
//
// @Summary Get a member
// @Description Same rules as the list; a member the bot may not know about answers 404.
// @ID botGetMember
// @Tags Bot API
// @Produce json
// @Security botAuth
// @Param userID path string true "User ID." Format(uuid)
// @Success 200 {object} BotMember "The member."
// @Failure 400 {object} httpx.DiscordError "Invalid form body (50035)."
// @Failure 401 {object} httpx.DiscordError "Missing or invalid token (0)."
// @Failure 403 {object} httpx.DiscordError "The bot is not allowed to read members (50013)."
// @Failure 404 {object} httpx.DiscordError "Unknown User (10013)."
// @Failure 429 {object} httpx.DiscordRateLimit "Rate limited; see retry_after and Retry-After."
// @Failure 500 {object} httpx.DiscordError "Server failure."
// @Router /api/bot/v1/members/{userID} [get]
func (h *Handler) botGetMember(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "userID")
	if err != nil {
		return err
	}
	list, err := h.visibleMembers(r)
	if err != nil {
		return err
	}
	for _, m := range list {
		if m.ID == id {
			httpx.WriteJSON(w, http.StatusOK, m)
			return nil
		}
	}
	return httpx.ErrNotFound("unknown user")
}
