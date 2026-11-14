package media

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// MountBot registers the bot API's file upload (the bot authentication
// middleware must already apply). send is the per-channel send limit.
func (h *Handler) MountBot(r chi.Router, send func(http.Handler) http.Handler) {
	r.With(httpx.MaxBodyDiscord(h.UploadBodyLimit()), send).Post("/channels/{channelID}/upload", httpx.HandleDiscord(h.botUpload))
}

// botUpload handles POST /api/bot/v1/channels/{channelID}/upload.
//
// @Summary Post a message with an attachment
// @Description multipart/form-data. Creates a message carrying the file as the bot. Needs View channels, Send messages and Attach files in the channel. The size limit is the Owner's upload limit for the bot's roles (and the server's), the type is sniffed and checked against an allowlist. Counts towards the send limit of the channel.
// @ID botUploadFile
// @Tags Bot API
// @Accept mpfd
// @Produce json
// @Security botAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param file formData file true "The file; type is sniffed and checked against an allowlist."
// @Param content formData string false "Optional caption." maxlength(4000)
// @Param parent_id formData string false "Thread root message ID (reply in a thread)." Format(uuid)
// @Param reply_to_id formData string false "Quoted message ID." Format(uuid)
// @Param allowed_mentions formData string false "The allowed_mentions object of the message route (parse, users, roles), JSON-encoded. Without it the caption notifies the members it names, never @all, @here or a role."
// @Param lifetime_seconds formData integer false "The message's lifetime: the server deletes it that long after storing it (60 to 2592000, at most the community's ceiling). Needs the messages.lifetime feature." minimum(60) maximum(2592000)
// @Success 201 {object} core.Message "Created message."
// @Failure 400 {object} httpx.DiscordError "Invalid form body (50035)."
// @Failure 401 {object} httpx.DiscordError "Missing or invalid token (0)."
// @Failure 403 {object} httpx.DiscordError "Missing Permissions (50013)."
// @Failure 404 {object} httpx.DiscordError "Unknown Channel (10003)."
// @Failure 413 {object} httpx.DiscordError "Request entity too large (40005)."
// @Failure 415 {object} httpx.DiscordError "The file type is not allowed."
// @Failure 429 {object} httpx.DiscordRateLimit "Rate limited; see retry_after and Retry-After."
// @Failure 500 {object} httpx.DiscordError "Server failure."
// @Failure 503 {object} httpx.DiscordError "File storage is not available."
// @Router /api/bot/v1/channels/{channelID}/upload [post]
func (h *Handler) botUpload(w http.ResponseWriter, r *http.Request) error {
	if h.Store == nil {
		return httpx.ErrUnavailable("file storage is not configured")
	}
	return h.uploadAs(w, r, func(r *http.Request, ch uuid.UUID) (context.Context, captionContext, error) {
		ctx, withMentions, err := messages.BotUploadContext(h.Perms, r, ch)
		if err != nil {
			return nil, nil, err
		}
		return ctx, func(raw string) (context.Context, error) {
			am, err := parseAllowedMentions(raw)
			if err != nil {
				return nil, err
			}
			return withMentions(am), nil
		}, nil
	})
}

// parseAllowedMentions reads the allowed_mentions form field: the JSON object
// the message route takes in its body. Absent means the default.
func parseAllowedMentions(raw string) (*messages.AllowedMentions, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var am *messages.AllowedMentions
	dec := json.NewDecoder(strings.NewReader(raw))
	if err := dec.Decode(&am); err != nil || dec.More() {
		return nil, httpx.ErrInvalidInput("allowed_mentions must be a JSON object")
	}
	return am, nil
}
