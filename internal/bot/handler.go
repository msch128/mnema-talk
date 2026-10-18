package bot

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/media"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Handler serves the bot management API under /api/admin/bots.
type Handler struct {
	DB *db.Pool
	// Perms is required: Manage bots and the hierarchy decide everything.
	Perms *perm.Service
	// Media stores bot avatars.
	Media *media.Handler
	// OnRevoke closes live gateway connections: of one token when tokenID is
	// set, of the whole bot otherwise. nil does nothing.
	OnRevoke func(botID uuid.UUID, tokenID *uuid.UUID)
	// Events tells connected members to reload the member list, where bots
	// appear. nil does nothing.
	Events events.Publisher
}

// membersChanged makes every client reload the member list and the roles.
func (h *Handler) membersChanged() {
	if h.Events != nil {
		h.Events.Broadcast("roles_changed", nil)
	}
}

// MountAdmin registers the management routes. Every one needs Manage bots
// (the Owner always holds it).
func (h *Handler) MountAdmin(r chi.Router) {
	h.routes(r.With(auth.RequirePerm(h.Perms, perm.ManageBots)))
}

func (h *Handler) routes(r chi.Router) {
	r.Get("/bots", httpx.Handle(h.list))
	r.Post("/bots", httpx.Handle(h.create))
	r.Get("/bots/{id}", httpx.Handle(h.get))
	r.Patch("/bots/{id}", httpx.Handle(h.update))
	r.Delete("/bots/{id}", httpx.Handle(h.delete))
	r.Post("/bots/{id}/avatar", httpx.Handle(h.avatar))
	r.Put("/bots/{id}/roles", httpx.Handle(h.setRoles))
	r.Get("/bots/{id}/access", httpx.Handle(h.access))
	r.Get("/bots/{id}/tokens", httpx.Handle(h.listTokens))
	r.Post("/bots/{id}/tokens", httpx.Handle(h.createToken))
	r.Post("/bots/{id}/tokens/{tokenID}/regenerate", httpx.Handle(h.regenerateToken))
	r.Delete("/bots/{id}/tokens/{tokenID}", httpx.Handle(h.revokeToken))
	r.Get("/bots/{id}/commands", httpx.Handle(h.listCommands))
	r.Patch("/bots/{id}/commands/{commandID}", httpx.Handle(h.patchCommand))
	r.Delete("/bots/{id}/commands/{commandID}", httpx.Handle(h.deleteCommand))
}

// mountUnguarded registers the routes without the permission guard, for
// tests that reach the handlers' own lookups.
func (h *Handler) mountUnguarded(r chi.Router) {
	h.routes(r)
}

// load finds the bot of the path and checks the caller may manage it: the
// bot's highest role must be below the caller's (the Owner outranks all). It
// also returns the permission state the check used.
func (h *Handler) load(r *http.Request) (*Bot, *perm.Snapshot, error) {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return nil, nil, err
	}
	b, err := Get(r.Context(), h.DB, id)
	if err != nil {
		return nil, nil, err
	}
	snap, err := h.Perms.Snapshot(r.Context())
	if err != nil {
		return nil, nil, err
	}
	b.RoleIDs = snap.RoleIDs(b.UserID)
	if err := snap.CanModerate(auth.UserFrom(r.Context()).Subject(), perm.Subject{ID: b.UserID}); err != nil {
		return nil, nil, err
	}
	return b, snap, nil
}

// loadActing is load for everything that changes the bot or reveals its
// tokens: the caller must also be able to do all that the bot can do (the
// Owner for a bot with administrator power), or the bot would be a way to act
// with rights the caller lacks.
func (h *Handler) loadActing(r *http.Request) (*Bot, error) {
	b, snap, err := h.load(r)
	if err != nil {
		return nil, err
	}
	if err := snap.CanActAs(auth.UserFrom(r.Context()).Subject(), perm.Subject{ID: b.UserID}); err != nil {
		return nil, err
	}
	return b, nil
}

// CreateRequest is the body of creating a bot.
type CreateRequest struct {
	Name string `json:"name" minLength:"1" maxLength:"24"`
	// AllowedIntents defaults to none.
	AllowedIntents int64 `json:"allowed_intents,omitempty" binding:"optional" minimum:"0"`
}

