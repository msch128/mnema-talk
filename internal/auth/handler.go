package auth

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// Handler serves /api/auth/* and the profile endpoints under /api/users.
type Handler struct {
	Sessions *Sessions
	Events   events.Publisher
	// Live acts on users' open connections (voice kick, disconnect); optional.
	Live LiveControl

	loginPerIP    *httpx.RateLimiter
	registerPerIP *httpx.RateLimiter
	// loginFailures locks one client address out of one account;
	// accountFailures is the much higher cap against guessing from many addresses.
	loginFailures   *httpx.LoginFailureLimiter
	accountFailures *httpx.LoginFailureLimiter
	passwordFails   *httpx.LoginFailureLimiter
}

func NewHandler(s *Sessions, pub events.Publisher) *Handler {
	return &Handler{
		Sessions: s,
		Events:   pub,
		// Generous per address: a household or office shares one public IP.
		loginPerIP:    httpx.NewRateLimiter(60, 15*time.Minute),
		registerPerIP: httpx.NewRateLimiter(10, time.Hour),
		// 10 failures → 1 min, then 5 min, then 30 min lockouts per address+username.
		loginFailures:   httpx.NewLoginFailureLimiter(10, time.Minute),
		accountFailures: httpx.NewLoginFailureLimiter(100, time.Minute),
		passwordFails:   httpx.NewLoginFailureLimiter(5, time.Minute),
	}
}

// MountPublic registers the unauthenticated auth routes.
func (h *Handler) MountPublic(r chi.Router) {
	r.With(h.loginPerIP.PerIP).Post("/auth/login", httpx.Handle(h.login))
	r.With(h.registerPerIP.PerIP).Post("/auth/register", httpx.Handle(h.register))
	r.Post("/auth/logout", httpx.Handle(h.logout))
}

// MountAuthenticated registers routes that need a session.
func (h *Handler) MountAuthenticated(r chi.Router) {
	r.Get("/auth/me", httpx.Handle(h.me))
	r.Post("/auth/logout-all", httpx.Handle(h.logoutAll))
	r.Put("/auth/password", httpx.Handle(h.changePassword))
	r.Get("/users/{userID}", httpx.Handle(h.getUser))
	r.Put("/users/me/profile", httpx.Handle(h.updateProfile))
	r.Put("/users/me/locale", httpx.Handle(h.setLocale))
	r.Put("/users/me/presence", httpx.Handle(h.setPresence))
	r.Put("/users/me/status", httpx.Handle(h.setStatus))
}

// login handles POST /api/auth/login.
//
// @Summary Sign in
// @Description Sets the session cookie. Rate limited per IP (60 per 15 min) and by escalating lockouts per address+username and per account; limited requests answer 429 with Retry-After.
// @ID login
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body LoginRequest true "Request body."
// @Success 200 {object} UserEnvelope "Signed in."
// @Header 200 {string} Set-Cookie "Session cookie (HttpOnly, SameSite=Lax, Path=/; Secure on HTTPS)."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "INVALID_CREDENTIALS: unknown user or wrong password."
// @Failure 403 {object} httpx.ErrorResponse "ACCOUNT_DISABLED (correct password, disabled account) or cross-origin request rejected."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/auth/login [post]
func (h *Handler) login(w http.ResponseWriter, r *http.Request) error {
	var req LoginRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	account := strings.ToLower(strings.TrimSpace(req.Username))
	key := httpx.ClientIP(r) + "|" + account
	for _, l := range []struct {
		limiter *httpx.LoginFailureLimiter
		key     string
	}{{h.loginFailures, key}, {h.accountFailures, account}} {
		if locked, retry := l.limiter.IsLockedOut(l.key); locked {
			httpx.WriteRateLimited(w, retry)
			return nil
		}
	}

	u, tv, err := Login(r.Context(), h.Sessions.DB, req.Username, req.Password)
	if err == ErrInvalidCredentials {
		h.loginFailures.RecordFailure(key)
		h.accountFailures.RecordFailure(account)
		return err
	}
	if err != nil {
		return err
	}
	h.loginFailures.ResetFailures(key)
	if err := h.Sessions.Start(w, u, tv); err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, UserEnvelope{User: *u})
	return nil
}

