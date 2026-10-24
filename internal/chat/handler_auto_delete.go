package chat

import (
	"net/http"

	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// AutoDeleteRequest is the body of PUT /api/admin/channels/{id}/auto-delete.
type AutoDeleteRequest struct {
	// Days is how long messages stay: 1, 7, 30 or 90; 0 turns auto-delete off.
	Days int `json:"days" enums:"0,1,7,30,90"`
}

// setAutoDelete handles PUT /api/admin/channels/{id}/auto-delete.
//
// @Summary Set a channel's auto-delete
// @Description Opt-in per channel: a background sweeper removes messages older than 1, 7, 30 or 90 days, attachments included, and broadcasts message_delete for each. Pinned messages and thread roots that still have a newer or pinned reply stay. 0 turns it off. Direct messages never take part (404). Requires the Manage channels permission. Changing the setting is written to the audit log (channel.auto_delete_set), and every sweep that removed messages as channel.auto_delete_run with the counts. Needs the channels.auto_delete feature.
// @ID setChannelAutoDelete
// @Tags Admin
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "Resource ID." Format(uuid)
// @Param request body AutoDeleteRequest true "Request body."
// @Success 200 {object} Channel "Updated channel."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or days other than 0, 1, 7, 30, 90."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage channels permission, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/channels/{id}/auto-delete [put]
func (h *Handler) setAutoDelete(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	var req AutoDeleteRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if err := h.require(r, id, perm.ViewChannel); err != nil {
		return err
	}
	ch, err := SetAutoDelete(r.Context(), h.DB, id, req.Days)
	if err != nil {
		return err
	}
	h.channelsChanged()
	httpx.WriteJSON(w, http.StatusOK, ch)
	return nil
}
