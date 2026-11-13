package bot

import (
	"context"
	"fmt"
	"net/http"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// The values of an access choice (doc/bots.md, "Give a bot access to every
// channel").
const (
	AccessChannels = "channels"
	AccessAll      = "all"
)

// maxInstallChannels bounds the channels of one install.
const maxInstallChannels = 500

// InstallRequest is the body of adding a bot from a manifest: the manifest
// and what the installer kept ticked on the confirm screen.
type InstallRequest struct {
	Manifest Manifest `json:"manifest"`
	// Permissions are keys from the manifest's permissions; none means none.
	Permissions []string `json:"permissions"`
	// Intents are names from the manifest's intents; none means none.
	Intents []string `json:"intents"`
	// Access is "channels" (the default: the kept permissions only in the
	// listed channels) or "all" (the kept permissions in every channel, held
	// server-wide by the bot's own role; a channel plain members cannot see
	// stays hidden unless it is listed).
	Access string `json:"access,omitempty" binding:"optional" enums:"channels,all"`
	// ChannelIDs are the channels the bot may work in (none: it sees nothing
	// yet); with access "all" the private channels it may also see. Each must
	// be a channel the installer can view.
	ChannelIDs []uuid.UUID `json:"channel_ids" format:"uuid" maxItems:"500"`
}

// InstallResponse is the added bot and its first token, whose secret is
// shown only now.
type InstallResponse struct {
	Bot   Bot              `json:"bot"`
	Token NewTokenResponse `json:"token"`
}

// installPlan is a validated install.
type installPlan struct {
	manifest CheckedManifest
	perms    perm.Set
	intents  int64
	all      bool
	channels []uuid.UUID
}

// checkManifest handles POST /api/admin/bots/manifests/check.
//
// @Summary Check a bot manifest
// @Description Owner or Manage bots. Validates a manifest and answers it normalised: cleaned text, its intents with their bits and its permissions with their names, in a fixed order, without duplicates. Unknown fields are ignored; the avatar is not part of the answer. Nothing is stored.
// @ID checkBotManifest
// @Tags Bots
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param body body Manifest true "The manifest."
// @Success 200 {object} CheckedManifest "The manifest as the server understands it."
// @Failure 400 {object} httpx.ErrorResponse "INVALID_INPUT: the manifest is invalid or asks for a permission a manifest cannot ask for; the message names it."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage bots permission, or cross-origin request rejected."
// @Failure 413 {object} httpx.ErrorResponse "PAYLOAD_TOO_LARGE: the body exceeds 1 MiB."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/bots/manifests/check [post]
func (h *Handler) checkManifest(w http.ResponseWriter, r *http.Request) error {
	var m Manifest
	if err := httpx.DecodeJSON(r, &m); err != nil {
		return err
	}
	checked, err := m.Check()
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, checked)
	return nil
}

// planInstall validates the request: the kept permissions and intents must be
// the manifest's, the channels are deduplicated.
func planInstall(req InstallRequest) (installPlan, error) {
	m, err := req.Manifest.Check()
	if err != nil {
		return installPlan{}, err
	}
	p := installPlan{manifest: m}
	for _, k := range req.Permissions {
		i := slices.IndexFunc(m.Permissions, func(mp ManifestPermission) bool { return mp.Key == k })
		if i < 0 {
			return installPlan{}, httpx.ErrInvalidInput("permission not in the manifest: " + clipForError(k))
		}
		p.perms |= m.Permissions[i].Bit
	}
	for _, n := range req.Intents {
		i := slices.IndexFunc(m.Intents, func(mi ManifestIntent) bool { return mi.Name == n })
		if i < 0 {
			return installPlan{}, httpx.ErrInvalidInput("intent not in the manifest: " + clipForError(n))
		}
		p.intents |= m.Intents[i].Bit
	}
	switch req.Access {
	case "", AccessChannels:
	case AccessAll:
		p.all = true
	default:
		return installPlan{}, httpx.ErrInvalidInput(`access must be "channels" or "all"`)
	}
	p.channels = uniqueChannels(req.ChannelIDs)
	if len(p.channels) > maxInstallChannels {
		return installPlan{}, httpx.ErrInvalidInput(fmt.Sprintf("at most %d channels", maxInstallChannels))
	}
	return p, nil
}

