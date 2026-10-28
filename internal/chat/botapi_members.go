package chat

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// BotMember is a member as a bot sees them.
type BotMember struct {
	ID          uuid.UUID `json:"id" format:"uuid"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	AvatarURL   string    `json:"avatar_url,omitempty" binding:"optional"`
	// Bot is true for bot accounts, which are members too. Mention one by its
	// username like any member; message.mentions then lists its id.
	Bot bool `json:"bot,omitempty" binding:"optional"`
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
				out = append(out, BotMember{ID: m.ID, Username: m.Username, DisplayName: m.DisplayName, AvatarURL: m.AvatarURL, Bot: m.IsBot})
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
	return httpx.ErrNotFound("unknown user").WithDiscordCode(httpx.DiscordCodeUnknownUser)
}
