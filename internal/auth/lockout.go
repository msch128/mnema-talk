package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// Login lockout state lives in this process's memory. Two ways clear it
// without a restart: the Owner's "Clear login lockout" action (an
// authenticated admin route) and `mnema-talk unlock`, which runs in another
// process and therefore leaves a request row in login_unlock_requests that the
// running server picks up (UnlockWorker). Only someone with database access,
// who could do far worse, can write that table; there is no unauthenticated
// network endpoint.

// ClearLoginLockout forgets every login lockout and failure counter for one
// account: the per-address caps, the account-wide cap, the unknown-device
// trickle, the password-change lockout and the second-step (authenticator and
// recovery code) lockouts.
func (h *Handler) ClearLoginLockout(username string, userID uuid.UUID) {
	account := strings.ToLower(strings.TrimSpace(username))
	suffix := "|" + account
	h.loginFailures.Clear(func(k string) bool { return strings.HasSuffix(k, suffix) })
	h.accountFailures.Clear(func(k string) bool { return k == account })
	h.unknownDevice.Clear(func(k string) bool { return strings.HasSuffix(k, suffix) })
	h.unknownDeviceTotal.Clear(func(k string) bool { return k == account })
	if userID != uuid.Nil {
		id := userID.String()
		h.passwordFails.Clear(func(k string) bool { return k == id })
		h.twoFactorFails.Clear(func(k string) bool { return k == id })
		h.recoveryFails.Clear(func(k string) bool { return strings.HasPrefix(k, id+"|") })
	}
}

// ClearAllLoginLockouts forgets the lockout state of every account and client.
func (h *Handler) ClearAllLoginLockouts() {
	all := func(string) bool { return true }
	h.loginFailures.Clear(all)
	h.accountFailures.Clear(all)
	h.passwordFails.Clear(all)
	h.twoFactorFails.Clear(all)
	h.recoveryFails.Clear(all)
	h.unknownDevice.Clear(all)
	h.unknownDeviceTotal.Clear(all)
	h.loginPerIP.Clear(all)
}

// lockedAccounts returns the (lower-cased) accounts that are locked out now,
// either account-wide or for at least one client address.
func (h *Handler) lockedAccounts() map[string]bool {
	out := map[string]bool{}
	for _, k := range h.accountFailures.LockedKeys() {
		out[k] = true
	}
	for _, k := range h.loginFailures.LockedKeys() {
		if i := strings.LastIndex(k, "|"); i >= 0 {
			out[k[i+1:]] = true
		}
	}
	return out
}

// clearUserLockout handles POST /api/admin/users/{id}/login-lockout/clear.
//
// @Summary Clear an account's login lockout
// @Description Forgets the wrong-password counters and lockouts held in memory for the account (per client address, account-wide and the password-change lockout), so its owner can sign in at once. Owner only (403 otherwise). The same is possible from the host with `mnema-talk unlock <username>`.
// @ID clearUserLoginLockout
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Param id path string true "User ID." Format(uuid)
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: only the Owner may do this, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/users/{id}/login-lockout/clear [post]
func (h *Handler) clearUserLockout(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	var username string
	err = h.Sessions.DB.QueryRow(r.Context(), `SELECT username FROM users WHERE id = $1 AND NOT is_bot`, id).Scan(&username)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.ErrNotFound("user not found")
	}
	if err != nil {
		return err
	}
	h.ClearLoginLockout(username, id)
	slog.Info("login lockout cleared", "user_id", id, "via", "admin")
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// ErrUnlockUnknownUser is returned by RequestUnlock for a username that does not exist.
var ErrUnlockUnknownUser = errors.New("no such user")

// RequestUnlock records, for the running server, a request to clear the
// lockouts of one account (username) or of everybody (all). It is what the
// `mnema-talk unlock` command calls. The returned id identifies the row.
func RequestUnlock(ctx context.Context, q db.Querier, username string, all bool) (int64, error) {
	var arg any
	if !all {
		var canonical string
		err := q.QueryRow(ctx, `SELECT username FROM users WHERE LOWER(username) = LOWER($1) AND NOT is_bot`, strings.TrimSpace(username)).Scan(&canonical)
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrUnlockUnknownUser
		}
		if err != nil {
			return 0, err
		}
		arg = canonical
	}
	var id int64
	err := q.QueryRow(ctx, `INSERT INTO login_unlock_requests (username, all_accounts) VALUES ($1, $2) RETURNING id`, arg, all).Scan(&id)
	return id, err
}

// UnlockRequestPending reports whether the request is still waiting for the server.
func UnlockRequestPending(ctx context.Context, q db.Querier, id int64) (bool, error) {
	var pending bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM login_unlock_requests WHERE id = $1)`, id).Scan(&pending)
	return pending, err
}

type unlockReq struct {
	username *string
	all      bool
}

func readUnlockRequests(ctx context.Context, q db.Querier) ([]unlockReq, error) {
	rows, err := q.Query(ctx, `DELETE FROM login_unlock_requests RETURNING username, all_accounts`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var reqs []unlockReq
	for rows.Next() {
		var r unlockReq
		if err := rows.Scan(&r.username, &r.all); err != nil {
			return nil, err
		}
		reqs = append(reqs, r)
	}
	return reqs, rows.Err()
}

func (h *Handler) applyUnlockRequest(ctx context.Context, r unlockReq) {
	if r.all || r.username == nil {
		h.ClearAllLoginLockouts()
		slog.Info("login lockouts cleared", "scope", "all", "via", "cli")
		return
	}
	var id uuid.UUID
	err := h.Sessions.DB.QueryRow(ctx, `SELECT id FROM users WHERE LOWER(username) = LOWER($1)`, *r.username).Scan(&id)
	if err != nil {
		// The id-keyed limiters stay locked when the lookup fails; the request
		// row is already gone, so say so instead of reporting a full clear.
		id = uuid.Nil
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("unlock request: resolve account", "err", err)
		}
	}
	h.ClearLoginLockout(*r.username, id)
	slog.Info("login lockout cleared", "scope", "account", "via", "cli")
}

// ProcessUnlockRequests applies and deletes every queued unlock request and
// returns how many it handled.
func (h *Handler) ProcessUnlockRequests(ctx context.Context) (int, error) {
	reqs, err := readUnlockRequests(ctx, h.Sessions.DB)
	if err != nil {
		return 0, err
	}
	for _, r := range reqs {
		h.applyUnlockRequest(ctx, r)
	}
	return len(reqs), nil
}

// UnlockPollInterval is how often the server looks for unlock requests.
const UnlockPollInterval = 2 * time.Second

// RunUnlockWorker applies unlock requests until ctx ends. It polls one tiny
// query; failures are logged and retried.
func (h *Handler) RunUnlockWorker(ctx context.Context, interval time.Duration) {
	if h.Sessions == nil || h.Sessions.DB == nil {
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if _, err := h.ProcessUnlockRequests(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("process unlock requests", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
