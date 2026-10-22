package push

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// Handler serves /api/push/*: the key a browser subscribes with, a member's
// devices, the preview preference and a test push.
type Handler struct {
	DB *db.Pool
	// Service sends the test push; nil when push is turned off.
	Service *Service
	// Keys is the server's VAPID key pair (only the public half is served).
	Keys Keys
	// Enabled is PUSH_ENABLED. Off, every route but the config answers 404.
	Enabled bool
	// PreviewsAllowed is false when the operator forces "no preview".
	PreviewsAllowed bool

	writes *httpx.RateLimiter
	tests  *httpx.RateLimiter
}

// NewHandler builds the handler with its per-account budgets: registering and
// removing devices is rare, and a test push is a real request to a push service.
func NewHandler(h Handler) *Handler {
	h.writes = httpx.NewRateLimiter(60, time.Hour).Named("push_write")
	h.tests = httpx.NewRateLimiter(6, time.Minute).Named("push_test")
	return &h
}

// Mount registers the routes every signed-in member may call.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/push/config", httpx.Handle(h.getConfig))
	r.Get("/push/subscriptions", httpx.Handle(h.listSubscriptions))
	r.With(h.writes.By(auth.UserKey)).Post("/push/subscriptions", httpx.Handle(h.subscribe))
	r.With(h.writes.By(auth.UserKey)).Delete("/push/subscriptions/{id}", httpx.Handle(h.unsubscribe))
	r.With(h.writes.By(auth.UserKey)).Put("/push/preferences", httpx.Handle(h.putPreferences))
	r.With(h.tests.By(auth.UserKey)).Post("/push/test", httpx.Handle(h.sendTest))
}

// Config is what a browser needs to turn push on.
type Config struct {
	// Enabled is false when the operator turned push off (PUSH_ENABLED=false).
	Enabled bool `json:"enabled"`
	// PublicKey is the VAPID public key (base64url) to subscribe with; empty while push is off.
	PublicKey string `json:"public_key"`
	// PreviewAllowed is false when the operator forces pushes without message text.
	PreviewAllowed bool `json:"preview_allowed"`
	// Preview is the member's choice: include the message text in pushes. Only takes effect while PreviewAllowed.
	Preview bool `json:"preview"`
	// MaxDevices is how many devices one account may enable push on.
	MaxDevices int `json:"max_devices" minimum:"1"`
}

// SubscribeRequest is a browser's push subscription (PushSubscription.toJSON()).
type SubscribeRequest struct {
	// Endpoint is the push service address the browser got. https only, a public host, port 443.
	Endpoint string `json:"endpoint" maxLength:"2048"`
	// P256dh is the browser's P-256 public key, base64url.
	P256dh string `json:"p256dh" maxLength:"128"`
	// Auth is the browser's 16 byte authentication secret, base64url.
	Auth string `json:"auth" maxLength:"64"`
	// Label names the device in the list ("Firefox on Windows"); at most 64 characters.
	Label string `json:"label" maxLength:"64" binding:"optional"`
}

// Subscriptions lists a member's devices.
type Subscriptions struct {
	Subscriptions []Subscription `json:"subscriptions"`
}

// PreferencesRequest is the body of PUT /api/push/preferences.
type PreferencesRequest struct {
	// Preview includes the message text in pushes (default true).
	Preview bool `json:"preview"`
}

// TestResult says how many of the member's devices a test push reached.
type TestResult struct {
	Delivered int `json:"delivered" minimum:"0"`
	Failed    int `json:"failed" minimum:"0"`
}

func (h *Handler) requireEnabled() error {
	if !h.Enabled {
		return httpx.ErrNotFound("push notifications are turned off on this server")
	}
	return nil
}

// getConfig handles GET /api/push/config.
//
// @Summary Web Push settings
// @Description Whether push is on, the VAPID public key to subscribe with, whether the operator allows message text in pushes and the caller's own choice. Always answers 200; with push off the key is empty.
// @ID getPushConfig
// @Tags Push
// @Produce json
// @Security cookieAuth
// @Success 200 {object} Config "Settings."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/push/config [get]
func (h *Handler) getConfig(w http.ResponseWriter, r *http.Request) error {
	on, err := preview(r.Context(), h.DB, auth.UserFrom(r.Context()).ID)
	if err != nil {
		return httpx.ErrServer(err)
	}
	cfg := Config{Enabled: h.Enabled, PreviewAllowed: h.PreviewsAllowed, Preview: on, MaxDevices: MaxDevices}
	if h.Enabled {
		cfg.PublicKey = h.Keys.Public
	}
	httpx.WriteJSON(w, http.StatusOK, cfg)
	return nil
}

// listSubscriptions handles GET /api/push/subscriptions.
//
// @Summary List my push devices
// @Description The caller's devices with push on, oldest first. The push address itself is never returned; fingerprint is the first 32 hex characters of the SHA-256 of the endpoint, so a browser can tell which entry is itself.
// @ID listPushSubscriptions
// @Tags Push
// @Produce json
// @Security cookieAuth
// @Success 200 {object} Subscriptions "Devices."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: push is turned off on this server."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/push/subscriptions [get]
func (h *Handler) listSubscriptions(w http.ResponseWriter, r *http.Request) error {
	if err := h.requireEnabled(); err != nil {
		return err
	}
	subs, err := list(r.Context(), h.DB, auth.UserFrom(r.Context()).ID)
	if err != nil {
		return httpx.ErrServer(err)
	}
	httpx.WriteJSON(w, http.StatusOK, Subscriptions{Subscriptions: subs})
	return nil
}

