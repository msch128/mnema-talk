package authn

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
	"github.com/msch128/mnema-talk/internal/auth"
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

// secondFactorNeeded reports whether the account has two-factor sign-in on.
func (h *Handler) secondFactorNeeded(ctx context.Context, id uuid.UUID) (bool, error) {
	if h.twoFactor != nil {
		return h.twoFactor(ctx, id)
	}
	st, err := h.Sessions.TwoFactorState(ctx, h.Sessions.DB, id)
	return st.Enabled, err
}

// openChallenge stores a challenge for a password-verified sign-in and returns
// its plain token.
func (h *Handler) openChallenge(ctx context.Context, id uuid.UUID, tokenVersion int) (auth.TwoFactorChallenge, error) {
	raw := make([]byte, auth.TokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return auth.TwoFactorChallenge{}, fmt.Errorf("random challenge: %w", err)
	}
	plain := base64.RawURLEncoding.EncodeToString(raw)
	db := h.Sessions.DB
	if _, err := db.Exec(ctx, `DELETE FROM login_challenges WHERE expires_at < NOW()`); err != nil {
		return auth.TwoFactorChallenge{}, err
	}
	// Only the newest few stay open per account.
	if _, err := db.Exec(ctx, `DELETE FROM login_challenges WHERE user_id = $1 AND token_hash IN (
		SELECT token_hash FROM login_challenges WHERE user_id = $1 ORDER BY created_at DESC OFFSET $2)`, id, challengesPerUser-1); err != nil {
		return auth.TwoFactorChallenge{}, err
	}
	_, err := db.Exec(ctx, `INSERT INTO login_challenges (token_hash, user_id, token_version, expires_at)
		VALUES ($1, $2, $3, NOW() + $4 * INTERVAL '1 second')`, auth.HashToken(plain), id, tokenVersion, int64(challengeTTL.Seconds()))
	return auth.TwoFactorChallenge{TwoFactorRequired: true, Challenge: plain, ExpiresIn: int(challengeTTL.Seconds())}, err
}

// claimChallenge counts one attempt on the challenge (the caller releases it
// again when the lockout turns the request away) and returns whom it
// belongs to. Unknown, expired and used-up challenges all yield ErrWrongCode.
func (h *Handler) claimChallenge(ctx context.Context, plain string) (uuid.UUID, int, error) {
	if !auth.WellFormedToken(plain) {
		return uuid.Nil, 0, auth.ErrWrongCode
	}
	var id uuid.UUID
	var tv int
	err := h.Sessions.DB.QueryRow(ctx, `UPDATE login_challenges SET attempts = attempts + 1
		WHERE token_hash = $1 AND expires_at > NOW() AND attempts < $2
		RETURNING user_id, token_version`, auth.HashToken(plain), challengeAttempts).Scan(&id, &tv)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, 0, auth.ErrWrongCode
	}
	return id, tv, err
}

// releaseAttempt gives back the attempt claimChallenge counted for a request
// the lockout turned away, so waiting out a lock does not use up the challenge.
func (h *Handler) releaseAttempt(ctx context.Context, plain string) {
	_, _ = h.Sessions.DB.Exec(ctx, `UPDATE login_challenges SET attempts = attempts - 1 WHERE token_hash = $1 AND attempts > 0`, auth.HashToken(plain))
}

func (h *Handler) closeChallenge(ctx context.Context, plain string) {
	_, _ = h.Sessions.DB.Exec(ctx, `DELETE FROM login_challenges WHERE token_hash = $1`, auth.HashToken(plain))
}

// completeSecondFactor finishes a sign-in: it validates the challenge and the
// code and returns the account with the token version the password check
// captured. A non-zero duration means the lockout for that kind of code is on;
// the caller answers 429 with it. Every other failure is ErrWrongCode.
// client identifies the caller (httpx.ClientIPKey) for the recovery-code limit.
func (h *Handler) completeSecondFactor(ctx context.Context, client, challenge, code string) (*auth.User, int, time.Duration, error) {
	id, tv, err := h.claimChallenge(ctx, challenge)
	if err != nil {
		return nil, 0, 0, err
	}
	limiter, key := h.factorLimiter(id, client, code)
	defer h.lockFactor(id)()
	if locked, retry := limiter.IsLockedOut(key); locked {
		h.releaseAttempt(ctx, challenge) // turned away before it counts
		return nil, 0, retry, nil
	}
	u, err := h.activeUserAtVersion(ctx, id, tv)
	if err != nil {
		return nil, 0, 0, err
	}
	res, err := h.Sessions.VerifyFactor(ctx, h.Sessions.DB, id, code)
	if err != nil {
		return nil, 0, 0, err
	}
	if !res.OK {
		limiter.RecordFailure(key)
		h.auditTwoFactorFailure(ctx, u)
		if res.Unreported == auth.NoticeAfter {
			h.raiseAlert(ctx, u)
		}
		return nil, 0, 0, auth.ErrWrongCode
	}
	limiter.ResetFailures(key)
	h.closeChallenge(ctx, challenge)
	h.AuditUser(ctx, u, "auth.login")
	if res.Recovery {
		h.AuditUser(ctx, u, "auth.recovery_code_used")
	}
	return u, tv, 0, nil
}

