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

// requireMemberIntent admits only a bot with the GUILD_MEMBERS intent.
func requireMemberIntent(r *http.Request) error {
	if bot := auth.BotFrom(r.Context()); bot == nil || bot.Intents&intentGuildMembers == 0 {
		return errBotMembers
	}
	return nil
}

// memberVisibility answers which members the bot may know about: those who can
// view at least one channel the bot can view, so nothing reveals people who
// are unrelated to the bot's channels.
type memberVisibility struct {
	snap *perm.Snapshot
	mine map[uuid.UUID]perm.Set
}

func (h *Handler) memberVisibility(r *http.Request) (memberVisibility, error) {
	snap, err := h.Perms.Snapshot(r.Context())
	if err != nil {
		return memberVisibility{}, err
	}
	return memberVisibility{snap: snap, mine: snap.ChannelPermissions(auth.UserFrom(r.Context()).Subject())}, nil
}

func (v memberVisibility) sees(m Member) bool {
	sub := m.Subject()
	for ch := range v.mine {
		if v.snap.Channel(sub, ch).Has(perm.ViewChannel) {
			return true
		}
	}
	return false
}

func botMemberOf(m Member) BotMember {
	return BotMember{ID: m.ID, Username: m.Username, DisplayName: m.DisplayName, AvatarURL: m.AvatarURL, Bot: m.IsBot}
}

// visibleMembers lists the members the bot may know about.
func (h *Handler) visibleMembers(r *http.Request) ([]BotMember, error) {
	if err := requireMemberIntent(r); err != nil {
		return nil, err
	}
	members, err := GetAllMembers(r.Context(), h.DB)
	if err != nil {
		return nil, err
	}
	vis, err := h.memberVisibility(r)
	if err != nil {
		return nil, err
	}
	out := []BotMember{}
	for _, m := range members {
		if vis.sees(m) {
			out = append(out, botMemberOf(m))
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
	if err := requireMemberIntent(r); err != nil {
		return err
	}
	// One member is read and tested on its own, not picked from the whole list.
	m, err := GetMember(r.Context(), h.DB, id)
	if err != nil {
		return err
	}
	vis, err := h.memberVisibility(r)
	if err != nil {
		return err
	}
	if !vis.sees(*m) {
		return errUnknownUser
	}
	httpx.WriteJSON(w, http.StatusOK, botMemberOf(*m))
	return nil
}
