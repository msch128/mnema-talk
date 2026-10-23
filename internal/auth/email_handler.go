package auth

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// AccountInfo is what an account knows about its own sign-in security. It is
// only ever returned to the account itself.
type AccountInfo struct {
	// Email is the caller's own address; omitted when none is set. It is never
	// shown to other members or administrators.
	Email string `json:"email,omitempty" binding:"optional"`
	// EmailVerified is true once the address was confirmed by its link.
	EmailVerified bool `json:"email_verified"`
	// MailAvailable is true when the server can send e-mail (SMTP is
	// configured): addresses can be confirmed and passwords reset by mail.
	MailAvailable bool `json:"mail_available"`
	// TwoFactorEnabled is true while sign-in asks for an authenticator code.
	TwoFactorEnabled bool `json:"two_factor_enabled"`
	// RecoveryCodesRemaining is how many single-use recovery codes are unused
	// (0 when two-factor sign-in is off).
	RecoveryCodesRemaining int `json:"recovery_codes_remaining"`
}

// SetEmailRequest is the body of PUT /api/users/me/email.
type SetEmailRequest struct {
	Email           string `json:"email" format:"email" maxLength:"254"`
	CurrentPassword string `json:"current_password" format:"password"`
}

// PasswordConfirmRequest carries the caller's current password.
type PasswordConfirmRequest struct {
	CurrentPassword string `json:"current_password" format:"password"`
}

// VerifyEmailRequest is the body of POST /api/auth/email/verify.
type VerifyEmailRequest struct {
	// Token is the one from the link in the confirmation mail.
	Token string `json:"token" minLength:"43" maxLength:"43"`
}

// reauth checks the caller's password for a sensitive change, with the
// lockout shared with every other password prompt. It reports false when the
// response was already written (locked out).
func (h *Handler) reauth(w http.ResponseWriter, r *http.Request, id uuid.UUID, password string) (bool, error) {
	retry, err := h.ConfirmPassword(r.Context(), id, password)
	if retry > 0 {
		httpx.WriteRateLimited(w, retry)
		return false, nil
	}
	return err == nil, err
}

func (h *Handler) accountInfo(ctx context.Context, id uuid.UUID) (AccountInfo, error) {
	st, err := loadEmailState(ctx, h.Sessions.DB, id)
	if err != nil {
		return AccountInfo{}, err
	}
	tf, err := h.Sessions.twoFactorState(ctx, h.Sessions.DB, id)
	if err != nil {
		return AccountInfo{}, err
	}
	return AccountInfo{Email: st.Address, EmailVerified: st.Verified, MailAvailable: h.mailEnabled(),
		TwoFactorEnabled: tf.Enabled, RecoveryCodesRemaining: tf.RecoveryCodesRemaining}, nil
}

func (h *Handler) writeAccountInfo(w http.ResponseWriter, r *http.Request, id uuid.UUID) error {
	info, err := h.accountInfo(r.Context(), id)
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, info)
	return nil
}

// getAccount handles GET /api/users/me/account.
//
// @Summary Own sign-in security
// @Description The caller's e-mail address and whether it is confirmed, and whether the server can send mail. Visible to the account itself only.
// @ID getAccountSecurity
// @Tags Users
// @Produce json
// @Security cookieAuth
// @Success 200 {object} AccountInfo "Account security."
// @Header 200 {string} Cache-Control "no-store"
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/users/me/account [get]
func (h *Handler) getAccount(w http.ResponseWriter, r *http.Request) error {
	return h.writeAccountInfo(w, r, UserFrom(r.Context()).ID)
}

// setEmail handles PUT /api/users/me/email.
//
// @Summary Set the account's e-mail address
// @Description Optional; used only to confirm the address and to reset the password. Needs the current password. A new address starts unconfirmed; when the server can send mail, a single-use link (valid 24 hours) is sent to it. Typing the confirmed address again keeps it confirmed. Never shown to other members. Wrong passwords count toward the same lockout as PUT /api/auth/password.
// @ID setEmail
// @Tags Users
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body SetEmailRequest true "Request body."
// @Success 200 {object} AccountInfo "Account security after the change."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: the current password is incorrect, or cross-origin request rejected."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests, or too many confirmation mails for this account in the last hour."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Failure 503 {object} httpx.ErrorResponse "UNAVAILABLE: the confirmation mail could not be sent; the address is saved and the mail can be requested again."
// @Router /api/users/me/email [put]
func (h *Handler) setEmail(w http.ResponseWriter, r *http.Request) error {
	u := UserFrom(r.Context())
	var req SetEmailRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	email, err := NormalizeEmail(req.Email)
	if err != nil {
		return err
	}
	if ok, err := h.reauth(w, r, u.ID, req.CurrentPassword); !ok {
		return err
	}
	verified, err := storeEmail(r.Context(), h.Sessions.DB, u.ID, email)
	if err != nil {
		return err
	}
	h.auditUser(r.Context(), u, "auth.email_set")
	if !verified {
		if err := h.sendVerification(r.Context(), u.ID, requestLanguage(r)); err != nil {
			return err
		}
	}
	return h.writeAccountInfo(w, r, u.ID)
}

