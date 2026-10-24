package server

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/mailer"
)

// MailStatus is the outgoing mail setup as the Owner sees it. It carries no
// host, port or credentials.
type MailStatus struct {
	// Configured is true when SMTP_HOST is set.
	Configured bool `json:"configured"`
	// TLS is the transport security in use (starttls, tls or none); empty when not configured.
	TLS string `json:"tls"`
	// From is the sender address of outgoing mail; empty when not configured.
	From string `json:"from"`
}

func (h *systemHandler) mailStatus() MailStatus {
	if !h.mail.Enabled() {
		return MailStatus{}
	}
	return MailStatus{Configured: true, TLS: h.mail.TLS(), From: h.mail.From()}
}

// TestMailRequest is the body of POST /api/admin/system/mail/test.
type TestMailRequest struct {
	// To is the recipient; it defaults to the Owner's own e-mail address.
	To string `json:"to,omitempty" format:"email" maxLength:"254" binding:"optional"`
}

// sendTestMail handles POST /api/admin/system/mail/test.
//
// @Summary Send a test e-mail
// @Description Sends a short message through the configured SMTP server to show that mail works. The recipient defaults to the Owner's own address (set one under the account settings) and can be given explicitly. Owner only (403 otherwise); at most five per ten minutes. The audit entry names no recipient. The failure answer says which stage failed (connect, tls, auth, sender, recipient, data), never a server reply or a credential.
// @ID sendTestMail
// @Tags Admin
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body TestMailRequest false "Request body."
// @Success 204 "Test mail sent."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): no valid recipient, or SMTP is not configured."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: only the Owner may do this, or cross-origin request rejected."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Failure 503 {object} httpx.ErrorResponse "UNAVAILABLE: the mail could not be delivered; the message names the failing stage."
// @Router /api/admin/system/mail/test [post]
func (h *systemHandler) sendTestMail(w http.ResponseWriter, r *http.Request) error {
	if !h.mail.Enabled() {
		return httpx.ErrInvalidInput("SMTP is not configured (SMTP_HOST)")
	}
	owner := auth.UserFrom(r.Context())
	if ok, retry := h.testMailLimit.Allow(owner.ID.String()); !ok {
		httpx.WriteRateLimited(w, retry)
		return nil
	}
	var req TestMailRequest
	if r.ContentLength != 0 {
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}
	}
	to, err := h.testMailRecipient(r, owner, req.To)
	if err != nil {
		return err
	}
	locale := owner.Locale
	if err := h.mail.Send(r.Context(), auth.TestMail(to, locale)); err != nil {
		var se *mailer.SendError
		if errors.As(err, &se) {
			return httpx.ErrUnavailable(se.Error())
		}
		return httpx.ErrUnavailable("mail delivery failed")
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *systemHandler) testMailRecipient(r *http.Request, owner *auth.User, explicit string) (string, error) {
	if explicit != "" {
		return auth.NormalizeEmail(explicit)
	}
	var stored *string
	err := h.db.QueryRow(r.Context(), `SELECT email FROM users WHERE id = $1`, owner.ID).Scan(&stored)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	if stored == nil {
		return "", httpx.ErrInvalidInput("add an e-mail address to your account or name a recipient")
	}
	return *stored, nil
}
