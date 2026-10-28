package auth

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// errBadCode is the answer to a wrong code on a signed-in route. It is a 403,
// not the sign-in's 401: the session itself is fine.
var errBadCode = httpx.ErrForbidden("the code is incorrect")

// TwoFactorSetupRequest is the body of POST /api/users/me/2fa/setup.
type TwoFactorSetupRequest struct {
	CurrentPassword string `json:"current_password" format:"password"`
}

// TwoFactorSetup carries the new authenticator secret.
type TwoFactorSetup struct {
	// Secret is the shared secret in base32, for typing into an app by hand.
	Secret string `json:"secret"`
	// OtpauthURI is the same secret as an otpauth:// URI, for the QR code.
	OtpauthURI string `json:"otpauth_uri"`
}

// TwoFactorEnableRequest is the body of POST /api/users/me/2fa/enable.
type TwoFactorEnableRequest struct {
	// Code is the six digits the app shows for the new secret.
	Code string `json:"code" maxLength:"32"`
}

// RecoveryCodes are shown once.
type RecoveryCodes struct {
	RecoveryCodes []string `json:"recovery_codes"`
}

// TwoFactorProofRequest asks for the password and a current code (or an
// unused recovery code).
type TwoFactorProofRequest struct {
	CurrentPassword string `json:"current_password" format:"password"`
	Code            string `json:"code" maxLength:"32"`
}

func (h *Handler) mountTwoFactor(r chi.Router) {
	r.Post("/users/me/2fa/setup", httpx.Handle(h.twoFactorSetup))
	r.Post("/users/me/2fa/enable", httpx.Handle(h.twoFactorEnable))
	r.Post("/users/me/2fa/disable", httpx.Handle(h.twoFactorDisable))
	r.Post("/users/me/2fa/recovery-codes", httpx.Handle(h.twoFactorRegenerate))
}

// failedCode counts a wrong code against the lockout its shape belongs to, the
// same one the sign-in step uses (see factorLimiter).
func (h *Handler) failedCode(r *http.Request, u *User, code string) error {
	limiter, key := h.factorLimiter(u.ID, httpx.ClientIPKey(r), code)
	limiter.RecordFailure(key)
	return errBadCode
}

// clearCodeFailures forgets the wrong codes counted for the lockout a correct
// code of this shape belongs to.
func (h *Handler) clearCodeFailures(r *http.Request, id uuid.UUID, code string) {
	limiter, key := h.factorLimiter(id, httpx.ClientIPKey(r), code)
	limiter.ResetFailures(key)
}

// lockedOut writes the 429 of a locked second step and reports it.
func (h *Handler) lockedOut(w http.ResponseWriter, r *http.Request, id uuid.UUID, code string) bool {
	limiter, key := h.factorLimiter(id, httpx.ClientIPKey(r), code)
	if locked, retry := limiter.IsLockedOut(key); locked {
		httpx.WriteRateLimited(w, retry)
		return true
	}
	return false
}

// twoFactorSetup handles POST /api/users/me/2fa/setup.
//
// @Summary Start two-factor setup
// @Description Needs the current password. Creates a new pending authenticator secret (an earlier unconfirmed one is replaced) and returns it as text and as otpauth:// URI for the QR code. Nothing changes at sign-in until POST /api/users/me/2fa/enable confirms a code. 409 when two-factor sign-in is already on.
// @ID setupTwoFactor
// @Tags Users
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body TwoFactorSetupRequest true "Request body."
// @Success 200 {object} TwoFactorSetup "Pending secret."
// @Header 200 {string} Cache-Control "no-store"
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: the current password is incorrect, or cross-origin request rejected."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: two-factor sign-in is already on."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/users/me/2fa/setup [post]
func (h *Handler) twoFactorSetup(w http.ResponseWriter, r *http.Request) error {
	u := UserFrom(r.Context())
	var req TwoFactorSetupRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if ok, err := h.reauth(w, r, u.ID, req.CurrentPassword); !ok {
		return err
	}
	secret, err := h.Sessions.beginEnrolment(r.Context(), h.Sessions.DB, u.ID)
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, TwoFactorSetup{Secret: totpEncoding.EncodeToString(secret), OtpauthURI: otpauthURI(secret, u.Username)})
	return nil
}

// twoFactorEnable handles POST /api/users/me/2fa/enable.
//
// @Summary Turn on two-factor sign-in
// @Description Confirms the first code of the pending secret from POST /api/users/me/2fa/setup, turns two-factor sign-in on and returns ten single-use recovery codes, once. From the next sign-in the password is followed by a code. Wrong codes count toward the account's second-factor lockout.
// @ID enableTwoFactor
// @Tags Users
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body TwoFactorEnableRequest true "Request body."
// @Success 200 {object} RecoveryCodes "Recovery codes, shown once."
// @Header 200 {string} Cache-Control "no-store"
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: the code is incorrect, or cross-origin request rejected."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: two-factor sign-in is already on."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests, or the second step is locked."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/users/me/2fa/enable [post]
func (h *Handler) twoFactorEnable(w http.ResponseWriter, r *http.Request) error {
	u := UserFrom(r.Context())
	var req TwoFactorEnableRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	defer h.lockFactor(u.ID)()
	if h.lockedOut(w, r, u.ID, req.Code) {
		return nil
	}
	var codes []string
	err := pgx.BeginFunc(r.Context(), h.Sessions.DB, func(tx pgx.Tx) (err error) {
		codes, err = h.Sessions.confirmEnrolment(r.Context(), tx, u.ID, req.Code)
		return err
	})
	if errors.Is(err, ErrWrongCode) {
		return h.failedCode(r, u, req.Code)
	}
	if err != nil {
		return err
	}
	h.clearCodeFailures(r, u.ID, req.Code)
	h.auditUser(r.Context(), u, "auth.two_factor_enabled")
	writeRecoveryCodes(w, codes)
	return nil
}

