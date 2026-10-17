package webhook

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/media"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Handler serves the webhook management API (under /api/admin) and the
// public execute endpoint.
type Handler struct {
	DB     *db.Pool
	Events events.Publisher
	// Media stores attachments and avatars.
	Media *media.Handler
	// Perms decides who manages webhooks; nil imposes no limits (tests).
	Perms *perm.Service
	// Online resolves @here for the mentions of a message.
	Online chat.OnlineSource
	// PublicURL is the address in the URLs shown to the creator.
	PublicURL string

	perWebhook *httpx.RateLimiter
	perChannel *httpx.RateLimiter
	failures   *httpx.RateLimiter
}

// New returns a Handler with its rate limiters.
func New(h Handler) *Handler {
	h.perWebhook = httpx.NewRateLimiter(perWebhookLimit, perWebhookWindow).Named("webhook")
	h.perChannel = httpx.NewRateLimiter(perChannelLimit, perChannelWindow).Named("webhook_channel")
	h.failures = httpx.NewRateLimiter(failedAuthLimit, failedAuthWindow).Named("webhook_auth")
	return &h
}

// SetLimits replaces the per-webhook (per 2 seconds) and per-channel (per
// minute) request limits. For tests.
func (h *Handler) SetLimits(perWebhook, perChannel int) {
	h.perWebhook = httpx.NewRateLimiter(perWebhook, perWebhookWindow).Named("webhook")
	h.perChannel = httpx.NewRateLimiter(perChannel, perChannelWindow).Named("webhook_channel")
}

// MountPublic registers the execute endpoint. It must not sit behind the
// session check: the secret in the URL is its authentication.
func (h *Handler) MountPublic(r chi.Router) {
	r.Post("/webhooks/{id}/{token}", h.execute)
}

// MountAdmin registers the management routes (RequireUser and the admin area
// gate apply; each route checks Manage webhooks in the channel itself).
func (h *Handler) MountAdmin(r chi.Router) {
	r.Get("/channels/{id}/webhooks", httpx.Handle(h.list))
	r.Post("/channels/{id}/webhooks", httpx.Handle(h.create))
	r.Patch("/webhooks/{id}", httpx.Handle(h.update))
	r.Post("/webhooks/{id}/token", httpx.Handle(h.regenerate))
	r.Post("/webhooks/{id}/avatar", httpx.Handle(h.avatar))
	r.Delete("/webhooks/{id}", httpx.Handle(h.delete))
}

func (h *Handler) url(rec *record, token string) string {
	return strings.TrimRight(h.PublicURL, "/") + "/api/webhooks/" + rec.ID.String() + "/" + token
}

// authorize checks the caller may manage webhooks in the channel and returns
// the permission state it decided on (nil without a permission model). A
// channel they cannot view looks like a missing one.
func (h *Handler) authorize(r *http.Request, ch uuid.UUID) (*perm.Snapshot, error) {
	if h.Perms == nil {
		return nil, nil
	}
	snap, err := h.Perms.ForChannel(r.Context(), ch)
	if err != nil {
		return nil, err
	}
	return snap, perm.CheckChannel(snap.Channel(auth.UserFrom(r.Context()).Subject(), ch), perm.ManageWebhooks)
}

// load finds the webhook of the path and checks the caller may manage it:
// Manage webhooks in its channel and, when somebody else created it, a
// highest role not below the creator's. Nobody but the Owner touches a
// webhook of the Owner.
func (h *Handler) load(r *http.Request) (*record, error) {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return nil, err
	}
	rec, err := get(r.Context(), h.DB, id)
	if err != nil {
		return nil, err
	}
	snap, err := h.authorize(r, rec.ChannelID)
	if err != nil {
		// A hidden channel hides its webhooks; a plain missing permission
		// stays a 403.
		if ae, ok := httpx.AsAPIError(err); ok && ae == perm.ErrChannelHidden {
			return nil, errNotFound
		}
		return nil, err
	}
	if snap == nil || rec.CreatedBy == nil {
		return rec, nil
	}
	user := auth.UserFrom(r.Context())
	actor, creator := user.Subject(), perm.Subject{ID: *rec.CreatedBy}
	switch {
	case snap.IsOwner(actor) || *rec.CreatedBy == user.ID:
		return rec, nil
	case snap.IsOwner(creator):
		return nil, httpx.ErrForbidden("only the Owner can change a webhook of the Owner")
	case snap.TopPosition(actor) < snap.TopPosition(creator):
		return nil, httpx.ErrForbidden("that webhook was created by a member with a higher role than yours")
	}
	return rec, nil
}

// CreateRequest is the body of creating a webhook.
type CreateRequest struct {
	Name string `json:"name" maxLength:"80"`
}

// UpdateRequest is the body of renaming a webhook.
type UpdateRequest struct {
	Name string `json:"name" maxLength:"80"`
}

// list handles GET /api/admin/channels/{id}/webhooks.
//
// @Summary List a channel's webhooks
// @Description Needs Manage webhooks and View channel in that channel. The tokens are never listed.
// @ID listWebhooks
// @Tags Webhooks
// @Produce json
// @Security cookieAuth
// @Param id path string true "Channel ID." Format(uuid)
// @Success 200 {array} Webhook "Webhooks, oldest first."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): bad ID."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing permission."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the channel does not exist or is hidden from the caller."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/channels/{id}/webhooks [get]
func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	ch, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	if _, err := h.authorize(r, ch); err != nil {
		return err
	}
	hooks, err := list(r.Context(), h.DB, ch)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, hooks)
	return nil
}