// factorLimiter picks the in-memory lockout a code counts against. Wrong
// authenticator codes lock per account, so guessing six digits stays slow
// however many addresses ask. Recovery codes have their own, tighter limiter
// per account and client: they cannot be guessed, so a lockout there only
// bounds the work, and nobody who knows just the password can use the
// authenticator lockout to keep the real owner from their recovery codes.
func (h *Handler) factorLimiter(id uuid.UUID, client, code string) (*httpx.LoginFailureLimiter, string) {
	if auth.IsTOTPShape(auth.CleanCode(code)) {
		return h.twoFactorFails, id.String()
	}
	return h.recoveryFails, id.String() + "|" + client
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
func (h *Handler) activeUserAtVersion(ctx context.Context, id uuid.UUID, tv int) (*auth.User, error) {
	var current int
	u, err := auth.ScanUser(h.Sessions.DB.QueryRow(ctx, `SELECT `+auth.UserColumns+`, token_version FROM users
		WHERE id = $1 AND `+auth.ActiveSQL("")+` AND NOT is_bot`, id), &current)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && current != tv) {
		return nil, auth.ErrWrongCode
	}
	return u, err
}

// login2fa handles POST /api/auth/login/2fa.
//
// @Summary Finish a sign-in with the second factor
// @Description Second step of POST /api/auth/login for an account with two-factor sign-in: the challenge from the 202 answer plus a six-digit authenticator code or an unused recovery code. Sets the session cookie like a normal sign-in. The challenge lives five minutes, is bound to that sign-in, and accepts five codes in total; an authenticator code works once (a replay is refused). After ten wrong authenticator codes the authenticator step of the account is locked (429, escalating, at most 15 minutes); recovery codes are limited separately, per client, so that lockout never blocks them. Wrong, expired, replayed and unknown all answer the same 401; sessions revoked since the password check also fail.
// @ID loginSecondFactor
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body auth.TwoFactorLoginRequest true "Request body."
// @Success 200 {object} auth.TwoFactorLoginResult "Signed in; security_notice is set once when wrong codes were entered since the last sign-in."
// @Header 200 {string} Set-Cookie "Session cookie (HttpOnly, SameSite=Lax, Path=/; Secure on HTTPS)."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "INVALID_CREDENTIALS: the code or the challenge is not valid."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests, or the account's second step is locked."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/auth/login/2fa [post]
func (h *Handler) login2fa(w http.ResponseWriter, r *http.Request) error {
	var req auth.TwoFactorLoginRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	u, tv, retry, err := h.completeSecondFactor(r.Context(), httpx.ClientIPKey(r), req.Challenge, req.Code)
	if retry > 0 {
		httpx.WriteRateLimited(w, retry)
		return nil
	}
	if err != nil {
		return err
	}
	notice, err := h.Sessions.TakeNotice(r.Context(), h.Sessions.DB, u.ID)
	auth.LogFailureError(err)
	if err := h.Sessions.Start(w, u, tv); err != nil {
		return err
	}
	h.Sessions.MarkDevice(w, strings.ToLower(u.Username), u.ID, tv)
	httpx.WriteJSON(w, http.StatusOK, auth.TwoFactorLoginResult{User: h.withRulesPending(r.Context(), u), SecurityNotice: notice})
	return nil
}

// challengeIfNeeded answers a password-verified sign-in of an account with
// two-factor sign-in with a challenge instead of a session.
func (h *Handler) challengeIfNeeded(ctx context.Context, w http.ResponseWriter, u *auth.User, tv int) (bool, error) {
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
func writeChallenge(w http.ResponseWriter, c auth.TwoFactorChallenge) {
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusAccepted, c)
}
