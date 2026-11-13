package bot

import (
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// AccessRequest is the access choice of a bot (doc/bots.md, "Give a bot
// access to every channel").
type AccessRequest struct {
	// Access is "channels" (the permissions only in the listed channels) or
	// "all" (the permissions in every channel, held server-wide by the bot's
	// own role; a channel plain members cannot see stays hidden unless it is
	// listed).
	Access string `json:"access" enums:"channels,all"`
	// Permissions are permission keys, at most the channel permissions a
	// manifest may ask for: view_channel, send_messages, attach_files,
	// add_reactions, manage_messages, mention_everyone. They replace what the
	// choice granted before.
	Permissions []string `json:"permissions"`
	// ChannelIDs are the channels of the bot's own overwrites (with access
	// "all": the private channels it may also see). Overwrites in channels
	// the caller cannot view are left as they are.
	ChannelIDs []uuid.UUID `json:"channel_ids" format:"uuid" maxItems:"500"`
}

// setAccess handles PUT /api/admin/bots/{id}/access.
//
// @Summary Choose which channels a bot works in
// @Description Owner or Manage bots. Sets the bot's access: "channels" gives the listed permissions to the bot's own role in exactly the listed channels (its other server-wide channel grants and overwrites go), "all" gives them to the bot's own role server-wide, so the bot works in every channel plain members can see; a channel where @everyone is denied View channels stays hidden from it unless it is listed. Only these channel permissions can be given: view, send, attach, react, manage messages, mention everyone; never Administrator or the admin area. Overwrites of the bot's own role in channels the caller cannot view stay. The usual bot invariant applies: nobody but the Owner gives a bot anything elevated, and nobody gives a bot a permission they lack in a channel it reaches. A bot that reaches a channel the caller cannot view cannot be changed by the caller either. Logged as bot.access.
// @ID setBotAccess
// @Tags Bots
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "Bot ID." Format(uuid)
// @Param body body AccessRequest true "The access choice."
// @Success 200 {object} Access "The access now."
// @Failure 400 {object} httpx.ErrorResponse "INVALID_INPUT: an unknown access or permission key."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage bots permission, the bot's highest role is not below yours, a permission you lack, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such bot, or a listed channel does not exist or you cannot view it."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: a listed channel is a temporary Talk."
// @Failure 413 {object} httpx.ErrorResponse "PAYLOAD_TOO_LARGE: the body exceeds 1 MiB."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/bots/{id}/access [put]
func (h *Handler) setAccess(w http.ResponseWriter, r *http.Request) error {
	b, err := h.loadActing(r)
	if err != nil {
		return err
	}
	var req AccessRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	access, err := planAccess(req)
	if err != nil {
		return err
	}
	if err := h.Perms.SetBotAccess(r.Context(), auth.UserFrom(r.Context()).Subject(), b.UserID, access); err != nil {
		return err
	}
	h.membersChanged()
	snap, err := h.Perms.Snapshot(r.Context())
	if err != nil {
		return err
	}
	out, err := h.currentAccess(r, b, snap)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

func planAccess(req AccessRequest) (perm.BotAccess, error) {
	var a perm.BotAccess
	switch req.Access {
	case AccessAll:
		a.All = true
	case AccessChannels:
	default:
		return a, httpx.ErrInvalidInput(`access must be "channels" or "all"`)
	}
	keys, err := permissionsByKey(req.Permissions)
	if err != nil {
		return a, err
	}
	for _, k := range keys {
		a.Allow |= k.Bit
	}
	a.Channels = uniqueChannels(req.ChannelIDs)
	if len(a.Channels) > maxInstallChannels {
		return a, httpx.ErrInvalidInput(fmt.Sprintf("at most %d channels", maxInstallChannels))
	}
	return a, nil
}

// addChoice fills the access choice of the bot: what it grants and where the
// bot's own role has overwrites, within the channels the caller can view.
func (h *Handler) addChoice(r *http.Request, snap *perm.Snapshot, botUser uuid.UUID, out *Access) error {
	ctx := r.Context()
	var server perm.Set
	if err := h.DB.QueryRow(ctx, `SELECT permissions FROM roles WHERE bot_user_id = $1`, botUser).Scan(&server); err != nil {
		return fmt.Errorf("load bot role: %w", err)
	}
	rows, err := h.DB.Query(ctx, `
		SELECT o.channel_id, o.allow FROM permission_overwrites o JOIN roles r ON r.id = o.role_id
		WHERE r.bot_user_id = $1 AND o.channel_id IS NOT NULL AND o.allow & $2::bigint <> 0`, botUser, int64(perm.BotChannelPerms))
	if err != nil {
		return fmt.Errorf("load bot overwrites: %w", err)
	}
	type row struct {
		Channel uuid.UUID
		Allow   perm.Set
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[row])
	if err != nil {
		return fmt.Errorf("load bot overwrites: %w", err)
	}
	caller := auth.UserFrom(ctx).Subject()
	out.SelectedChannelIDs = []uuid.UUID{}
	if out.Mode == AccessAll {
		out.Granted = server & perm.BotChannelPerms
	}
	for _, o := range list {
		if !snap.Channel(caller, o.Channel).Has(perm.ViewChannel) {
			continue
		}
		out.SelectedChannelIDs = append(out.SelectedChannelIDs, o.Channel)
		if out.Mode == AccessChannels {
			out.Granted |= o.Allow & perm.BotChannelPerms
		}
	}
	return nil
}