// removeEmailHandler handles POST /api/users/me/email/remove.
//
// @Summary Remove the account's e-mail address
// @Description Always possible; needs the current password. Pending confirmation and reset links stop working. Without an address, password reset by e-mail is not available for the account.
// @ID removeEmail
// @Tags Users
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body PasswordConfirmRequest true "Request body."
// @Success 200 {object} AccountInfo "Account security after the change."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: the current password is incorrect, or cross-origin request rejected."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/users/me/email/remove [post]
func (h *Handler) removeEmailHandler(w http.ResponseWriter, r *http.Request) error {
	u := UserFrom(r.Context())
	var req PasswordConfirmRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if ok, err := h.reauth(w, r, u.ID, req.CurrentPassword); !ok {
		return err
	}
	if err := removeEmail(r.Context(), h.Sessions.DB, u.ID); err != nil {
		return err
	}
	h.auditUser(r.Context(), u, "auth.email_remove")
	return h.writeAccountInfo(w, r, u.ID)
}

// resendVerification handles POST /api/users/me/email/resend.
//
// @Summary Send the confirmation mail again
// @Description For an address that is set but not confirmed, when the server can send mail. At most one per minute and five per hour for the account; an earlier link stops working.
// @ID resendEmailVerification
// @Tags Users
// @Produce json
// @Security cookieAuth
// @Success 204 "Mail sent."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): no unconfirmed address, or the server cannot send mail."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Failure 503 {object} httpx.ErrorResponse "UNAVAILABLE: the mail could not be sent."
// @Router /api/users/me/email/resend [post]
func (h *Handler) resendVerification(w http.ResponseWriter, r *http.Request) error {
	u := UserFrom(r.Context())
	if !h.mailEnabled() {
		return httpx.ErrInvalidInput("this server cannot send e-mail")
	}
	st, err := loadEmailState(r.Context(), h.Sessions.DB, u.ID)
	if err != nil {
		return err
	}
	if st.Address == "" || st.Verified {
		return httpx.ErrInvalidInput("there is no unconfirmed e-mail address")
	}
	if ok, retry := h.rec.resendPerUser.Allow(u.ID.String()); !ok {
		httpx.WriteRateLimited(w, retry)
		return nil
	}
	if err := h.sendVerification(r.Context(), u.ID, requestLanguage(r)); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// sendVerification issues a link for the account's unconfirmed address and
// mails it. It does nothing when mail is off or there is nothing to confirm.
func (h *Handler) sendVerification(ctx context.Context, id uuid.UUID, lang string) error {
	if !h.mailEnabled() {
		return nil
	}
	st, err := loadEmailState(ctx, h.Sessions.DB, id)
	if err != nil || st.Address == "" || st.Verified {
		return err
	}
	tooOften, err := issuedTooOften(ctx, h.Sessions.DB, id, purposeEmailVerify)
	if err != nil {
		return err
	}
	// The address gets a few mails an hour whoever asks, so the form cannot be
	// used to flood somebody's mailbox through several accounts.
	if allowed, _ := h.rec.perAddress.Allow(addressKey(st.Address)); tooOften || !allowed {
		return httpx.ErrRateLimited()
	}
	token, err := issueToken(ctx, h.Sessions.DB, id, purposeEmailVerify, st.Address, EmailVerifyTTL)
	if err != nil {
		return err
	}
	if err := h.sendMail(ctx, h.verifyMail(st.Address, mailLocale(st.Locale, lang), st.Username, token)); err != nil {
		return httpx.ErrUnavailable("the confirmation e-mail could not be sent")
	}
	return nil
}

// verifyEmail handles POST /api/auth/email/verify.
//
// @Summary Confirm an e-mail address
// @Description Uses the single-use token from the confirmation mail (no session needed; the link carries it in the URL fragment). The token works once, for 24 hours, and only while the account still has the address it was sent to. Every unusable token answers the same 400.
// @ID verifyEmail
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body VerifyEmailRequest true "Request body."
// @Success 204 "Address confirmed."
// @Failure 400 {object} httpx.ErrorResponse "INVALID_INPUT: the link is invalid or has expired."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: another account has already confirmed this address."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/auth/email/verify [post]
func (h *Handler) verifyEmail(w http.ResponseWriter, r *http.Request) error {
	var req VerifyEmailRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	var id uuid.UUID
	err := pgx.BeginFunc(r.Context(), h.Sessions.DB, func(tx pgx.Tx) (err error) {
		id, err = confirmEmail(r.Context(), tx, req.Token)
		return err
	})
	if err != nil {
		return err
	}
	if u, err := GetUser(r.Context(), h.Sessions.DB, id); err == nil {
		h.auditUser(r.Context(), u, "auth.email_verified")
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
