package bot

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// RolesRequest replaces a bot's roles.
type RolesRequest struct {
	RoleIDs []uuid.UUID `json:"role_ids" format:"uuid"`
}

// Access is what a bot can reach with its roles and overwrites.
type Access struct {
	// Permissions are the server-wide permissions (a bit set).
	Permissions perm.Set `json:"permissions" swaggertype:"integer" minimum:"0"`
	// Elevated is true when the bot holds Administrator or opens any part of
	// the admin area: the interface warns about it.
	Elevated bool            `json:"elevated"`
	Channels []ChannelAccess `json:"channels"`
}

// ChannelAccess is what a bot may do in one channel it can view.
type ChannelAccess struct {
	ChannelID   uuid.UUID `json:"channel_id" format:"uuid"`
	Name        string    `json:"name"`
	Permissions perm.Set  `json:"permissions" swaggertype:"integer" minimum:"1"`
}

// setRoles handles PUT /api/admin/bots/{id}/roles.
//
// @Summary Set a bot's roles
// @Description Replaces the bot's roles. The usual hierarchy applies: only roles below the caller's highest role, and no permission the caller lacks. A role with Administrator or any admin-area permission can only be given by the Owner. The bot never inherits @everyone, always keeps the shared Bot role and its own managed role, and another bot's own role is refused.
// @ID setBotRoles
// @Tags Bots
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "Bot ID." Format(uuid)
// @Param body body RolesRequest true "The bot's roles."
// @Success 200 {object} Bot "Updated."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT)."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage bots permission, a role is not below your highest role, or it holds a permission you lack (roles with Administrator or admin-area permissions: Owner only), or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such bot."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/bots/{id}/roles [put]
func (h *Handler) setRoles(w http.ResponseWriter, r *http.Request) error {
	b, err := h.loadActing(r)
	if err != nil {
		return err
	}
	var req RolesRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if err := h.Perms.SetBotRoles(r.Context(), auth.UserFrom(r.Context()).Subject(), b.UserID, req.RoleIDs); err != nil {
		return err
	}
	return h.writeCurrent(w, r, b.ID)
}

// access handles GET /api/admin/bots/{id}/access.
//
// @Summary Show what a bot can reach
// @Description The effective result of the bot's roles and the channel overwrites: its server-wide permissions and, per channel it can view, what it may do there.
// @ID getBotAccess
// @Tags Bots
// @Produce json
// @Security cookieAuth
// @Param id path string true "Bot ID." Format(uuid)
// @Success 200 {object} Access "Effective access."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage bots permission, or the bot's highest role is not below yours."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such bot."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/bots/{id}/access [get]
func (h *Handler) access(w http.ResponseWriter, r *http.Request) error {
	b, snap, err := h.load(r)
	if err != nil {
		return err
	}
	sub := perm.Subject{ID: b.UserID}
	out := Access{Permissions: snap.Global(sub), Channels: []ChannelAccess{}}
	out.Elevated = snap.ElevatedFor(sub)
	// Only channels the caller can see: names of others stay theirs.
	caller := auth.UserFrom(r.Context()).Subject()
	perms := map[uuid.UUID]perm.Set{}
	for ch, p := range snap.ChannelPermissions(sub) {
		if snap.Channel(caller, ch).Has(perm.ViewChannel) {
			perms[ch] = p
		}
	}
	names, err := channelNames(r.Context(), h.DB)
	if err != nil {
		return err
	}
	for id, p := range perms {
		out.Channels = append(out.Channels, ChannelAccess{ChannelID: id, Name: names[id], Permissions: p})
	}
	slices.SortFunc(out.Channels, func(a, b ChannelAccess) int {
		return cmp.Or(strings.Compare(a.Name, b.Name), bytes.Compare(a.ChannelID[:], b.ChannelID[:]))
	})
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

func channelNames(ctx context.Context, q db.Querier) (map[uuid.UUID]string, error) {
	type row struct {
		ID   uuid.UUID
		Name string
	}
	var list []row
	rows, err := q.Query(ctx, `SELECT id, name FROM channels WHERE type <> 'dm'`)
	if err == nil {
		list, err = pgx.CollectRows(rows, pgx.RowToStructByPos[row])
	}
	if err != nil {
		return nil, fmt.Errorf("channel names: %w", err)
	}
	out := make(map[uuid.UUID]string, len(list))
	for _, r := range list {
		out[r.ID] = r.Name
	}
	return out, nil
}