// UpdateRequest changes a bot; absent fields stay.
type UpdateRequest struct {
	Name           *string `json:"name,omitempty" binding:"optional" minLength:"1" maxLength:"24"`
	AllowedIntents *int64  `json:"allowed_intents,omitempty" binding:"optional" minimum:"0"`
	Disabled       *bool   `json:"disabled,omitempty" binding:"optional"`
}

// RolesRequest replaces a bot's roles.
type RolesRequest struct {
	RoleIDs []uuid.UUID `json:"role_ids" format:"uuid"`
}

// TokenRequest is the body of issuing a token.
type TokenRequest struct {
	Name string `json:"name,omitempty" binding:"optional" maxLength:"32"`
	// ExpiresAt is optional; without it the token never expires.
	ExpiresAt *time.Time `json:"expires_at,omitempty" binding:"optional" format:"date-time"`
}

// NewTokenResponse carries a token's secret, shown once.
type NewTokenResponse struct {
	Token
	// Secret is the API token. It is shown only now; only its hash is kept.
	Secret string `json:"secret"`
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

func cleanName(s string) (string, error) { return httpx.CleanText("name", s, MaxNameLen, true) }

func checkIntents(v int64) error {
	if v < 0 || v&^AllIntents != 0 {
		return httpx.ErrInvalidInput("unknown intent bits")
	}
	return nil
}

// list handles GET /api/admin/bots.
//
// @Summary List bots
// @Description Owner or Manage bots. Bots the caller outranks or not alike are all listed; changing one needs a higher role than the bot's.
// @ID listBots
// @Tags Bots
// @Produce json
// @Security cookieAuth
// @Success 200 {array} Bot "Bots, oldest first."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing permission."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/bots [get]
func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	bots, err := List(r.Context(), h.DB)
	if err != nil {
		return err
	}
	roles, err := h.Perms.MemberRoles(r.Context())
	if err != nil {
		return err
	}
	WithRoles(bots, roles)
	httpx.WriteJSON(w, http.StatusOK, bots)
	return nil
}

// create handles POST /api/admin/bots.
//
// @Summary Create a bot
// @Description Owner or Manage bots. The bot starts with no role, no intents and no token: it can do nothing until an admin gives it roles and a token. At most 50 bots.
// @ID createBot
// @Tags Bots
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param body body CreateRequest true "Name and optional intents."
// @Success 201 {object} Bot "Created."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT)."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing permission."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the bot limit is reached."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/bots [post]
func (h *Handler) create(w http.ResponseWriter, r *http.Request) error {
	var req CreateRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	name, err := cleanName(req.Name)
	if err != nil {
		return err
	}
	if err := checkIntents(req.AllowedIntents); err != nil {
		return err
	}
	// Under the permission lock: the new bot starts with the Bot role and its
	// overwrites, which must not be more than its creator holds.
	var id uuid.UUID
	actor := auth.UserFrom(r.Context())
	err = h.Perms.LockedCreatingBot(r.Context(), actor.Subject(), func(tx pgx.Tx, _ *perm.Snapshot) (err error) {
		id, err = createIn(r.Context(), tx, name, req.AllowedIntents, actor.ID)
		return err
	})
	if err != nil {
		return wrapCreate(err)
	}
	b, err := Get(r.Context(), h.DB, id)
	if err != nil {
		return err
	}
	h.Perms.Changed(nil)
	h.membersChanged()
	httpx.WriteJSON(w, http.StatusCreated, b)
	return nil
}

// get handles GET /api/admin/bots/{id}.
//
// @Summary Get a bot
// @ID getBot
// @Tags Bots
// @Produce json
// @Security cookieAuth
// @Param id path string true "Bot ID." Format(uuid)
// @Success 200 {object} Bot "The bot."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing permission, or the bot's highest role is not below yours."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such bot."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/bots/{id} [get]
func (h *Handler) get(w http.ResponseWriter, r *http.Request) error {
	b, _, err := h.load(r)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, b)
	return nil
}

