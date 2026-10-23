package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// AuthOptions tells the sign-in screen which recovery paths exist.
type AuthOptions struct {
	// PasswordReset is true when members can reset a forgotten password by
	// e-mail (the server can send mail). It does not say who has an address.
	PasswordReset bool `json:"password_reset"`
}

// PasswordResetRequest is the body of POST /api/auth/password-reset/request.
type PasswordResetRequest struct {
	Email string `json:"email" format:"email" maxLength:"254"`
}

// PasswordResetConfirmation is the body of POST /api/auth/password-reset/confirm.
type PasswordResetConfirmation struct {
	// Token is the one from the link in the reset mail.
	Token       string `json:"token" minLength:"43" maxLength:"43"`
	NewPassword string `json:"new_password" format:"password" minLength:"10"`
}

// addressKey identifies an address for rate limiting without keeping it.
func addressKey(email string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(email)))
	return hex.EncodeToString(sum[:8])
}

// authOptions handles GET /api/auth/options.
//
// @Summary Sign-in options
// @Description Which recovery paths the server offers, so the sign-in screen can show them. Carries nothing about any account.
// @ID getAuthOptions
// @Tags Auth
// @Produce json
// @Success 200 {object} AuthOptions "Options."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Router /api/auth/options [get]
func (h *Handler) authOptions(w http.ResponseWriter, _ *http.Request) error {
	httpx.WriteJSON(w, http.StatusOK, AuthOptions{PasswordReset: h.mailEnabled()})
	return nil
}

// requestPasswordReset handles POST /api/auth/password-reset/request.
//
// @Summary Ask for a password reset mail
// @Description Answers 202 with an empty body whether or not the address belongs to an account, has been confirmed, or the account is disabled, and takes the same time either way: lookup and mail run after the response. Only an active account with a confirmed address gets a single-use link (valid 30 minutes). Rate limited per client address (10 per hour, IPv6 per /64) and, silently, per address and per account (a few mails per hour); 404 when the server cannot send mail.
// @ID requestPasswordReset
// @Tags Auth
// @Accept json
// @Param request body PasswordResetRequest true "Request body."
// @Success 202 "Accepted; if the address is confirmed for an account, a mail is on its way."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON or not an e-mail address."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the server cannot send e-mail."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/auth/password-reset/request [post]
func (h *Handler) requestPasswordReset(w http.ResponseWriter, r *http.Request) error {
	if !h.mailEnabled() {
		return httpx.ErrNotFound("password reset by e-mail is not available")
	}
	var req PasswordResetRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	email, err := NormalizeEmail(req.Email)
	if err != nil {
		return err
	}
	if allowed, _ := h.rec.perAddress.Allow(addressKey(email)); allowed {
		lang := requestLanguage(r)
		h.background(func(ctx context.Context) { h.sendReset(ctx, email, lang) })
	}
	w.WriteHeader(http.StatusAccepted)
	return nil
}

// sendReset looks the address up and, only for an active account with that
// confirmed address that has not used up its hourly budget, mails a link.
// Every other case ends silently.
func (h *Handler) sendReset(ctx context.Context, email, lang string) {
	id, username, locale, stored, err := userByVerifiedEmail(ctx, h.Sessions.DB, email)
	if err != nil {
		if !notFound(err) {
			slog.Warn("password reset lookup failed", "err", err)
		}
		return
	}
	tooOften, err := issuedTooOften(ctx, h.Sessions.DB, id, purposePasswordReset)
	if err != nil || tooOften {
		return
	}
	token, err := issueToken(ctx, h.Sessions.DB, id, purposePasswordReset, "", PasswordResetTTL)
	if err != nil {
		slog.Warn("password reset token not stored", "err", err)
		return
	}
	// A failed send leaves the token; it expires unused.
	_ = h.sendMail(ctx, h.resetMail(stored, mailLocale(locale, lang), username, token))
}

// confirmPasswordReset handles POST /api/auth/password-reset/confirm.
//
// @Summary Set a new password with a reset link
// @Description Uses the single-use token from the reset mail. The token works once and for 30 minutes. A new password follows the usual policy (the Owner needs 14 characters). Success ends every session of the account, including other devices; it does not sign anyone in, and a second factor, if enabled, is still asked at sign-in. Every unusable token answers the same 400.
// @ID confirmPasswordReset
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body PasswordResetConfirmation true "Request body."
// @Success 204 "Password changed."
// @Failure 400 {object} httpx.ErrorResponse "INVALID_INPUT: the link is invalid or has expired, or the password does not meet the policy."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Failure 503 {object} httpx.ErrorResponse "UNAVAILABLE: too many password checks are queued; retry after the Retry-After seconds."
// @Header 503 {integer} Retry-After "Seconds until the client may retry."
// @Router /api/auth/password-reset/confirm [post]
func (h *Handler) confirmPasswordReset(w http.ResponseWriter, r *http.Request) error {
	var req PasswordResetConfirmation
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if !wellFormedToken(req.Token) {
		return errInvalidLink
	}
	id, err := h.applyReset(r.Context(), req)
	if err != nil {
		setRetryIfBusy(w, err)
		return err
	}
	u, err := GetUser(r.Context(), h.Sessions.DB, id)
	if err != nil {
		return err
	}
	h.ClearLoginLockout(u.Username, u.ID)
	h.auditUser(r.Context(), u, "auth.password_reset_mail")
	if h.Live != nil {
		h.Live.DisconnectUser(id)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// applyReset spends the token and stores the new password in one transaction,
// so a rejected password or a failure in between leaves the link usable and a
// success leaves it spent. Hashing happens only for a live token of an
// active account.
func (h *Handler) applyReset(ctx context.Context, req PasswordResetConfirmation) (uuid.UUID, error) {
	var id uuid.UUID
	err := pgx.BeginFunc(ctx, h.Sessions.DB, func(tx pgx.Tx) error {
		claim, err := consumeToken(ctx, tx, req.Token, purposePasswordReset)
		if err != nil {
			return err
		}
		var role string
		err = tx.QueryRow(ctx, `SELECT role FROM users WHERE id = $1 AND NOT is_bot AND `+ActiveSQL("")+` FOR UPDATE`, claim.UserID).Scan(&role)
		if notFound(err) {
			return errInvalidLink
		}
		if err != nil {
			return err
		}
		if err := ValidatePasswordFor(role, req.NewPassword); err != nil {
			return err
		}
		hash, err := hashPasswordCtx(ctx, req.NewPassword)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE users SET password_hash = $1, token_version = token_version + 1, updated_at = NOW()
			WHERE id = $2`, hash, claim.UserID); err != nil {
			return err
		}
		id = claim.UserID
		return dropTokens(ctx, tx, claim.UserID, purposePasswordReset)
	})
	return id, err
}
