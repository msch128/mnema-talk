package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/httpx"
)

const (
	// challengeTTL is how long the second step stays open after the password.
	challengeTTL = 5 * time.Minute
	// challengeAttempts is how many codes one challenge accepts in total.
	challengeAttempts = 5
	// challengesPerUser bounds the open challenges of one account.
	challengesPerUser = 5
)

// TwoFactorChallenge is the answer to a correct password for an account that
// has two-factor sign-in on. It is not a session: the sign-in is finished by
// POST /api/auth/login/2fa.
type TwoFactorChallenge struct {
	TwoFactorRequired bool `json:"two_factor_required"`
	// Challenge is a single-use token bound to the account and to this
	// sign-in; it is worth nothing without a valid code.
	Challenge string `json:"challenge" minLength:"43" maxLength:"43"`
	// ExpiresIn is the number of seconds the challenge stays open.
	ExpiresIn int `json:"expires_in"`
}

// TwoFactorLoginRequest is the body of POST /api/auth/login/2fa.
type TwoFactorLoginRequest struct {
	Challenge string `json:"challenge" minLength:"43" maxLength:"43"`
	// Code is the six digits of the authenticator app, or an unused recovery
	// code (spaces and dashes are ignored).
	Code string `json:"code" maxLength:"32"`
}

// secondFactorNeeded reports whether the account has two-factor sign-in on.
func (h *Handler) secondFactorNeeded(ctx context.Context, id uuid.UUID) (bool, error) {
	if h.verify != nil {
		// Unit tests that replace the credential check have no database either.
		return false, nil
	}
	st, err := h.Sessions.twoFactorState(ctx, h.Sessions.DB, id)
	return st.Enabled, err
}

// openChallenge stores a challenge for a password-verified sign-in and returns
// its plain token.
func (h *Handler) openChallenge(ctx context.Context, id uuid.UUID, tokenVersion int) (TwoFactorChallenge, error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return TwoFactorChallenge{}, fmt.Errorf("random challenge: %w", err)
	}
	plain := base64.RawURLEncoding.EncodeToString(raw)
	db := h.Sessions.DB
	if _, err := db.Exec(ctx, `DELETE FROM login_challenges WHERE expires_at < NOW()`); err != nil {
		return TwoFactorChallenge{}, err
	}
	// Only the newest few stay open per account.
	if _, err := db.Exec(ctx, `DELETE FROM login_challenges WHERE user_id = $1 AND token_hash IN (
		SELECT token_hash FROM login_challenges WHERE user_id = $1 ORDER BY created_at DESC OFFSET $2)`, id, challengesPerUser-1); err != nil {
		return TwoFactorChallenge{}, err
	}
	_, err := db.Exec(ctx, `INSERT INTO login_challenges (token_hash, user_id, token_version, expires_at)
		VALUES ($1, $2, $3, NOW() + $4 * INTERVAL '1 second')`, hashToken(plain), id, tokenVersion, int64(challengeTTL.Seconds()))
	return TwoFactorChallenge{TwoFactorRequired: true, Challenge: plain, ExpiresIn: int(challengeTTL.Seconds())}, err
}

// claimChallenge counts one attempt on the challenge and returns whom it
// belongs to. Unknown, expired and used-up challenges all yield ErrWrongCode.
func (h *Handler) claimChallenge(ctx context.Context, plain string) (uuid.UUID, int, error) {
	if !wellFormedToken(plain) {
		return uuid.Nil, 0, ErrWrongCode
	}
	var id uuid.UUID
	var tv int
	err := h.Sessions.DB.QueryRow(ctx, `UPDATE login_challenges SET attempts = attempts + 1
		WHERE token_hash = $1 AND expires_at > NOW() AND attempts < $2
		RETURNING user_id, token_version`, hashToken(plain), challengeAttempts).Scan(&id, &tv)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, 0, ErrWrongCode
	}
	return id, tv, err
}

func (h *Handler) closeChallenge(ctx context.Context, plain string) {
	_, _ = h.Sessions.DB.Exec(ctx, `DELETE FROM login_challenges WHERE token_hash = $1`, hashToken(plain))
}

