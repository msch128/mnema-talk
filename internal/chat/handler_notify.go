package chat

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// SetNotifyLevelRequest is the body of PUT /api/channels/{channelID}/notifications
// and PUT /api/categories/{id}/notifications.
type SetNotifyLevelRequest struct {
	Level NotifyLevel `json:"level" enums:"all,mentions,mute,inherit"`
	// MutedUntil ends a mute (level mute only): in the future, at most a year ahead. Null or omitted mutes until unmuted.
	MutedUntil *time.Time `json:"muted_until" format:"date-time" binding:"optional" extensions:"x-nullable"`
}

// MarkAllReadRequest is the optional body of POST /api/read-state/read-all.
type MarkAllReadRequest struct {
	// CategoryID limits the call to one category's channels; omitted or null means every channel the caller may view.
	CategoryID *uuid.UUID `json:"category_id" format:"uuid" binding:"optional" extensions:"x-nullable"`
}

// publishReadStateRefresh tells all of the user's sessions to reload their read
// states and notification settings.
func (h *Handler) publishReadStateRefresh(userID uuid.UUID) {
	h.Events.SendToUsers([]uuid.UUID{userID}, events.ReadState, map[string]any{"refresh": true})
}

// setNotifyLevel handles PUT /api/channels/{channelID}/notifications.
//
// @Summary Set channel notification level
// @Description Level all, mentions, mute or inherit (follow the category). A mute may end at muted_until (future, at most a year ahead). Notifies the user's other sessions with a read_state refresh.
// @ID setChannelNotifications
// @Tags Channels
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param request body SetNotifyLevelRequest true "Request body."
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: cross-origin request rejected by the CSRF check."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/channels/{channelID}/notifications [put]
func (h *Handler) setNotifyLevel(w http.ResponseWriter, r *http.Request) error {
	chID, err := h.channelParam(r)
	if err != nil {
		return err
	}
	var req SetNotifyLevelRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	user := auth.UserFrom(r.Context())
	if err := SetNotifyLevel(r.Context(), h.DB, user.ID, chID, req.Level, req.MutedUntil); err != nil {
		return err
	}
	h.publishReadStateRefresh(user.ID)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// setCategoryNotify handles PUT /api/categories/{id}/notifications.
//
// @Summary Set category notification level
// @Description The level channels of the category inherit when they have no setting of their own. A category the caller may not see answers 404 like a missing one. Notifies the user's other sessions with a read_state refresh.
// @ID setCategoryNotifications
// @Tags Channels
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "Category ID." Format(uuid)
// @Param request body SetNotifyLevelRequest true "Request body."
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: cross-origin request rejected by the CSRF check."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/categories/{id}/notifications [put]
func (h *Handler) setCategoryNotify(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	if err := h.requireCategory(r, id); err != nil {
		return err
	}
	var req SetNotifyLevelRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	user := auth.UserFrom(r.Context())
	if err := SetCategoryNotify(r.Context(), h.DB, user.ID, id, req.Level, req.MutedUntil); err != nil {
		return err
	}
	h.publishReadStateRefresh(user.ID)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// notificationSettings handles GET /api/notification-settings.
//
// @Summary Notification settings
// @Description The caller's stored channel and category settings (level all, mentions or mute, with muted_until for timed mutes), plus the server-wide default under server. Channels and categories without a setting, and expired mutes, are not listed; the read-state array carries the effective level per channel. Only channels and categories the caller may view.
// @ID getNotificationSettings
// @Tags Channels
// @Produce json
// @Security cookieAuth
// @Success 200 {object} NotificationSettings "Stored settings."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/notification-settings [get]
func (h *Handler) notificationSettings(w http.ResponseWriter, r *http.Request) error {
	visible, err := h.viewableChannels(r)
	if err != nil {
		return err
	}
	cats, err := h.viewableCategories(r)
	if err != nil {
		return err
	}
	out, err := GetNotificationSettings(r.Context(), h.DB, auth.UserFrom(r.Context()).ID, visible, cats)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

// SetServerNotifyRequest is the body of PUT /api/notification-settings/server.
type SetServerNotifyRequest struct {
	Level NotifyLevel `json:"level" enums:"all,mentions,mute"`
	// MutedUntil ends a mute (level mute only): in the future, at most a year ahead. Null or omitted mutes until unmuted.
	MutedUntil *time.Time `json:"muted_until" format:"date-time" binding:"optional" extensions:"x-nullable"`
	// SuppressEveryone ignores @all and @here for the caller, at every level.
	SuppressEveryone bool `json:"suppress_everyone" binding:"optional"`
}

// setServerNotify handles PUT /api/notification-settings/server.
//
// @Summary Set server notification default
// @Description The caller's server-wide default: level all, mentions or mute (optionally until muted_until) and whether @all and @here are ignored. Channels and categories without a setting of their own follow it (channel, then category, then this default); conversations are not affected by the level. Notifies the user's other sessions with a read_state refresh.
// @ID setServerNotifications
// @Tags Channels
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body SetServerNotifyRequest true "Request body."
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: cross-origin request rejected by the CSRF check."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/notification-settings/server [put]
func (h *Handler) setServerNotify(w http.ResponseWriter, r *http.Request) error {
	var req SetServerNotifyRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	user := auth.UserFrom(r.Context())
	in := ServerNotify{Level: req.Level, MutedUntil: req.MutedUntil, SuppressEveryone: req.SuppressEveryone}
	if err := SetServerNotify(r.Context(), h.DB, user.ID, in); err != nil {
		return err
	}
	h.publishReadStateRefresh(user.ID)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// markAllRead handles POST /api/read-state/read-all.
//
// @Summary Mark everything read
// @Description Moves the read marker of every channel the caller may view (or of one category's channels) to now; never backwards. Sends one read_state refresh to the user's sessions.
// @ID markAllRead
// @Tags Channels
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body MarkAllReadRequest false "Optional; omit the body to mark every viewable channel."
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the category does not exist or the caller may not see it."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/read-state/read-all [post]
func (h *Handler) markAllRead(w http.ResponseWriter, r *http.Request) error {
	var req MarkAllReadRequest
	if err := httpx.DecodeOptionalJSON(r, &req); err != nil {
		return err
	}
	if req.CategoryID != nil {
		if err := h.requireCategory(r, *req.CategoryID); err != nil {
			return err
		}
	}
	visible, err := h.viewableChannels(r)
	if err != nil {
		return err
	}
	user := auth.UserFrom(r.Context())
	if err := MarkAllRead(r.Context(), h.DB, user.ID, visible, req.CategoryID); err != nil {
		return err
	}
	h.publishReadStateRefresh(user.ID)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// viewableCategories lists the categories the caller may see; nil means all.
func (h *Handler) viewableCategories(r *http.Request) ([]uuid.UUID, error) {
	if h.Perms == nil {
		return nil, nil
	}
	snap, err := h.Perms.Snapshot(r.Context())
	if err != nil {
		return nil, err
	}
	return snap.VisibleCategories(auth.UserFrom(r.Context()).Subject()), nil
}
