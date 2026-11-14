package channels

import (
	"net/http"

	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// ServeLifetimeSettings handles GET /api/message-settings.
//
// @Summary Lifetime settings
// @Description Whether members may send messages with a lifetime and the longest one they may choose (doc/timed-messages.md). Changes come with channels_changed. Needs the messages.lifetime feature.
// @ID getLifetimeSettings
// @Tags Messages
// @Produce json
// @Security cookieAuth
// @Success 200 {object} channels.LifetimeSettings "The settings."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/message-settings [get]
func (a *API) ServeLifetimeSettings(w http.ResponseWriter, r *http.Request) error {
	s, err := core.LoadMessageSettings(r.Context(), a.DB)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, LifetimeSettings{Enabled: s.LifetimesEnabled, MaxSeconds: s.LifetimeMaxSeconds})
	return nil
}

// ServeMessageSettings handles GET /api/admin/message-settings.
//
// @Summary Message settings
// @Description Owner and Administrators: the lifetime switch and ceiling, the community's auto-delete default and when auto-delete runs next. Needs the messages.lifetime feature.
// @ID getMessageSettings
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Success 200 {object} channels.MessageSettingsView "The settings."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not the Owner or an Administrator."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/message-settings [get]
func (a *API) ServeMessageSettings(w http.ResponseWriter, r *http.Request) error {
	v, err := messageSettingsView(r.Context(), a.DB)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, v)
	return nil
}

// ServeSetLifetimeSettings handles PUT /api/admin/message-settings/lifetimes.
//
// @Summary Set the lifetime settings
// @Description Owner and Administrators; audited as settings.message_lifetimes_set. Switching lifetimes off refuses new messages with a lifetime; those that have one still end on time. The ceiling holds for everyone, the Owner included. Broadcasts channels_changed. Needs the messages.lifetime feature.
// @ID setLifetimeSettings
// @Tags Admin
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body channels.LifetimeSettingsRequest true "Request body."
// @Success 200 {object} channels.MessageSettingsView "The settings now."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields or a ceiling outside 60 to 2592000."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not the Owner or an Administrator, or cross-origin request rejected."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/message-settings/lifetimes [put]
func (a *API) ServeSetLifetimeSettings(w http.ResponseWriter, r *http.Request) error {
	var req LifetimeSettingsRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if err := setLifetimeSettings(r.Context(), a.DB, req); err != nil {
		return err
	}
	a.ChannelsChanged()
	return a.ServeMessageSettings(w, r)
}

// ServeSetAutoDeleteDefault handles PUT /api/admin/message-settings/auto-delete.
//
// @Summary Set the auto-delete default
// @Description Owner and Administrators; audited as settings.auto_delete_default_set. Every community channel without an auto-delete setting of its own follows it; conversations and their call Talks never do. hours 0 turns the default off (the initial state). Broadcasts channels_changed. Needs the channels.auto_delete.v2 feature.
// @ID setAutoDeleteDefault
// @Tags Admin
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body channels.AutoDeleteDefaultRequest true "Request body."
// @Success 200 {object} channels.MessageSettingsView "The settings now."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, hours outside 0 to 8760 or an unknown scope."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not the Owner or an Administrator, or cross-origin request rejected."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/message-settings/auto-delete [put]
func (a *API) ServeSetAutoDeleteDefault(w http.ResponseWriter, r *http.Request) error {
	var req AutoDeleteDefaultRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	hours, scope, err := req.valid()
	if err != nil {
		return err
	}
	if err := setAutoDeleteDefault(r.Context(), a.DB, hours, scope); err != nil {
		return err
	}
	a.ChannelsChanged()
	return a.ServeMessageSettings(w, r)
}

// ServePreviewAutoDeleteDefault handles POST /api/admin/message-settings/auto-delete/preview.
//
// @Summary Preview the auto-delete default
// @Description Owner and Administrators: a dry run of PUT /api/admin/message-settings/auto-delete with the same body, counted over every channel that follows the default. Changes nothing. Needs the channels.auto_delete.v2 feature.
// @ID previewAutoDeleteDefault
// @Tags Admin
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body channels.AutoDeleteDefaultRequest true "Request body."
// @Success 200 {object} channels.AutoDeletePreview "What would be removed."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): as for setting it."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not the Owner or an Administrator, or cross-origin request rejected."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/message-settings/auto-delete/preview [post]
func (a *API) ServePreviewAutoDeleteDefault(w http.ResponseWriter, r *http.Request) error {
	var req AutoDeleteDefaultRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	hours, scope, err := req.valid()
	if err != nil {
		return err
	}
	chans, err := channelsFollowingDefault(r.Context(), a.DB)
	if err != nil {
		return err
	}
	out, err := PreviewAutoDelete(r.Context(), a.DB, chans, hours, scope)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}