func writeRecoveryCodes(w http.ResponseWriter, codes []string) {
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, RecoveryCodes{RecoveryCodes: codes})
}

// requireProof checks the password and a current second-factor code for a
// change that only exists while two-factor sign-in is on. It reports false when
// the response was already written.
func (h *Handler) requireProof(w http.ResponseWriter, r *http.Request, u *User, req TwoFactorProofRequest) (bool, error) {
	return h.identity(w, r, u, req.CurrentPassword, req.Code, true)
}

// ConfirmIdentity re-authenticates the signed-in member before an irreversible
// or revealing action (deleting the account, exporting its data): the
// password, and a current authenticator or recovery code when two-factor
// sign-in is on. Wrong answers count toward the same lockouts as everywhere
// else. It reports false when the response was already written.
func (h *Handler) ConfirmIdentity(w http.ResponseWriter, r *http.Request, u *User, password, code string) (bool, error) {
	return h.identity(w, r, u, password, code, false)
}

func (h *Handler) identity(w http.ResponseWriter, r *http.Request, u *User, password, code string, mustBeOn bool) (bool, error) {
	// The password check can wait a long time for a hashing slot, so it runs
	// before the factor lock is taken: that lock is shared by a stripe of
	// accounts, and only the code check and its failure count need it.
	if h.lockedOut(w, r, u.ID, code) {
		return false, nil
	}
	if ok, err := h.reauth(w, r, u.ID, password); !ok {
		return false, err
	}
	defer h.lockFactor(u.ID)()
	if h.lockedOut(w, r, u.ID, code) {
		return false, nil
	}
	st, err := h.Sessions.twoFactorState(r.Context(), h.Sessions.DB, u.ID)
	if err != nil {
		return false, err
	}
	if !st.Enabled {
		if mustBeOn {
			return false, errTwoFactorOff
		}
		return true, nil
	}
	res, err := h.Sessions.verifyFactor(r.Context(), h.Sessions.DB, u.ID, code)
	if err != nil {
		return false, err
	}
	if !res.OK {
		return false, h.failedCode(r, u, code)
	}
	h.clearCodeFailures(r, u.ID, code)
	return true, nil
}

// twoFactorDisable handles POST /api/users/me/2fa/disable.
//
// @Summary Turn off two-factor sign-in
// @Description Needs the current password and a current authenticator code or an unused recovery code. Deletes the secret and all recovery codes. 409 when two-factor sign-in is off.
// @ID disableTwoFactor
// @Tags Users
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body TwoFactorProofRequest true "Request body."
// @Success 204 "Two-factor sign-in is off."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: the password or the code is incorrect, or cross-origin request rejected."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: two-factor sign-in is not on."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests, or the second step is locked."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/users/me/2fa/disable [post]
func (h *Handler) twoFactorDisable(w http.ResponseWriter, r *http.Request) error {
	u := UserFrom(r.Context())
	var req TwoFactorProofRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if ok, err := h.requireProof(w, r, u, req); !ok {
		return err
	}
	err := pgx.BeginFunc(r.Context(), h.Sessions.DB, func(tx pgx.Tx) error {
		return removeTwoFactor(r.Context(), tx, u.ID)
	})
	if err != nil {
		return err
	}
	h.auditUser(r.Context(), u, "auth.two_factor_disabled")
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// twoFactorRegenerate handles POST /api/users/me/2fa/recovery-codes.
//
// @Summary Make new recovery codes
// @Description Needs the current password and a current authenticator code or an unused recovery code. Replaces all recovery codes with ten new single-use ones, shown once.
// @ID regenerateRecoveryCodes
// @Tags Users
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body TwoFactorProofRequest true "Request body."
// @Success 200 {object} RecoveryCodes "Recovery codes, shown once."
// @Header 200 {string} Cache-Control "no-store"
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: the password or the code is incorrect, or cross-origin request rejected."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: two-factor sign-in is not on."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests, or the second step is locked."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/users/me/2fa/recovery-codes [post]
func (h *Handler) twoFactorRegenerate(w http.ResponseWriter, r *http.Request) error {
	u := UserFrom(r.Context())
	var req TwoFactorProofRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if ok, err := h.requireProof(w, r, u, req); !ok {
		return err
	}
	var codes []string
	err := pgx.BeginFunc(r.Context(), h.Sessions.DB, func(tx pgx.Tx) (err error) {
		codes, err = h.Sessions.replaceRecoveryCodes(r.Context(), tx, u.ID)
		return err
	})
	if err != nil {
		return err
	}
	h.auditUser(r.Context(), u, "auth.recovery_codes_regenerated")
	writeRecoveryCodes(w, codes)
	return nil
}

// ResetTwoFactor turns the second factor of an account off without asking for
// a code and ends all of its sessions, all or nothing. It is for the Owner's
// support action and the host command; callers audit it.
func ResetTwoFactor(ctx context.Context, p *db.Pool, id uuid.UUID) error {
	return pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error { return ResetTwoFactorTx(ctx, tx, id) })
}

// ResetTwoFactorTx is ResetTwoFactor inside the caller's transaction, for a
// caller that has to commit more with it (the host command's audit row).
func ResetTwoFactorTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	if err := removeTwoFactor(ctx, tx, id); err != nil {
		return err
	}
	return RevokeSessions(ctx, tx, id)
}