// create handles POST /api/admin/channels/{id}/webhooks.
//
// @Summary Create a webhook
// @Description Needs Manage webhooks and View channel in that channel. The answer carries the secret URL; it is shown only once. At most 15 webhooks per channel.
// @ID createWebhook
// @Tags Webhooks
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "Channel ID." Format(uuid)
// @Param body body CreateRequest true "Name."
// @Success 201 {object} CreatedWebhook "Created."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT)."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing permission, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the channel does not exist or is hidden from the caller."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the channel has the maximum number of webhooks."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/channels/{id}/webhooks [post]
func (h *Handler) create(w http.ResponseWriter, r *http.Request) error {
	ch, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	if _, err := h.authorize(r, ch); err != nil {
		return err
	}
	var req CreateRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	name, err := cleanName("name", req.Name)
	if err != nil {
		return err
	}
	rec, token, err := create(r.Context(), h.DB, ch, auth.UserFrom(r.Context()).ID, name)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, CreatedWebhook{Webhook: rec.Webhook, Token: token, URL: h.url(rec, token)})
	return nil
}

// update handles PATCH /api/admin/webhooks/{id}.
//
// @Summary Rename a webhook
// @Description Needs Manage webhooks in the webhook's channel, and a highest role not below the creator's. Earlier messages keep the name they were posted under.
// @ID updateWebhook
// @Tags Webhooks
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "Webhook ID." Format(uuid)
// @Param body body UpdateRequest true "New name."
// @Success 200 {object} Webhook "Updated."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT)."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing permission or hierarchy, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such webhook."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/webhooks/{id} [patch]
func (h *Handler) update(w http.ResponseWriter, r *http.Request) error {
	rec, err := h.load(r)
	if err != nil {
		return err
	}
	var req UpdateRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	name, err := cleanName("name", req.Name)
	if err != nil {
		return err
	}
	if err := rename(r.Context(), h.DB, rec, name); err != nil {
		return err
	}
	return h.writeCurrent(w, r, rec.ID)
}

func (h *Handler) writeCurrent(w http.ResponseWriter, r *http.Request, id uuid.UUID) error {
	cur, err := get(r.Context(), h.DB, id)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, cur.Webhook)
	return nil
}

// regenerate handles POST /api/admin/webhooks/{id}/token.
//
// @Summary Regenerate a webhook's token
// @Description The old URL stops working at once. The answer carries the new secret URL; it is shown only once.
// @ID regenerateWebhookToken
// @Tags Webhooks
// @Produce json
// @Security cookieAuth
// @Param id path string true "Webhook ID." Format(uuid)
// @Success 200 {object} CreatedWebhook "New token and URL."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT)."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing permission or hierarchy, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such webhook."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/webhooks/{id}/token [post]
func (h *Handler) regenerate(w http.ResponseWriter, r *http.Request) error {
	rec, err := h.load(r)
	if err != nil {
		return err
	}
	token, err := regenerate(r.Context(), h.DB, rec)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, CreatedWebhook{Webhook: rec.Webhook, Token: token, URL: h.url(rec, token)})
	return nil
}

// avatar handles POST /api/admin/webhooks/{id}/avatar.
//
// @Summary Set a webhook's avatar
// @Description multipart/form-data with an image in field 'avatar' (max 5 MB). Replaces the previous one.
// @ID uploadWebhookAvatar
// @Tags Webhooks
// @Accept mpfd
// @Produce json
// @Security cookieAuth
// @Param id path string true "Webhook ID." Format(uuid)
// @Param avatar formData file true "Image file, at most 5 MB."
// @Success 200 {object} Webhook "Updated."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT)."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing permission or hierarchy, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such webhook."
// @Failure 413 {object} httpx.ErrorResponse "PAYLOAD_TOO_LARGE: file exceeds the limit."
// @Failure 415 {object} httpx.ErrorResponse "UNSUPPORTED_MEDIA_TYPE: the file type is not allowed."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Failure 503 {object} httpx.ErrorResponse "UNAVAILABLE: file storage is not available."
// @Router /api/admin/webhooks/{id}/avatar [post]
func (h *Handler) avatar(w http.ResponseWriter, r *http.Request) error {
	rec, err := h.load(r)
	if err != nil {
		return err
	}
	if err := h.Media.ReplaceAvatar(r, rec.botUserID); err != nil {
		return err
	}
	return h.writeCurrent(w, r, rec.ID)
}

// delete handles DELETE /api/admin/webhooks/{id}.
//
// @Summary Delete a webhook
// @Description Its URL stops working at once. Messages it posted stay.
// @ID deleteWebhook
// @Tags Webhooks
// @Security cookieAuth
// @Param id path string true "Webhook ID." Format(uuid)
// @Success 204 "Deleted."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT)."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing permission or hierarchy, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such webhook."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/webhooks/{id} [delete]
func (h *Handler) delete(w http.ResponseWriter, r *http.Request) error {
	rec, err := h.load(r)
	if err != nil {
		return err
	}
	if err := remove(r.Context(), h.DB, h.Media.Store, rec); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