// register handles POST /api/auth/register.
//
// @Summary Register with an invite code
// @Description Creates a regular user, consumes one use of the invite and signs the user in. Rate limited per IP (10 per hour).
// @ID register
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body RegisterRequest true "Request body."
// @Success 201 {object} UserEnvelope "Account created and signed in."
// @Header 201 {string} Set-Cookie "Session cookie (HttpOnly, SameSite=Lax, Path=/; Secure on HTTPS)."
// @Failure 400 {object} httpx.ErrorResponse "INVALID_INPUT: validation failed, or the invite code is invalid, expired or used up."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the resource already exists."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/auth/register [post]
func (h *Handler) register(w http.ResponseWriter, r *http.Request) error {
	var req RegisterRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	u, err := Register(r.Context(), h.Sessions.DB, strings.TrimSpace(req.Username), req.DisplayName, req.Password, req.InviteCode)
	if err != nil {
		return err
	}
	if err := h.Sessions.Start(w, u, 0); err != nil {
		return err
	}
	h.Events.Broadcast("member_joined", u.Public())
	httpx.WriteJSON(w, http.StatusCreated, UserEnvelope{User: *u})
	return nil
}

// logout handles POST /api/auth/logout.
//
// @Summary Sign out
// @Description Clears the session cookie. Needs no session.
// @ID logout
// @Tags Auth
// @Produce json
// @Success 204 "Cookie cleared."
// @Header 204 {string} Set-Cookie "Expired session cookie that clears the session."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/auth/logout [post]
func (h *Handler) logout(w http.ResponseWriter, r *http.Request) error {
	h.Sessions.End(w)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// logoutAll ends every session of the user, including copied cookies.
//
// @Summary Sign out everywhere
// @Description Invalidates every session of the caller, including copied cookies.
// @ID logoutAll
// @Tags Auth
// @Produce json
// @Security cookieAuth
// @Success 204 "All sessions revoked."
// @Header 204 {string} Set-Cookie "Expired session cookie that clears the session."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/auth/logout-all [post]
func (h *Handler) logoutAll(w http.ResponseWriter, r *http.Request) error {
	if err := RevokeSessions(r.Context(), h.Sessions.DB, UserFrom(r.Context()).ID); err != nil {
		return err
	}
	h.Sessions.End(w)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// setLocale handles PUT /api/users/me/locale.
//
// @Summary Set UI language
// @ID setLocale
// @Tags Users
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body SetLocaleRequest true "Request body."
// @Success 200 {object} User "Updated user."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/users/me/locale [put]
func (h *Handler) setLocale(w http.ResponseWriter, r *http.Request) error {
	var req SetLocaleRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	u, err := SetLocale(r.Context(), h.Sessions.DB, UserFrom(r.Context()).ID, req.Locale)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, u)
	return nil
}

// me handles GET /api/auth/me.
//
// @Summary Current user
// @ID getCurrentUser
// @Tags Auth
// @Produce json
// @Security cookieAuth
// @Success 200 {object} User "The caller, including chosen presence."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/auth/me [get]
func (h *Handler) me(w http.ResponseWriter, r *http.Request) error {
	httpx.WriteJSON(w, http.StatusOK, UserFrom(r.Context()))
	return nil
}

// changePassword handles PUT /api/auth/password.
//
// @Summary Change own password
// @Description Signs out all other sessions and keeps the current one (a fresh cookie is set). Five wrong current passwords trigger a lockout (429).
// @ID changePassword
// @Tags Auth
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body ChangePasswordRequest true "Request body."
// @Success 204 "Password changed."
// @Header 204 {string} Set-Cookie "Session cookie (HttpOnly, SameSite=Lax, Path=/; Secure on HTTPS)."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: current password is incorrect, or cross-origin request rejected."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/auth/password [put]
func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request) error {
	u := UserFrom(r.Context())
	var req ChangePasswordRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	key := u.ID.String()
	if locked, retry := h.passwordFails.IsLockedOut(key); locked {
		httpx.WriteRateLimited(w, retry)
		return nil
	}
	tv, err := ChangePassword(r.Context(), h.Sessions.DB, u.ID, req.CurrentPassword, req.NewPassword)
	if err == ErrWrongPassword {
		h.passwordFails.RecordFailure(key)
		return err
	}
	if err != nil {
		return err
	}
	h.passwordFails.ResetFailures(key)
	// All other sessions are revoked by the version bump; keep this one alive.
	if err := h.Sessions.Start(w, u, tv); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// getUser handles GET /api/users/{userID}.
//
// @Summary Get a user profile
// @Description Includes voice_seconds and message_count. Other users are returned without the private presence choice.
// @ID getUser
// @Tags Users
// @Produce json
// @Security cookieAuth
// @Param userID path string true "User ID." Format(uuid)
// @Success 200 {object} User "The user."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/users/{userID} [get]
func (h *Handler) getUser(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "userID")
	if err != nil {
		return err
	}
	u, err := GetUser(r.Context(), h.Sessions.DB, id)
	if err != nil {
		return err
	}
	if err := LoadStats(r.Context(), h.Sessions.DB, u); err != nil {
		return err
	}
	if u.ID != UserFrom(r.Context()).ID {
		pub := u.Public()
		u = &pub
	}
	httpx.WriteJSON(w, http.StatusOK, u)
	return nil
}

