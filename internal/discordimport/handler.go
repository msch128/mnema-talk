package discordimport

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// ImportsPerMinute is how many previews and imports one administrator may run
// per minute; a preview makes an outbound request to Discord.
const ImportsPerMinute = 10

// Handler serves the import routes under /api/admin/import/discord.
type Handler struct {
	Importer *Importer
	Perms    *perm.Service
	// Limit caps previews and imports per member; nil means no cap.
	Limit *httpx.RateLimiter
}

// MountAdmin registers the routes. Only the Owner and members with the
// Administrator permission pass (RequireUser applies).
func (h *Handler) MountAdmin(r chi.Router) {
	adm := r.With(auth.RequirePerm(h.Perms, perm.Administrator))
	adm.Post("/import/discord/preview", httpx.Handle(h.preview))
	adm.Post("/import/discord/apply", httpx.Handle(h.apply))
	adm.Get("/import/discord/imports", httpx.Handle(h.list))
	adm.Get("/import/discord/imports/{id}", httpx.Handle(h.detail))
	adm.Post("/import/discord/imports/{id}/undo", httpx.Handle(h.undo))
}

func (h *Handler) allow(r *http.Request) error {
	if h.Limit == nil {
		return nil
	}
	if ok, _ := h.Limit.Allow(auth.UserKey(r)); !ok {
		return httpx.ErrRateLimited()
	}
	return nil
}

// PreviewRequest is the body of the preview call.
type PreviewRequest struct {
	// Template is a Discord server template link (discord.new/<code>,
	// discord.com/template/<code>) or the bare code.
	Template string `json:"template" maxLength:"300"`
	// RoleMap maps Discord role ids onto existing role ids: those roles are
	// left out of the plan and their overwrites point at the existing role.
	RoleMap map[string]string `json:"role_map,omitempty" binding:"optional"`
}

// preview handles POST /api/admin/import/discord/preview.
//
// @Summary Preview a Discord server import
// @Description Downloads the Discord server template (discord.com only, 2 MB at most, no redirects) and returns exactly what importing it would create: categories, channels, roles with their mapped permissions, and the Discord permissions that are dropped. An optional role_map (Discord role id to existing role id) leaves those roles out of the plan; the suggested_role_id of each role names the existing role with the same name. Writes nothing and is not audited. Owner and Administrators only. 404 when the template does not exist or is not shared, 502/503 when Discord cannot be reached.
// @ID previewDiscordImport
// @Tags Admin
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body PreviewRequest true "Request body."
// @Success 200 {object} Plan "The plan."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): not a template link or code, malformed JSON, unknown JSON fields or an invalid role_map."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: only the Owner and Administrators may import, a mapped role is managed, held by a bot or not below the importer's highest role, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the template does not exist or is no longer shared."
// @Failure 413 {object} httpx.ErrorResponse "The template is larger than 2 MB or has more channels or roles than Discord allows."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Failure 503 {object} httpx.ErrorResponse "Discord could not be reached."
// @Router /api/admin/import/discord/preview [post]
func (h *Handler) preview(w http.ResponseWriter, r *http.Request) error {
	var req PreviewRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if err := h.allow(r); err != nil {
		return err
	}
	plan, err := h.Importer.Preview(r.Context(), auth.UserFrom(r.Context()).Subject(), req.Template, req.RoleMap)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, plan)
	return nil
}

// apply handles POST /api/admin/import/discord/apply.
//
// @Summary Import a Discord server structure
// @Description Downloads the template again and creates the categories, channels and roles of the plan in one transaction, strictly additively: categories and channels are appended at the end of the layout, roles are created below every existing role without Administrator, and @everyone is not touched. Roles listed in role_map are not created: their channel overwrites are written for the existing role, whose own permissions stay as they are (managed roles such as Bot, roles above the importer and, unless the Owner imports, roles held by bots are refused). The request must repeat the plan_hash of the preview and carry all three confirmations (confirm_additive, confirm_roles, and confirm_text: the server name or IMPORT); a changed template or server state answers 409 and needs a new preview. Writes one audit entry (import.discord). Owner and Administrators only.
// @ID applyDiscordImport
// @Tags Admin
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body ApplyRequest true "Request body."
// @Success 201 {object} Record "The recorded import."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): a confirmation is missing or wrong, the template contains nothing to import, or the role_map is invalid."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: only the Owner and Administrators may import, a mapped role is managed, held by a bot or not below the importer's highest role, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the template does not exist or is no longer shared."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the preview is out of date, or the import would exceed the role limit."
// @Failure 413 {object} httpx.ErrorResponse "The template is too large."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Failure 503 {object} httpx.ErrorResponse "Discord could not be reached."
// @Router /api/admin/import/discord/apply [post]
func (h *Handler) apply(w http.ResponseWriter, r *http.Request) error {
	var req ApplyRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if err := h.allow(r); err != nil {
		return err
	}
	u := auth.UserFrom(r.Context())
	rec, err := h.Importer.Apply(r.Context(), u.Subject(), u.DisplayName, req)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, rec)
	return nil
}

// list handles GET /api/admin/import/discord/imports.
//
// @Summary List Discord imports
// @Description The recorded imports, newest first (at most 100). Owner and Administrators only.
// @ID listDiscordImports
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Success 200 {array} Record "Imports."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: only the Owner and Administrators may read it."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/import/discord/imports [get]
func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	recs, err := h.Importer.List(r.Context())
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, recs)
	return nil
}

// detail handles GET /api/admin/import/discord/imports/{id}.
//
// @Summary Read a Discord import
// @Description The import and what an undo would do now: how much of it still exists, which created channels have messages (those need a separate confirmation) and which created categories stay because they hold other channels. Owner and Administrators only.
// @ID getDiscordImport
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Param id path string true "Resource ID." Format(uuid)
// @Success 200 {object} Detail "The import."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: only the Owner and Administrators may read it."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/import/discord/imports/{id} [get]
func (h *Handler) detail(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	d, err := h.Importer.Detail(r.Context(), id)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, d)
	return nil
}

// undo handles POST /api/admin/import/discord/imports/{id}/undo.
//
// @Summary Undo a Discord import
// @Description Removes exactly the categories, channels and roles this import created and nothing else. Objects deleted by hand since are skipped; a created category that now holds other channels stays. A created channel that has messages now needs accept_message_loss (409 otherwise): its messages are deleted with it. Members who were given a created role lose it. Writes one audit entry (import.discord_undo). Owner and Administrators only.
// @ID undoDiscordImport
// @Tags Admin
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "Resource ID." Format(uuid)
// @Param request body UndoRequest true "Request body."
// @Success 200 {object} UndoResult "What was removed."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: only the Owner and Administrators may undo an import, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: already undone, or created channels have messages and accept_message_loss is not set."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/import/discord/imports/{id}/undo [post]
func (h *Handler) undo(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	var req UndoRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	res, err := h.Importer.Undo(r.Context(), auth.UserFrom(r.Context()).Subject(), id, req)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, res)
	return nil
}