// update handles PATCH /api/admin/bots/{id}.
//
// @Summary Change a bot
// @Description Name (the bot's own managed role is renamed with it), allowed gateway intents, or disabled. Disabling closes its gateway connections at once and refuses all its tokens.
// @ID updateBot
// @Tags Bots
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "Bot ID." Format(uuid)
// @Param body body UpdateRequest true "Fields to change."
// @Success 200 {object} Bot "Updated."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT)."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing permission, or the bot's highest role is not below yours."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such bot."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/bots/{id} [patch]
func (h *Handler) update(w http.ResponseWriter, r *http.Request) error {
	b, err := h.loadActing(r)
	if err != nil {
		return err
	}
	var req UpdateRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if req.Name != nil {
		n, err := cleanName(*req.Name)
		if err != nil {
			return err
		}
		req.Name = &n
	}
	if req.AllowedIntents != nil {
		if err := checkIntents(*req.AllowedIntents); err != nil {
			return err
		}
	}
	if err := Update(r.Context(), h.DB, b.ID, req.Name, req.AllowedIntents, req.Disabled); err != nil {
		return err
	}
	if req.Disabled != nil && *req.Disabled && h.OnRevoke != nil {
		h.OnRevoke(b.ID, nil)
	} else if req.AllowedIntents != nil && h.OnRevoke != nil {
		// A narrower intent set must not outlive the connections that used it.
		h.OnRevoke(b.ID, nil)
	}
	h.Perms.Changed(nil)
	h.membersChanged()
	return h.writeCurrent(w, r, b.ID)
}

func (h *Handler) writeCurrent(w http.ResponseWriter, r *http.Request, id uuid.UUID) error {
	h.membersChanged() // avatar and role changes show in the member list
	cur, err := Get(r.Context(), h.DB, id)
	if err != nil {
		return err
	}
	snap, err := h.Perms.Snapshot(r.Context())
	if err != nil {
		return err
	}
	cur.RoleIDs = snap.RoleIDs(cur.UserID)
	httpx.WriteJSON(w, http.StatusOK, cur)
	return nil
}

// delete handles DELETE /api/admin/bots/{id}.
//
// @Summary Delete a bot
// @Description Its tokens stop working and its gateway connections close at once, and its own managed role is deleted. Messages it posted stay, with its name.
// @ID deleteBot
// @Tags Bots
// @Security cookieAuth
// @Param id path string true "Bot ID." Format(uuid)
// @Success 204 "Deleted."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing permission, or the bot's highest role is not below yours."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such bot."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/bots/{id} [delete]
func (h *Handler) delete(w http.ResponseWriter, r *http.Request) error {
	b, err := h.loadActing(r)
	if err != nil {
		return err
	}
	if err := Delete(r.Context(), h.DB, b.ID); err != nil {
		return err
	}
	if h.OnRevoke != nil {
		h.OnRevoke(b.ID, nil)
	}
	h.Perms.Changed(nil)
	h.membersChanged()
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// avatar handles POST /api/admin/bots/{id}/avatar.
//
// @Summary Set a bot's avatar
// @Description multipart/form-data with an image in field 'avatar' (max 5 MB).
// @ID uploadBotAvatar
// @Tags Bots
// @Accept mpfd
// @Produce json
// @Security cookieAuth
// @Param id path string true "Bot ID." Format(uuid)
// @Param avatar formData file true "Image file, at most 5 MB."
// @Success 200 {object} Bot "Updated."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT)."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing permission, or the bot's highest role is not below yours."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such bot."
// @Failure 413 {object} httpx.ErrorResponse "PAYLOAD_TOO_LARGE: file exceeds the limit."
// @Failure 415 {object} httpx.ErrorResponse "UNSUPPORTED_MEDIA_TYPE: the file type is not allowed."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Failure 503 {object} httpx.ErrorResponse "UNAVAILABLE: file storage is not available."
// @Router /api/admin/bots/{id}/avatar [post]
func (h *Handler) avatar(w http.ResponseWriter, r *http.Request) error {
	b, err := h.loadActing(r)
	if err != nil {
		return err
	}
	if h.Media == nil {
		return httpx.ErrUnavailable("file storage is not configured")
	}
	if err := h.Media.ReplaceAvatar(r, b.UserID); err != nil {
		return err
	}
	return h.writeCurrent(w, r, b.ID)
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
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing permission or authority over a role."
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
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing permission, or the bot's highest role is not below yours."
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
	rows, err := q.Query(ctx, `SELECT id, name FROM channels`)
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

// listTokens handles GET /api/admin/bots/{id}/tokens.
//
// @Summary List a bot's tokens
// @Description Never contains the secrets: name, last characters, state, expiry and last use.
// @ID listBotTokens
// @Tags Bots
// @Produce json
// @Security cookieAuth
// @Param id path string true "Bot ID." Format(uuid)
// @Success 200 {array} Token "Tokens, newest first."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing permission, or the bot's highest role is not below yours."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such bot."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/bots/{id}/tokens [get]
func (h *Handler) listTokens(w http.ResponseWriter, r *http.Request) error {
	b, err := h.loadActing(r)
	if err != nil {
		return err
	}
	ts, err := Tokens(r.Context(), h.DB, b.ID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, ts)
	return nil
}

func cleanTokenRequest(r *http.Request) (string, *time.Time, error) {
	var req TokenRequest
	if r.ContentLength != 0 {
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return "", nil, err
		}
	}
	name, err := httpx.CleanText("name", req.Name, 32, false)
	if err != nil {
		return "", nil, err
	}
	if req.ExpiresAt != nil {
		if err := CheckExpiry(*req.ExpiresAt, time.Now()); err != nil {
			return "", nil, err
		}
	}
	return name, req.ExpiresAt, nil
}

// createToken handles POST /api/admin/bots/{id}/tokens.
//
// @Summary Issue a token
// @Description The secret is in the answer and shown only once; only a SHA-256 hash is stored. Optional expiry. At most 10 active tokens per bot.
// @ID createBotToken
// @Tags Bots
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "Bot ID." Format(uuid)
// @Param body body TokenRequest false "Name and expiry."
// @Success 201 {object} NewTokenResponse "Issued."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT)."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing permission, or the bot's highest role is not below yours."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such bot."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: too many active tokens."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/bots/{id}/tokens [post]
func (h *Handler) createToken(w http.ResponseWriter, r *http.Request) error {
	b, err := h.loadActing(r)
	if err != nil {
		return err
	}
	name, exp, err := cleanTokenRequest(r)
	if err != nil {
		return err
	}
	t, secret, err := IssueToken(r.Context(), h.DB, b.ID, name, exp)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, NewTokenResponse{Token: t, Secret: secret})
	return nil
}

