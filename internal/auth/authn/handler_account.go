package authn

import (
	"context"
	"net/http"
	"strings"

	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// register handles POST /api/auth/register.
//
// @Summary Register with an invite code
// @Description Creates a regular user, consumes one use of the invite and signs the user in. Rate limited per IP (10 per hour).
// @ID register
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body auth.RegisterRequest true "Request body."
// @Success 201 {object} auth.UserEnvelope "Account created and signed in."
// @Header 201 {string} Set-Cookie "Session cookie (HttpOnly, SameSite=Lax, Path=/; Secure on HTTPS)."
// @Failure 400 {object} httpx.ErrorResponse "INVALID_INPUT: validation failed, or the invite code is invalid, expired or used up."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: cross-origin request rejected by the CSRF check."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the resource already exists."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Failure 503 {object} httpx.ErrorResponse "UNAVAILABLE: too many password checks are queued; retry after the Retry-After seconds."
// @Header 503 {integer} Retry-After "Seconds until the client may retry."
// @Router /api/auth/register [post]
func (h *Handler) register(w http.ResponseWriter, r *http.Request) error {
	var req auth.RegisterRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	u, err := auth.Register(r.Context(), h.Sessions.DB, strings.TrimSpace(req.Username), req.DisplayName, req.Password, req.InviteCode)
	if err != nil {
		setRetryIfBusy(w, err)
		return err
	}
	if err := h.Sessions.Start(w, u, 0); err != nil {
		return err
	}
	h.announceRegistered(r.Context(), u)
	httpx.WriteJSON(w, http.StatusCreated, auth.UserEnvelope{User: h.withRulesPending(r.Context(), u)})
	return nil
}

// announceRegistered audits a new account and tells the clients about it.
func (h *Handler) announceRegistered(ctx context.Context, u *auth.User) {
	h.AuditUser(ctx, u, "auth.register")
	if h.Events != nil {
		h.Events.Broadcast(auth.MemberJoined(u.Public()))
	}
}

// logout handles POST /api/auth/logout.
//
// @Summary Sign out
// @Description Clears the session cookie and revokes this session, so a copied cookie stops working, and closes the WebSockets opened with it. Needs no session.
// @ID logout
// @Tags Auth
// @Produce json
// @Success 204 "Cookie cleared."
// @Header 204 {string} Set-Cookie "Expired session cookie that clears the session."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: cross-origin request rejected by the CSRF check."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/auth/logout [post]
func (h *Handler) logout(w http.ResponseWriter, r *http.Request) error {
	jti, _ := h.Sessions.SessionClaims(r)
	if err := h.Sessions.Revoke(r); err != nil {
		return err
	}
	// Sockets opened with this session end now, not at the next revalidation.
	if h.Live != nil {
		h.Live.DisconnectSession(jti)
	}
	h.Sessions.End(w)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// logoutAll ends every session of the user, including copied cookies.
//
// @Summary Sign out everywhere
// @Description Invalidates every session of the caller, including copied cookies, and closes their open WebSockets.
// @ID logoutAll
// @Tags Auth
// @Produce json
// @Security cookieAuth
// @Success 204 "All sessions revoked."
// @Header 204 {string} Set-Cookie "Expired session cookie that clears the session."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: cross-origin request rejected by the CSRF check."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/auth/logout-all [post]
func (h *Handler) logoutAll(w http.ResponseWriter, r *http.Request) error {
	u := auth.UserFrom(r.Context())
	if err := auth.RevokeSessions(r.Context(), h.Sessions.DB, u.ID); err != nil {
		return err
	}
	h.AuditUser(r.Context(), u, "auth.logout_all")
	// Open WebSockets were authenticated with the old version; close them.
	if h.Live != nil {
		h.Live.DisconnectUser(u.ID)
	}
	h.Sessions.End(w)
	w.WriteHeader(http.StatusNoContent)
	return nil
}