// updateProfile handles PUT /api/users/me/profile.
//
// @Summary Update display name and bio
// @Description Broadcasts user_update.
// @ID updateProfile
// @Tags Users
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body UpdateProfileRequest true "Request body."
// @Success 200 {object} User "Updated user."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/users/me/profile [put]
func (h *Handler) updateProfile(w http.ResponseWriter, r *http.Request) error {
	var req UpdateProfileRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	u, err := UpdateProfile(r.Context(), h.Sessions.DB, UserFrom(r.Context()).ID, req.DisplayName, req.Bio)
	if err != nil {
		return err
	}
	h.Events.Broadcast("user_update", u.Public())
	httpx.WriteJSON(w, http.StatusOK, u)
	return nil
}

// setPresence handles PUT /api/users/me/presence.
//
// @Summary Choose presence
// @Description Applies to the user's live WebSocket status.
// @ID setPresence
// @Tags Users
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body SetPresenceRequest true "Request body."
// @Success 200 {object} User "Updated user."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/users/me/presence [put]
func (h *Handler) setPresence(w http.ResponseWriter, r *http.Request) error {
	var req SetPresenceRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	u, err := SetPresence(r.Context(), h.Sessions.DB, UserFrom(r.Context()).ID, req.Presence)
	if err != nil {
		return err
	}
	if h.Live != nil {
		h.Live.SetPresence(u.ID, u.Presence)
	}
	httpx.WriteJSON(w, http.StatusOK, u)
	return nil
}

// setStatus changes the caller's own status line. Nobody else can, except an
// admin (see setUserStatus).
//
// @Summary Set own status line
// @Description Broadcasts user_update.
// @ID setStatus
// @Tags Users
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body SetStatusRequest true "Request body."
// @Success 200 {object} User "Updated user."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/users/me/status [put]
func (h *Handler) setStatus(w http.ResponseWriter, r *http.Request) error {
	return h.writeStatus(w, r, UserFrom(r.Context()).ID)
}

func (h *Handler) writeStatus(w http.ResponseWriter, r *http.Request, userID uuid.UUID) error {
	var req SetStatusRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	u, err := SetStatusText(r.Context(), h.Sessions.DB, userID, req.StatusText)
	if err != nil {
		return err
	}
	pub := u.Public()
	h.Events.Broadcast("user_update", pub)
	if userID != UserFrom(r.Context()).ID {
		u = &pub
	}
	httpx.WriteJSON(w, http.StatusOK, u)
	return nil
}

// LoginRequest is the body of POST /api/auth/login.
type LoginRequest struct {
	// Username is case-insensitive.
	Username string `json:"username"`
	Password string `json:"password" format:"password"`
}

// RegisterRequest is the body of POST /api/auth/register.
type RegisterRequest struct {
	// Username must be unique; "all" and "here" are reserved.
	Username string `json:"username" pattern:"^[A-Za-z0-9_.-]{3,32}$"`
	// DisplayName defaults to the username.
	DisplayName string `json:"display_name" maxLength:"24" binding:"optional"`
	// Password needs 10 characters at least and 72 bytes at most.
	Password   string `json:"password" format:"password" minLength:"10"`
	InviteCode string `json:"invite_code" maxLength:"64"`
}

// UserEnvelope wraps the signed-in user in login and register responses.
type UserEnvelope struct {
	User User `json:"user"`
}

// ChangePasswordRequest is the body of PUT /api/auth/password.
type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password" format:"password"`
	NewPassword     string `json:"new_password" format:"password" minLength:"10"`
}

// UpdateProfileRequest is the body of PUT /api/users/me/profile.
type UpdateProfileRequest struct {
	// DisplayName: empty resets it to the username.
	DisplayName string `json:"display_name" maxLength:"24" binding:"optional"`
	Bio         string `json:"bio" maxLength:"250" binding:"optional"`
}

// SetLocaleRequest is the body of PUT /api/users/me/locale.
type SetLocaleRequest struct {
	Locale string `json:"locale" enums:"de,en"`
}

// SetPresenceRequest is the body of PUT /api/users/me/presence.
type SetPresenceRequest struct {
	// Presence is the chosen presence; "offline" is never chosen, it follows
	// from having no open WebSocket.
	Presence string `json:"presence" enums:"online,away,dnd,focus"`
}

// SetStatusRequest is the body of the status-line endpoints.
type SetStatusRequest struct {
	// StatusText: empty clears the status.
	StatusText string `json:"status_text" maxLength:"32"`
}