// regenerateToken handles POST /api/admin/bots/{id}/tokens/{tokenID}/regenerate.
//
// @Summary Regenerate a token
// @Description Issues a new token with the same name and expiry and revokes the old one at once (its gateway connections close).
// @ID regenerateBotToken
// @Tags Bots
// @Produce json
// @Security cookieAuth
// @Param id path string true "Bot ID." Format(uuid)
// @Param tokenID path string true "Token ID." Format(uuid)
// @Success 201 {object} NewTokenResponse "Issued; the secret is shown once."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing permission, or the bot's highest role is not below yours."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such bot or token."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: too many active tokens."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/bots/{id}/tokens/{tokenID}/regenerate [post]
func (h *Handler) regenerateToken(w http.ResponseWriter, r *http.Request) error {
	b, err := h.loadActing(r)
	if err != nil {
		return err
	}
	tokenID, err := httpx.PathUUID(r, "tokenID")
	if err != nil {
		return err
	}
	ts, err := Tokens(r.Context(), h.DB, b.ID)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(ts, func(t Token) bool { return t.ID == tokenID })
	if i < 0 {
		return errTokenNotFound
	}
	old := ts[i]
	if err := RevokeToken(r.Context(), h.DB, b.ID, tokenID); err != nil {
		return err
	}
	if h.OnRevoke != nil {
		h.OnRevoke(b.ID, &tokenID)
	}
	t, secret, err := IssueToken(r.Context(), h.DB, b.ID, old.Name, old.ExpiresAt)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, NewTokenResponse{Token: t, Secret: secret})
	return nil
}

// revokeToken handles DELETE /api/admin/bots/{id}/tokens/{tokenID}.
//
// @Summary Revoke a token
// @Description Takes effect at once; open gateway connections that used the token close.
// @ID revokeBotToken
// @Tags Bots
// @Security cookieAuth
// @Param id path string true "Bot ID." Format(uuid)
// @Param tokenID path string true "Token ID." Format(uuid)
// @Success 204 "Revoked."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing permission, or the bot's highest role is not below yours."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such bot or token."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/bots/{id}/tokens/{tokenID} [delete]
func (h *Handler) revokeToken(w http.ResponseWriter, r *http.Request) error {
	b, err := h.loadActing(r)
	if err != nil {
		return err
	}
	tokenID, err := httpx.PathUUID(r, "tokenID")
	if err != nil {
		return err
	}
	if err := RevokeToken(r.Context(), h.DB, b.ID, tokenID); err != nil {
		return err
	}
	if h.OnRevoke != nil {
		h.OnRevoke(b.ID, &tokenID)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
