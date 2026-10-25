package chat

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
)

func (h *Handler) mountDMPrefs(r chi.Router) {
	r.Put("/dms/{id}/hidden", httpx.Handle(h.setDMHidden))
	r.Put("/dms/favourites", httpx.Handle(h.setDMFavourites))
	r.Get("/users/me/dm-settings", httpx.Handle(h.getDMSettings))
	r.Put("/users/me/dm-settings", httpx.Handle(h.putDMSettings))
}

// DMPrefsUpdated is the payload of dm_prefs_updated, sent to the member's own
// sessions: DMID is the conversation to read again, null for the whole list.
type DMPrefsUpdated struct {
	DMID *uuid.UUID `json:"dm_id" format:"uuid" extensions:"x-nullable"`
}

func (h *Handler) tellOwnSessions(user uuid.UUID, ids ...*uuid.UUID) {
	if h.Events == nil {
		return
	}
	for _, id := range ids {
		h.Events.SendToUsers([]uuid.UUID{user}, events.DMPrefsUpdated, DMPrefsUpdated{DMID: id})
	}
}

// SetDMHiddenRequest is the body of PUT /api/dms/{id}/hidden.
type SetDMHiddenRequest struct {
	Hidden bool `json:"hidden"`
}

// setDMHidden handles PUT /api/dms/{id}/hidden.
//
// @Summary Close or reopen a direct message in the caller's list
// @Description Hides the conversation from the caller's list, or shows it again. Nothing is deleted and nobody else is told; the next message written in it shows it again. A favourite cannot be closed (409). Anyone who is not in the conversation gets 404. Sends dm_prefs_updated to the caller's other sessions.
// @ID setDirectMessageHidden
// @Tags Direct messages
// @Accept json
// @Security cookieAuth
// @Param id path string true "Conversation ID." Format(uuid)
// @Param request body SetDMHiddenRequest true "Request body."
// @Success 204 "Done."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the conversation is a favourite."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/dms/{id}/hidden [put]
func (h *Handler) setDMHidden(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	var req SetDMHiddenRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	me := auth.UserFrom(r.Context()).ID
	if err := SetDMHidden(r.Context(), h.DB, me, id, req.Hidden); err != nil {
		return err
	}
	h.tellOwnSessions(me, &id)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// SetDMFavouritesRequest is the body of PUT /api/dms/favourites.
type SetDMFavouritesRequest struct {
	// IDs are the favourite conversations, first to last; every other
	// conversation stops being one.
	IDs []uuid.UUID `json:"ids" maxItems:"25" format:"uuid"`
}

// setDMFavourites handles PUT /api/dms/favourites.
//
// @Summary Set the caller's favourite direct messages
// @Description Replaces the caller's favourites (at most 25) with the given conversations in the given order, which also reopens any that were closed. Every id must be a conversation the caller is in; otherwise nothing changes and the answer is 404, the same as for a conversation that does not exist. Sends dm_prefs_updated to the caller's sessions.
// @ID setDirectMessageFavourites
// @Tags Direct messages
// @Accept json
// @Security cookieAuth
// @Param request body SetDMFavouritesRequest true "Request body."
// @Success 204 "Done."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/dms/favourites [put]
func (h *Handler) setDMFavourites(w http.ResponseWriter, r *http.Request) error {
	var req SetDMFavouritesRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	me := auth.UserFrom(r.Context()).ID
	changed, err := SetDMFavourites(r.Context(), h.DB, me, req.IDs)
	if err != nil {
		return err
	}
	ids := make([]*uuid.UUID, len(changed))
	for i := range changed {
		ids[i] = &changed[i]
	}
	h.tellOwnSessions(me, ids...)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// getDMSettings handles GET /api/users/me/dm-settings.
//
// @Summary Read the direct message settings
// @Description The caller's settings for their conversations as a whole: mute all, and whether the sidebar section is folded. Who may start a conversation is in GET /api/users/me/privacy.
// @ID getDirectMessageSettings
// @Tags Direct messages
// @Produce json
// @Security cookieAuth
// @Success 200 {object} DMSettings "The settings."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/users/me/dm-settings [get]
func (h *Handler) getDMSettings(w http.ResponseWriter, r *http.Request) error {
	s, err := GetDMSettings(r.Context(), h.DB, auth.UserFrom(r.Context()).ID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, s)
	return nil
}

// putDMSettings handles PUT /api/users/me/dm-settings.
//
// @Summary Set the direct message settings
// @Description Stores both settings. With mute_all on, conversations send no push, play no sound and highlight nothing, but unread still counts; a conversation's own notification setting (PUT /api/channels/{id}/notifications) overrides it. Sends dm_prefs_updated (no conversation) to the caller's sessions.
// @ID setDirectMessageSettings
// @Tags Direct messages
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body DMSettings true "Request body."
// @Success 200 {object} DMSettings "The settings."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/users/me/dm-settings [put]
func (h *Handler) putDMSettings(w http.ResponseWriter, r *http.Request) error {
	var req DMSettings
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	me := auth.UserFrom(r.Context()).ID
	if err := SetDMSettings(r.Context(), h.DB, me, req); err != nil {
		return err
	}
	h.tellOwnSessions(me, nil)
	httpx.WriteJSON(w, http.StatusOK, req)
	return nil
}
