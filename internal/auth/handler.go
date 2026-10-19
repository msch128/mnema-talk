package auth

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Handler serves /api/auth/* and the profile endpoints under /api/users.
type Handler struct {
	Sessions *Sessions
	Events   events.Publisher
	// Live acts on users' open connections (voice kick, disconnect); optional.
	Live LiveControl
	// OnAccountEnded is told when an account is banned or disabled, after the
	// change is stored: bots hold no session of the web app, so their gateway
	// connections are closed through it. Optional.
	OnAccountEnded func(userID uuid.UUID)
	// Perms decides who may use the admin routes beyond the Owner. nil means
	// only the Owner may.
	Perms *perm.Service
	// Audit records sign-ins, failures, registrations and password changes;
	// nil records nothing.
	Audit       *audit.Log
	failureSeen *sync.Map

	loginPerIP    *httpx.RateLimiter
	registerPerIP *httpx.RateLimiter
	// loginFailures locks one client address out of one account;
	// accountFailures is the much higher cap against guessing from many addresses.
	loginFailures   *httpx.LoginFailureLimiter
	accountFailures *httpx.LoginFailureLimiter
	passwordFails   *httpx.LoginFailureLimiter
	// unknownDevice allows about one password check per minute (burst 2) per
	// ACCOUNT, shared by every device without a known-device cookie, while the
	// account-wide cap is on. The number of addresses does not matter.
	unknownDevice *httpx.RateLimiter
	// generation replaces the database lookup behind the known-device check in unit tests.
	generation func(ctx context.Context, account string) (uuid.UUID, int, error)
	// verify replaces Login in unit tests.
	verify func(ctx context.Context, username, password string) (*User, int, error)
}

// accountAttackWeight is how many failures a wrong password counts against its
// client address while the account-wide lockout is active (10 → 4 guesses).
const accountAttackWeight = 3

func NewHandler(s *Sessions, pub events.Publisher) *Handler {
	h := &Handler{
		Sessions:    s,
		Events:      pub,
		failureSeen: &sync.Map{},
		// Generous per address: a household or office shares one public IP.
		loginPerIP:    httpx.NewRateLimiter(60, 15*time.Minute),
		registerPerIP: httpx.NewRateLimiter(10, time.Hour),
		// 10 failures → 1 min, then 5 min, then 30 min lockouts per address+username
		// (IPv6 per /64); tiers are forgotten after an hour without failures.
		loginFailures:   httpx.NewLoginFailureLimiter(10, time.Minute),
		accountFailures: httpx.NewLoginFailureLimiter(100, time.Minute),
		passwordFails:   httpx.NewLoginFailureLimiter(5, time.Minute),
		unknownDevice:   httpx.NewRateLimiter(2, 2*time.Minute),
	}
	// An attacker cannot keep the owner out for long by tripping the cap again.
	h.accountFailures.CapLockout(accountLockoutMax)
	return h
}

// accountLockoutMax bounds how long the account-wide guess cap stays on.
const accountLockoutMax = 15 * time.Minute

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
// @Description Sets the session cookie and a signed known-device cookie. Rate limited per IP (60 per 15 min, IPv6 per /64) and by escalating lockouts per address+username. Once the account-wide guess cap trips, devices without the known-device cookie share about one password check per minute for the whole account; everything else is answered 429 without a check. The cap lasts at most 15 minutes. Limited requests answer 429 with Retry-After.
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
	u, tv, retry, err := h.verifyLogin(r, req)
	if retry > 0 {
		httpx.WriteRateLimited(w, retry)
		return nil
	}
	if err != nil {
		return err
	}
	if err := h.Sessions.Start(w, u, tv); err != nil {
		return err
	}
	h.Sessions.markDevice(w, strings.ToLower(strings.TrimSpace(req.Username)), u.ID, tv)
	httpx.WriteJSON(w, http.StatusOK, UserEnvelope{User: *u})
	return nil
}

// verifyLogin is the shared browser/native credential and lockout policy.
// Both transports must use this Handler's per-IP route limiter as well. It
// returns the token version captured by password verification; callers must
// never reload and substitute a newer version before issuing a session.
func (h *Handler) verifyLogin(r *http.Request, req LoginRequest) (*User, int, time.Duration, error) {
	username := strings.TrimSpace(req.Username)
	if !usernamePattern.MatchString(username) {
		// No such account can exist. Answer like a wrong password, with the
		// same bcrypt cost, but never let arbitrary input become a limiter key.
		burnPasswordCheck(req.Password)
		return nil, 0, 0, ErrInvalidCredentials
	}
	account := strings.ToLower(username)
	key := httpx.ClientIPKey(r) + "|" + account
	if locked, retry := h.loginFailures.IsLockedOut(key); locked {
		h.auditLoginFailure(r.Context(), account, "auth.login_locked")
		return nil, 0, retry, nil
	}

	// Once the account-wide cap has tripped, passwords are only evaluated for
	// devices that signed in to this account before; anyone else is turned
	// away without a password check, so guessing from many addresses stops
	// being useful while the real owner keeps signing in from their devices.
	if locked, retry := h.accountFailures.IsLockedOut(account); locked && !h.isKnownDevice(r, account) {
		if ok, _ := h.unknownDevice.Allow(account); !ok {
			return nil, 0, retry, nil
		}
	}

	u, tv, err := h.verifyCredentials(r.Context(), username, req.Password)
	if err == ErrInvalidCredentials {
		// Known devices still get a verdict while the cap is on, but each
		// wrong guess counts several times against their address.
		accountLocked, retry := h.accountFailures.IsLockedOut(account)
		weight := 1
		if accountLocked {
			weight = accountAttackWeight
		}
		h.loginFailures.RecordFailures(key, weight)
		h.accountFailures.RecordFailure(account)
		if accountLocked {
			h.auditLoginFailure(r.Context(), account, "auth.login_locked")
			return nil, 0, retry, nil
		}
		h.auditLoginFailure(r.Context(), account, "auth.login_failed")
		return nil, 0, 0, err
	}
	if err != nil {
		return nil, 0, 0, err
	}
	h.loginFailures.ResetFailures(key)
	h.accountFailures.ResetFailures(account)
	h.auditUser(r.Context(), u, "auth.login")
	return u, tv, 0, nil
}

// verifyCredentials checks a login; tests replace it via h.verify.
func (h *Handler) verifyCredentials(ctx context.Context, username, password string) (*User, int, error) {
	if h.verify != nil {
		return h.verify(ctx, username, password)
	}
	return Login(ctx, h.Sessions.DB, username, password)
}

// LoginRequest is the body of POST /api/auth/login.
type LoginRequest struct {
	// Username is case-insensitive.
	Username string `json:"username"`
	Password string `json:"password" format:"password"`
}
