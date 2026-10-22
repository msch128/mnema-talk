package bot

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// BasePath is the versioned base path of the bot API. A new incompatible
// version gets a new path; /v1 keeps its shapes.
const BasePath = "/api/bot/v1"

// IsAPIPath reports whether the request is for the bot API. Those routes
// authenticate with the Authorization header only and never read the session
// cookie, so the same-origin (CSRF) check does not apply to them: bots are
// programs without an Origin, and a browser cannot make one act, because it
// cannot attach the token.
func IsAPIPath(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, BasePath+"/")
}

// ChannelParam reads the channelID path parameter as the canonical UUID text,
// so every spelling the UUID parser accepts (case, braces, urn: prefix, no
// dashes) names the same channel. It is empty when the parameter is no UUID.
func ChannelParam(r *http.Request) string {
	id, err := uuid.Parse(chi.URLParam(r, "channelID"))
	if err != nil {
		return ""
	}
	return id.String()
}

// Self is the bot as it sees itself.
type Self struct {
	// ID is the bot's user ID: the author of its messages (user_id).
	ID     uuid.UUID `json:"id" format:"uuid"`
	BotID  uuid.UUID `json:"bot_id" format:"uuid"`
	Name   string    `json:"username"`
	Bot    bool      `json:"bot" enums:"true"`
	Avatar string    `json:"avatar_url,omitempty" binding:"optional"`
	// AllowedIntents are the gateway intents the admin allowed.
	AllowedIntents int64 `json:"allowed_intents"`
}

// Me handles GET /api/bot/v1/users/@me.
//
// @Summary Get the bot itself
// @Description The bot behind the token: its user ID (the author of its messages), name, avatar and the gateway intents it may use. Also a cheap way to check a token.
// @ID botMe
// @Tags Bot API
// @Produce json
// @Security botAuth
// @Success 200 {object} Self "The bot."
// @Failure 401 {object} httpx.DiscordError "Missing or invalid token (0)."
// @Failure 429 {object} httpx.DiscordRateLimit "Rate limited; see retry_after and Retry-After."
// @Router /api/bot/v1/users/at_me [get]
func Me(w http.ResponseWriter, r *http.Request) {
	u, b := auth.UserFrom(r.Context()), auth.BotFrom(r.Context())
	httpx.WriteJSON(w, http.StatusOK, Self{ID: u.ID, BotID: b.BotID, Name: u.DisplayName, Bot: true, Avatar: u.AvatarURL, AllowedIntents: b.Intents})
}

// NotFound and MethodNotAllowed answer in the bot API's error shape.
func NotFound(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusNotFound, httpx.DiscordError{Message: "404: Not Found", Code: 0})
}

func MethodNotAllowed(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusMethodNotAllowed, httpx.DiscordError{Message: "405: Method Not Allowed", Code: 0})
}
