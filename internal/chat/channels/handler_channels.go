package channels

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// createCategory handles POST /api/admin/categories.
//
// @Summary Create a category
// @Description Requires the Manage channels permission (403 otherwise).
// @ID createCategory
// @Tags Admin
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body channels.CreateCategoryRequest true "Request body."
// @Success 201 {object} core.Category "Created category."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage channels permission, or cross-origin request rejected."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/categories [post]
func (a *API) ServeCreateCategory(w http.ResponseWriter, r *http.Request) error {
	var req CreateCategoryRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	cat, err := CreateCategory(r.Context(), a.DB, req.Name, req.SortOrder)
	if err != nil {
		return err
	}
	a.ChannelsChanged()
	httpx.WriteJSON(w, http.StatusCreated, cat)
	return nil
}

// deleteCategory handles DELETE /api/admin/categories/{id}.
//
// @Summary Delete a category
// @Description Its channels become uncategorized and keep its permission overwrites. A category with overwrites needs the Administrator permission and sight of every channel in it (404 otherwise); 409 when the category and its channels both have overwrites (they cannot be merged exactly). Requires the Manage channels permission (403 otherwise).
// @ID deleteCategory
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Param id path string true "Resource ID." Format(uuid)
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage channels permission (a category with permission overwrites also needs Administrator), or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/categories/{id} [delete]
func (a *API) ServeDeleteCategory(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	sub := auth.UserFrom(r.Context()).Subject()
	err = a.Guard.Perms.Locked(r.Context(), sub, func(tx pgx.Tx, snap *perm.Snapshot) error {
		if err := CheckWholeCategory(snap, sub, id); err != nil {
			return err
		}
		return DeleteCategoryTx(r.Context(), tx, id)
	})
	if err != nil {
		return err
	}
	a.structureChanged()
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// createChannel handles POST /api/admin/channels.
//
// @Summary Create a channel
// @Description Requires the Manage channels permission (403 otherwise).
// @ID createChannel
// @Tags Admin
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body channels.CreateChannelRequest true "Request body."
// @Success 201 {object} core.Channel "Created channel."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage channels permission, or cross-origin request rejected."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/channels [post]
func (a *API) CreateChannel(w http.ResponseWriter, r *http.Request) error {
	var req CreateChannelRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if req.Type == "" {
		req.Type = core.ChannelTypeText
	}
	if req.CategoryID != nil {
		if err := a.Guard.RequireCategory(r, *req.CategoryID); errors.Is(err, core.ErrCategoryNotFound) {
			return errNoCategory
		} else if err != nil {
			return err
		}
	}
	ch, err := CreateChannelWithStage(r.Context(), a.DB, req.CategoryID, req.Name, req.Type, req.Topic, req.SortOrder, req.IsStage)
	if err != nil {
		return err
	}
	a.structureChanged()
	httpx.WriteJSON(w, http.StatusCreated, ch)
	return nil
}

// duplicateChannel handles POST /api/admin/channels/{id}/duplicate.
//
// @Summary Duplicate a channel
// @Description Creates a channel with the source's name (unchanged; names need not be unique), type, topic and category, placed directly below the source: the channels after it in its category (or among the uncategorized channels) move down by one. Its permission overwrites are copied, so a copy of a private channel stays private. Messages, reactions, read state and per-user notification levels are not copied. No request body. Requires the Manage channels permission (403 otherwise).
// @ID duplicateChannel
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Param id path string true "Resource ID." Format(uuid)
// @Success 201 {object} core.Channel "Created channel."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage channels permission, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/channels/{id}/duplicate [post]
func (a *API) ServeDuplicateChannel(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	if err := a.Guard.Require(r, id, perm.ViewChannel); err != nil {
		return err
	}
	ch, err := DuplicateChannel(r.Context(), a.DB, id)
	if err != nil {
		return err
	}
	a.structureChanged()
	httpx.WriteJSON(w, http.StatusCreated, ch)
	return nil
}

// deleteChannel handles DELETE /api/admin/channels/{id}.
//
// @Summary Delete a channel
// @Description Cascades to its messages and attachments; everyone in a deleted voice channel is disconnected from it. Requires the Manage channels permission (403 otherwise).
// @ID deleteChannel
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Param id path string true "Resource ID." Format(uuid)
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage channels permission, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/channels/{id} [delete]
func (a *API) ServeDeleteChannel(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	if err := a.Guard.Require(r, id, perm.ViewChannel); err != nil {
		return err
	}
	keys, err := DeleteChannel(r.Context(), a.DB, id)
	if err != nil {
		return err
	}
	// Nobody may stay connected to the voice room of a channel that is gone.
	if v := a.Voice; v != nil {
		v.CloseVoiceChannel(id)
	}
	core.DeleteObjects(r.Context(), a.Objects, keys)
	a.structureChanged()
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// updateChannel handles PATCH /api/admin/channels/{id}.
//
// @Summary Rename a channel, change its topic or member limit
// @Description Requires the Manage channels permission (403 otherwise). user_limit (voice channels only) caps how many members can be in the room at once; 0 removes the limit (the default). A member already in the room always gets back in. is_lobby (permanent voice channels only) makes the Talk a Create-a-Talk lobby. is_stage (permanent voice channels that are no lobby) makes it a Stage, or a normal Talk again.
// @ID updateChannel
// @Tags Admin
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "Resource ID." Format(uuid)
// @Param request body channels.UpdateChannelRequest true "Request body."
// @Success 200 {object} core.Channel "Updated channel."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage channels permission, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/channels/{id} [patch]
func (a *API) UpdateChannel(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	var req UpdateChannelRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if err := a.Guard.Require(r, id, perm.ViewChannel); err != nil {
		return err
	}
	ch, err := EditChannel(r.Context(), a.DB, id, req.Name, req.Topic, req.UserLimit, req.SlowModeSeconds, req.IsLobby, req.IsStage)
	if err != nil {
		return err
	}
	if err := FillTalkFields(r.Context(), a.DB, ch); err != nil {
		return err
	}
	if req.IsStage != nil {
		a.structureChanged() // the hub re-checks who may still send media
	} else {
		a.ChannelsChanged()
	}
	httpx.WriteJSON(w, http.StatusOK, ch)
	return nil
}

// renameCategory handles PATCH /api/admin/categories/{id}.
//
// @Summary Rename a category
// @Description Requires the Manage channels permission (403 otherwise).
// @ID renameCategory
// @Tags Admin
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "Resource ID." Format(uuid)
// @Param request body channels.RenameCategoryRequest true "Request body."
// @Success 200 {object} channels.RenamedCategory "Renamed category."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage channels permission, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/categories/{id} [patch]
func (a *API) ServeRenameCategory(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	var req RenameCategoryRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if err := a.Guard.RequireCategory(r, id); err != nil {
		return err
	}
	name, err := RenameCategory(r.Context(), a.DB, id, req.Name)
	if err != nil {
		return err
	}
	a.ChannelsChanged()
	httpx.WriteJSON(w, http.StatusOK, RenamedCategory{ID: id, Name: name})
	return nil
}

// applyLayout handles PUT /api/admin/layout.
//
// @Summary Apply category order and channel placement
// @Description Applied in one transaction. Requires the Manage channels permission (403 otherwise).
// @ID applyLayout
// @Tags Admin
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body channels.LayoutRequest true "Request body."
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage channels permission, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/layout [put]
func (a *API) ServeApplyLayout(w http.ResponseWriter, r *http.Request) error {
	var req LayoutRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if len(req.Categories)+len(req.Channels) > 500 {
		return httpx.ErrInvalidInput("too many entries")
	}
	sub := auth.UserFrom(r.Context()).Subject()
	err := a.Guard.Perms.Locked(r.Context(), sub, func(tx pgx.Tx, snap *perm.Snapshot) error {
		if err := CheckLayout(snap, sub, req); err != nil {
			return err
		}
		return ApplyLayoutTx(r.Context(), tx, req.Categories, req.Channels)
	})
	if err != nil {
		return err
	}
	a.structureChanged()
	w.WriteHeader(http.StatusNoContent)
	return nil
}