// uniqueChannels sorts and deduplicates.
func uniqueChannels(ids []uuid.UUID) []uuid.UUID {
	return slices.Compact(slices.SortedFunc(slices.Values(ids), uuidCompare))
}

func uuidCompare(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) }

// install handles POST /api/admin/bots/install.
//
// @Summary Add a bot from a manifest
// @Description Owner or Manage bots. In one transaction under the permission lock: creates the bot with the manifest's name, description and homepage and the kept intents, allows the kept permissions to the bot's own role in each listed channel (nothing server-wide), or, with access "all", gives them to the bot's own role server-wide, so the bot works in every channel plain members can see (a channel they cannot see stays hidden from it unless it is listed), registers the manifest's commands and issues a token named after the bot, whose secret is in the answer and shown only once. The kept permissions and intents must be the manifest's; in each channel the installer must hold every kept permission (the Owner holds all), and with access "all" the installer must hold them in every channel the bot reaches. Anything refused creates nothing. Logged as bot.installed.
// @ID installBot
// @Tags Bots
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param body body InstallRequest true "The manifest and what was kept ticked."
// @Success 201 {object} InstallResponse "Added; the token's secret is shown once."
// @Failure 400 {object} httpx.ErrorResponse "INVALID_INPUT: the manifest is invalid, or a kept permission or intent is not in it; the message names it."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage bots permission, a kept permission the installer lacks in a listed channel (the message names both), or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: a listed channel does not exist, is not a channel, or the installer cannot view it."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the bot limit is reached, or a listed channel is a temporary Talk."
// @Failure 413 {object} httpx.ErrorResponse "PAYLOAD_TOO_LARGE: the body exceeds 1 MiB."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/bots/install [post]
func (h *Handler) install(w http.ResponseWriter, r *http.Request) error {
	var req InstallRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	plan, err := planInstall(req)
	if err != nil {
		return err
	}
	ctx := r.Context()
	actor := auth.UserFrom(ctx)
	var run installRun
	err = h.Perms.LockedCreatingBot(ctx, actor.Subject(), func(tx pgx.Tx, snap *perm.Snapshot) error {
		return h.installIn(ctx, tx, snap, actor, plan, &run)
	})
	if err != nil {
		return run.explain(err)
	}
	// Nothing after the commit may fail: the answer is the only place the
	// token's secret is ever shown, so it was read in the transaction.
	h.Perms.Changed(nil)
	h.membersChanged()
	httpx.WriteJSON(w, http.StatusCreated, InstallResponse{Bot: *run.bot, Token: NewTokenResponse{Token: run.token, Secret: run.secret}})
	return nil
}

