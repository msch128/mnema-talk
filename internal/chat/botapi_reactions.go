package chat

import (
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
)

func extractEmojiParam(r *http.Request) (string, error) {
	raw := chi.URLParam(r, "emoji")
	if decoded, err := url.PathUnescape(raw); err == nil {
		raw = decoded
	}
	return validateReactionRef(raw)
}

// setBotReaction adds (want) or removes the bot's reaction, idempotently.
func (h *Handler) setBotReaction(w http.ResponseWriter, r *http.Request, want bool) error {
	user := auth.UserFrom(r.Context())
	msgID, ref, err := h.messageInChannel(r)
	if err != nil {
		return err
	}
	emoji, err := extractEmojiParam(r)
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
	reactions, changed, err := SetReactionAs(r.Context(), h.DB, msgID, user.ID, emoji, want, p.Has(perm.AddReactions))
	if err != nil {
		return err
	}
	if changed {
		h.Events.BroadcastChannel(ref.ChannelID, events.MessageReaction, ReactionResult{
			MessageID: msgID, ChannelID: ref.ChannelID, Reactions: reactions,
		})
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
