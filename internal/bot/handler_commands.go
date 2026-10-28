package bot

import (
	"net/http"

	"github.com/msch128/mnema-talk/internal/httpx"
)

// CommandPatch changes a command; only an admin can.
type CommandPatch struct {
	Disabled *bool `json:"disabled" binding:"required"`
}

// listCommands handles GET /api/admin/bots/{id}/commands.
//
// @Summary List a bot's commands
// @Description All of them, including the disabled ones.
// @ID adminListBotCommands
// @Tags Bots
// @Produce json
// @Security cookieAuth
// @Param id path string true "Bot ID." Format(uuid)
// @Success 200 {array} Command "Commands by name."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage bots permission, or the bot's highest role is not below yours."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such bot."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/bots/{id}/commands [get]
func (h *Handler) listCommands(w http.ResponseWriter, r *http.Request) error {
	b, _, err := h.load(r)
	if err != nil {
		return err
	}
	list, err := ListCommands(r.Context(), h.DB, b.ID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, list)
	return nil
}

// patchCommand handles PATCH /api/admin/bots/{id}/commands/{commandID}.
//
// @Summary Enable or disable a command
// @Description A disabled command is hidden from members and refuses invocations. The bot cannot switch it back on, not even by registering the command again.
// @ID adminPatchBotCommand
// @Tags Bots
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "Bot ID." Format(uuid)
// @Param commandID path string true "Command ID." Format(uuid)
// @Param body body CommandPatch true "The new state."
// @Success 200 {object} Command "Updated."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT)."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage bots permission, or the bot's highest role is not below yours, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such bot or command."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/bots/{id}/commands/{commandID} [patch]
func (h *Handler) patchCommand(w http.ResponseWriter, r *http.Request) error {
	b, err := h.loadActing(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathUUID(r, "commandID")
	if err != nil {
		return err
	}
	var req CommandPatch
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if req.Disabled == nil {
		return httpx.ErrInvalidInput("disabled is required")
	}
	c, err := SetCommandDisabled(r.Context(), h.DB, b.ID, id, *req.Disabled)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, c)
	return nil
}

// deleteCommand handles DELETE /api/admin/bots/{id}/commands/{commandID}.
//
// @Summary Delete a command
// @Description The bot can register it again; disable it instead to keep it off.
// @ID adminDeleteBotCommand
// @Tags Bots
// @Security cookieAuth
// @Param id path string true "Bot ID." Format(uuid)
// @Param commandID path string true "Command ID." Format(uuid)
// @Success 204 "Deleted."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT)."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage bots permission, or the bot's highest role is not below yours, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such bot or command."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/bots/{id}/commands/{commandID} [delete]
func (h *Handler) deleteCommand(w http.ResponseWriter, r *http.Request) error {
	b, err := h.loadActing(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathUUID(r, "commandID")
	if err != nil {
		return err
	}
	if err := DeleteCommand(r.Context(), h.DB, b.ID, id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