// completeSecondFactor finishes a sign-in: it validates the challenge and the
// code and returns the account with the token version the password check
// captured. A non-zero duration means the account's second-factor lockout is
// on; the caller answers 429 with it. Every other failure is ErrWrongCode.
func (h *Handler) completeSecondFactor(ctx context.Context, challenge, code string) (*User, int, time.Duration, error) {
	id, tv, err := h.claimChallenge(ctx, challenge)
	if err != nil {
		return nil, 0, 0, err
	}
	key := id.String()
	defer h.lockFactor(id)()
	if locked, retry := h.twoFactorFails.IsLockedOut(key); locked {
		return nil, 0, retry, nil
	}
	u, err := h.activeUserAtVersion(ctx, id, tv)
	if err != nil {
		return nil, 0, 0, err
	}
	res, err := h.Sessions.verifyFactor(ctx, h.Sessions.DB, id, code)
	if err != nil {
		return nil, 0, 0, err
	}
	if !res.OK {
		h.twoFactorFails.RecordFailure(key)
		h.auditTwoFactorFailure(ctx, u)
		if res.Unreported == noticeAfter {
			h.raiseAlert(ctx, u)
		}
		return nil, 0, 0, ErrWrongCode
	}
	h.twoFactorFails.ResetFailures(key)
	h.closeChallenge(ctx, challenge)
	h.auditUser(ctx, u, "auth.login")
	if res.Recovery {
		h.auditUser(ctx, u, "auth.recovery_code_used")
	}
	return u, tv, 0, nil
}

// lockFactor serialises second-factor attempts of one account and returns the
// unlock function.
func (h *Handler) lockFactor(id uuid.UUID) func() {
	m := &h.factorLocks[int(id[15])%len(h.factorLocks)]
	m.Lock()
	return m.Unlock
}

// activeUserAtVersion loads the account behind a challenge if it may still
// sign in and its sessions were not revoked since the password check.
func (h *Handler) activeUserAtVersion(ctx context.Context, id uuid.UUID, tv int) (*User, error) {
	var current int
	u, err := scanUser(h.Sessions.DB.QueryRow(ctx, `SELECT `+userColumns+`, token_version FROM users
		WHERE id = $1 AND `+ActiveSQL("")+` AND NOT is_bot`, id), &current)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && current != tv) {
		return nil, ErrWrongCode
	}
	return u, err
}

// login2fa handles POST /api/auth/login/2fa.
//
// @Summary Finish a sign-in with the second factor
// @Description Second step of POST /api/auth/login for an account with two-factor sign-in: the challenge from the 202 answer plus a six-digit authenticator code or an unused recovery code. Sets the session cookie like a normal sign-in. The challenge lives five minutes, is bound to that sign-in, and accepts five codes in total; an authenticator code works once (a replay is refused). After ten wrong codes the account's second step is locked (429, escalating, at most 15 minutes). Wrong, expired, replayed and unknown all answer the same 401; sessions revoked since the password check also fail.
// @ID loginSecondFactor
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body TwoFactorLoginRequest true "Request body."
// @Success 200 {object} TwoFactorLoginResult "Signed in; security_notice is set once when wrong codes were entered since the last sign-in."
// @Header 200 {string} Set-Cookie "Session cookie (HttpOnly, SameSite=Lax, Path=/; Secure on HTTPS)."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "INVALID_CREDENTIALS: the code or the challenge is not valid."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests, or the account's second step is locked."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/auth/login/2fa [post]
func (h *Handler) login2fa(w http.ResponseWriter, r *http.Request) error {
	var req TwoFactorLoginRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	u, tv, retry, err := h.completeSecondFactor(r.Context(), req.Challenge, req.Code)
	if retry > 0 {
		httpx.WriteRateLimited(w, retry)
		return nil
	}
	if err != nil {
		return err
	}
	notice, err := h.Sessions.takeNotice(r.Context(), h.Sessions.DB, u.ID)
	logFailureError(err)
	if err := h.Sessions.Start(w, u, tv); err != nil {
		return err
	}
	h.Sessions.markDevice(w, strings.ToLower(u.Username), u.ID, tv)
	httpx.WriteJSON(w, http.StatusOK, TwoFactorLoginResult{User: h.withRulesPending(r.Context(), u), SecurityNotice: notice})
	return nil
}

// challengeIfNeeded answers a password-verified sign-in of an account with
// two-factor sign-in with a challenge instead of a session.
func (h *Handler) challengeIfNeeded(ctx context.Context, w http.ResponseWriter, u *User, tv int) (bool, error) {
	need, err := h.secondFactorNeeded(ctx, u.ID)
	if err != nil || !need {
		return false, err
	}
	c, err := h.openChallenge(ctx, u.ID, tv)
	if err != nil {
		return false, err
	}
	writeChallenge(w, c)
	return true, nil
}

// writeChallenge answers a password-verified sign-in that needs the code.
func writeChallenge(w http.ResponseWriter, c TwoFactorChallenge) {
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusAccepted, c)
}