// subscribe handles POST /api/push/subscriptions.
//
// @Summary Turn push on for this device
// @Description Registers the browser's push subscription for the caller. The endpoint must be an https URL on port 443 whose host is not a literal private, loopback or reserved address; the server connects only to that address, only to deliver encrypted pushes. Registering an endpoint again updates it; one registered by another account moves to the caller. At most 10 devices per account.
// @ID subscribePush
// @Tags Push
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body SubscribeRequest true "Request body."
// @Success 201 {object} Subscription "Registered."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, a refused endpoint or invalid keys."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: push is turned off on this server."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the account already has 10 devices."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/push/subscriptions [post]
func (h *Handler) subscribe(w http.ResponseWriter, r *http.Request) error {
	if err := h.requireEnabled(); err != nil {
		return err
	}
	var req SubscribeRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	endpoint, err := ValidateEndpoint(req.Endpoint)
	if err != nil {
		return err
	}
	p256dh, secret, err := ValidateKeys(req.P256dh, req.Auth)
	if err != nil {
		return err
	}
	sub, err := upsert(r.Context(), h.DB, auth.UserFrom(r.Context()).ID,
		newSubscription{endpoint: endpoint, p256dh: p256dh, auth: secret, label: clean(req.Label, 64)})
	if err != nil {
		if ae, ok := httpx.AsAPIError(err); ok {
			return ae
		}
		return httpx.ErrServer(err)
	}
	httpx.WriteJSON(w, http.StatusCreated, sub)
	return nil
}

// unsubscribe handles DELETE /api/push/subscriptions/{id}.
//
// @Summary Turn push off for a device
// @Description Removes one of the caller's devices. Another account's device answers 404 like a missing one.
// @ID unsubscribePush
// @Tags Push
// @Security cookieAuth
// @Param id path string true "Subscription ID." Format(uuid)
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/push/subscriptions/{id} [delete]
func (h *Handler) unsubscribe(w http.ResponseWriter, r *http.Request) error {
	if err := h.requireEnabled(); err != nil {
		return err
	}
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	found, err := remove(r.Context(), h.DB, auth.UserFrom(r.Context()).ID, id)
	if err != nil {
		return httpx.ErrServer(err)
	}
	if !found {
		return httpx.ErrNotFound("device not found")
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// putPreferences handles PUT /api/push/preferences.
//
// @Summary Set the push preview
// @Description Whether pushes carry the message text. Off, a push shows only the channel and the sender. The operator can force this off for everyone (PUSH_PREVIEWS=off); the stored choice is kept.
// @ID setPushPreferences
// @Tags Push
// @Accept json
// @Security cookieAuth
// @Param request body PreferencesRequest true "Request body."
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: push is turned off on this server."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/push/preferences [put]
func (h *Handler) putPreferences(w http.ResponseWriter, r *http.Request) error {
	if err := h.requireEnabled(); err != nil {
		return err
	}
	var req PreferencesRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if err := setPreview(r.Context(), h.DB, auth.UserFrom(r.Context()).ID, req.Preview); err != nil {
		return httpx.ErrServer(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// sendTest handles POST /api/push/test.
//
// @Summary Send a test push
// @Description Sends a test notification to every device of the caller and reports how many push services accepted it. At most 6 a minute.
// @ID sendTestPush
// @Tags Push
// @Produce json
// @Security cookieAuth
// @Success 200 {object} TestResult "Result."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: push is turned off on this server."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/push/test [post]
func (h *Handler) sendTest(w http.ResponseWriter, r *http.Request) error {
	if err := h.requireEnabled(); err != nil {
		return err
	}
	res, err := h.Service.SendTest(r.Context(), auth.UserFrom(r.Context()).ID)
	if err != nil {
		return httpx.ErrServer(err)
	}
	httpx.WriteJSON(w, http.StatusOK, res)
	return nil
}

// SendTest pushes a test notification to every device of the account, one
// after the other, and counts what the push services accepted.
func (s *Service) SendTest(ctx context.Context, userID uuid.UUID) (TestResult, error) {
	s.init()
	ctx, cancel := context.WithTimeout(ctx, testBudget)
	defer cancel()
	targets, err := targetsFor(ctx, s.DB, []uuid.UUID{userID})
	if err != nil {
		return TestResult{}, err
	}
	var locale string
	if err := s.DB.QueryRow(ctx, `SELECT locale FROM users WHERE id = $1`, userID).Scan(&locale); err != nil {
		return TestResult{}, err
	}
	body, err := testPayload(locale, s.Now()).encode()
	if err != nil {
		return TestResult{}, err
	}
	var out TestResult
	for _, t := range targets[userID] {
		res := s.Sender.Send(ctx, t, body, Options{TTL: testTTL, Urgent: true, Timeout: sendTimeout})
		s.record(ctx, t.id, res)
		if res.Outcome == Delivered {
			out.Delivered++
		} else {
			out.Failed++
		}
	}
	return out, nil
}
