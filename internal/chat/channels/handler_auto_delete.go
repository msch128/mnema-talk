package channels

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// ServeSetAutoDelete handles PUT /api/admin/channels/{id}/auto-delete.
//
// @Summary Set a channel's auto-delete
// @Description A background sweeper removes what is older than the period: with scope "all" the messages with their attachments (broadcasting message_delete for each), with "attachments" only the files (the messages stay and show the files as removed; message_update for each). Pinned messages and their files stay, and so do thread roots that still have a newer or pinned reply. hours (0 to 8760) wins over the older days (0, 1, 7, 30, 90); 0 turns it off for this channel even when the community has a default; use_default makes the channel follow the community default. Conversations and their call Talks never take part (404). Requires the Manage channels permission. Changing the setting is written to the audit log (channel.auto_delete_set), and every sweep that removed something as channel.auto_delete_run with the counts. Needs the channels.auto_delete feature; hours, scope and use_default need channels.auto_delete.v2.
// @ID setChannelAutoDelete
// @Tags Admin
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "Resource ID." Format(uuid)
// @Param request body channels.AutoDeleteRequest true "Request body."
// @Success 200 {object} core.Channel "Updated channel."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs, days other than 0, 1, 7, 30, 90, hours outside 0 to 8760, an unknown scope, or none of hours, days and use_default."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage channels permission, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/channels/{id}/auto-delete [put]
func (a *API) ServeSetAutoDelete(w http.ResponseWriter, r *http.Request) error {
	id, setting, err := a.autoDeleteInput(r)
	if err != nil {
		return err
	}
	ch, err := SetAutoDelete(r.Context(), a.DB, id, setting)
	if err != nil {
		return err
	}
	a.ChannelsChanged()
	httpx.WriteJSON(w, http.StatusOK, ch)
	return nil
}

// ServePreviewAutoDelete handles POST /api/admin/channels/{id}/auto-delete/preview.
//
// @Summary Preview a channel's auto-delete
// @Description A dry run of PUT /api/admin/channels/{id}/auto-delete with the same body: how many messages and files the setting would remove at its next run, and when that is. Changes nothing. Requires the Manage channels permission in the channel; the counts are of a channel the caller can already read. Needs the channels.auto_delete.v2 feature.
// @ID previewChannelAutoDelete
// @Tags Admin
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "Resource ID." Format(uuid)
// @Param request body channels.AutoDeleteRequest true "Request body."
// @Success 200 {object} channels.AutoDeletePreview "What would be removed."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): as for setting it."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage channels permission, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/channels/{id}/auto-delete/preview [post]
func (a *API) ServePreviewAutoDelete(w http.ResponseWriter, r *http.Request) error {
	id, setting, err := a.autoDeleteInput(r)
	if err != nil {
		return err
	}
	var community bool
	if err := a.DB.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM channels c WHERE id = $1 AND NOT `+core.PrivateChannelSQL("c")+`)`, id).Scan(&community); err != nil {
		return err
	}
	if !community {
		return core.ErrChannelNotFound
	}
	h, scope, err := a.effective(r, setting)
	if err != nil {
		return err
	}
	out, err := PreviewAutoDelete(r.Context(), a.DB, []uuid.UUID{id}, h, scope)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

// autoDeleteInput reads the channel id and the setting of an auto-delete
// request and checks that the caller may see the channel (the route already
// requires Manage channels).
func (a *API) autoDeleteInput(r *http.Request) (uuid.UUID, AutoDeleteSetting, error) {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return uuid.Nil, AutoDeleteSetting{}, err
	}
	var req AutoDeleteRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return uuid.Nil, AutoDeleteSetting{}, err
	}
	if err := a.Guard.Require(r, id, perm.ViewChannel); err != nil {
		return uuid.Nil, AutoDeleteSetting{}, err
	}
	setting, err := req.Setting()
	return id, setting, err
}

// effective is the period and scope a setting comes to: the community
// default when it follows that.
func (a *API) effective(r *http.Request, s AutoDeleteSetting) (int, string, error) {
	if s.Hours != nil {
		return *s.Hours, s.Scope, nil
	}
	set, err := core.LoadMessageSettings(r.Context(), a.DB)
	return set.AutoDeleteDefaultHours, set.AutoDeleteDefaultScope, err
}