// readBack loads the new bot with its roles in the install's transaction, for
// the answer.
func readBack(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Bot, error) {
	b, err := Get(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT role_id FROM user_roles WHERE user_id = $1 ORDER BY role_id`, b.UserID)
	if err != nil {
		return nil, fmt.Errorf("load bot roles: %w", err)
	}
	if b.RoleIDs, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID]); err != nil {
		return nil, fmt.Errorf("load bot roles: %w", err)
	}
	return b, nil
}

// installRun is what one install did, and what it needs to explain a refusal.
type installRun struct {
	botID uuid.UUID
	// bot is the installed bot as the answer shows it, read in the transaction.
	bot    *Bot
	token  Token
	secret string
	// written is set once every write is done: a refusal after it is the bot
	// invariant's.
	written bool
	// lacking is the first kept permission the installer did not hold in a
	// listed channel, as the snapshot of the transaction saw it.
	lacking, lackingIn string
}

// explain names the permission and the channel when the bot invariant refused
// the install, instead of its generic message.
func (run *installRun) explain(err error) error {
	if ae, ok := httpx.AsAPIError(err); ok && ae.Status == http.StatusForbidden && run.written && run.lacking != "" {
		if run.lackingIn == "" {
			return httpx.ErrForbidden(fmt.Sprintf("you do not have %s everywhere, so the bot cannot get it in every channel", run.lacking))
		}
		return httpx.ErrForbidden(fmt.Sprintf("you do not have %s in #%s, so the bot cannot get it there", run.lacking, run.lackingIn))
	}
	return wrapCreate(err)
}

// installIn does the install in the locked transaction.
func (h *Handler) installIn(ctx context.Context, tx pgx.Tx, snap *perm.Snapshot, actor *auth.User, plan installPlan, run *installRun) error {
	// Checked again under the lock: Manage bots may have been taken away
	// since the route's guard let the request in.
	if name := snap.Global(actor.Subject()).MissingName(perm.ManageBots); name != "" {
		return httpx.ErrForbidden("missing permission: " + name)
	}
	if err := checkInstallChannels(ctx, tx, snap, actor.Subject(), plan, run); err != nil {
		return err
	}
	m := plan.manifest
	id, err := createIn(ctx, tx, m.Name, plan.intents, actor.ID)
	if err != nil {
		return err
	}
	run.botID = id
	role, err := setProfile(ctx, tx, id, m.Description, m.Homepage)
	if err != nil {
		return err
	}
	if plan.perms != 0 || plan.all {
		access := perm.BotAccess{Role: role, All: plan.all, Allow: plan.perms, Channels: plan.channels}
		if err := perm.ApplyBotAccess(ctx, tx, access); err != nil {
			return err
		}
	}
	names, err := collectUniqueCommandNames(m.Commands)
	if err != nil {
		return err
	}
	if err := replaceCommandsTx(ctx, tx, id, names, m.Commands); err != nil {
		return err
	}
	if run.token, run.secret, err = finishIssue(insertToken(ctx, tx, id, m.Name, nil, actor.ID)); err != nil {
		return err
	}
	if err := h.Audit.RecordTx(ctx, tx, installEntry(id, plan)); err != nil {
		return fmt.Errorf("audit install: %w", err)
	}
	if run.bot, err = readBack(ctx, tx, id); err != nil {
		return err
	}
	run.written = true
	return nil
}

// checkInstallChannels makes sure every listed channel is a channel of the
// community the installer can view (anything else looks like a hidden
// channel), and notes the first kept permission the installer lacks in one,
// for explain. It refuses nothing for that: the bot invariant does.
func checkInstallChannels(ctx context.Context, tx pgx.Tx, snap *perm.Snapshot, actor perm.Subject, plan installPlan, run *installRun) error {
	if plan.all {
		if name := snap.Global(actor).MissingName(plan.perms); name != "" {
			run.lacking = name
		}
	}
	for _, ch := range plan.channels {
		if !snap.KnowsChannel(ch) || snap.IsPrivate(ch) || !snap.Channel(actor, ch).Has(perm.ViewChannel) {
			return perm.ErrChannelHidden
		}
		if name := snap.Channel(actor, ch).MissingName(plan.perms); name != "" && run.lacking == "" {
			if err := tx.QueryRow(ctx, `SELECT name FROM channels WHERE id = $1`, ch).Scan(&run.lackingIn); err != nil {
				return fmt.Errorf("load channel: %w", err)
			}
			run.lacking = name
		}
	}
	return nil
}

// installEntry is the audit entry of an install; it never holds the secret.
func installEntry(id uuid.UUID, plan installPlan) audit.Entry {
	m := plan.manifest
	var perms, intents, commands []string
	for _, p := range m.Permissions {
		if plan.perms.Has(p.Bit) {
			perms = append(perms, p.Key)
		}
	}
	for _, i := range m.Intents {
		if plan.intents&i.Bit != 0 {
			intents = append(intents, i.Name)
		}
	}
	for _, c := range m.Commands {
		commands = append(commands, c.Name)
	}
	return audit.Entry{
		Action: "bot.installed", TargetType: "bot", TargetID: id.String(), TargetName: m.Name,
		Details: map[string]any{
			"homepage": m.Homepage, "permissions": perms, "intents": intents,
			"channel_ids": plan.channels, "commands": commands, "access": accessName(plan.all),
		},
	}
}

func accessName(all bool) string {
	if all {
		return AccessAll
	}
	return AccessChannels
}
