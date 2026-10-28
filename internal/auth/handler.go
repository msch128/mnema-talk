package auth

import (
	"context"
	"errors"
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
	// Mail delivers account e-mails (address confirmation, password reset);
	// nil when SMTP is not configured, which switches those flows off.
	Mail MailSender
	// Objects is the object store; nil disables data exports and the removal
	// of a deleted account's files.
	Objects ObjectStore
	// ObjectMissing tells a "no such object" error of Objects from a real
	// failure; an export skips only the files that are gone. nil treats every
	// error as a failure.
	ObjectMissing func(error) bool
	// Lifetime bounds the background jobs (data exports): they stop when it
	// ends. nil means they run to completion.
	Lifetime    context.Context
	exportSlots chan struct{}
	// PublicURL is the address the links in those mails point to.
	PublicURL string
	rec       *recovery

	loginPerIP    *httpx.RateLimiter
	registerPerIP *httpx.RateLimiter
	// registerPerSite caps a whole IPv6 /48 (or one IPv4 address) together.
	registerPerSite *httpx.RateLimiter
	// loginFailures locks one client address out of one account;
	// accountFailures is the much higher cap against guessing from many addresses.
	loginFailures   *httpx.LoginFailureLimiter
	accountFailures *httpx.LoginFailureLimiter
	passwordFails   *httpx.LoginFailureLimiter
	// twoFactorFails locks the second sign-in step of one account after ten
	// wrong codes (escalating, at most accountLockoutMax).
	twoFactorFails *httpx.LoginFailureLimiter
	// recoveryFails is the separate, tighter limiter for recovery codes at
	// sign-in, keyed by account and client, so the lockout above never blocks them.
	recoveryFails *httpx.LoginFailureLimiter
	// factorLocks serialise the second-factor attempts of one account (striped
	// by account id), so the lockout check, the verification and the failure
	// count are one step and parallel requests cannot all pass the check
	// before the first failure is recorded.
	factorLocks *[64]sync.Mutex
	// unknownDevice allows about one password check per minute (burst 2) per
	// client address and account to devices without a known-device cookie,
	// while the account-wide cap is on, so one hostile client cannot use up
	// the checks of a legitimate one. unknownDeviceTotal bounds all such
	// checks for the account together; only granted checks are charged to it.
	unknownDevice      *httpx.RateLimiter
	unknownDeviceTotal *httpx.RateLimiter
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
		rec:         newRecovery(),
		exportSlots: make(chan struct{}, exportSlots),
		factorLocks: new([64]sync.Mutex),
		// Generous per address: a household or office shares one public IP.
		loginPerIP:      httpx.NewRateLimiter(60, 15*time.Minute),
		registerPerIP:   httpx.NewRateLimiter(10, time.Hour),
		registerPerSite: httpx.NewRateLimiter(40, time.Hour),
		// 10 failures → 1 min, then 5 min, then 30 min lockouts per address+username
		// (IPv6 per /64); tiers are forgotten after an hour without failures.
		loginFailures:   httpx.NewLoginFailureLimiter(10, time.Minute),
		accountFailures: httpx.NewLoginFailureLimiter(100, time.Minute),
		passwordFails:   httpx.NewLoginFailureLimiter(5, time.Minute),
		twoFactorFails:  httpx.NewLoginFailureLimiter(10, time.Minute),
		recoveryFails:   httpx.NewLoginFailureLimiter(5, time.Minute),
		unknownDevice:   httpx.NewRateLimiter(2, 2*time.Minute),
		// About 10 checks per minute for the whole account, however many
		// addresses ask: guessing stays slow, and a sustained attack needs
		// that many distinct clients to crowd out the owner.
		unknownDeviceTotal: httpx.NewRateLimiter(20, 2*time.Minute),
	}
	// An attacker cannot keep the owner out for long by tripping the cap again.
	h.accountFailures.CapLockout(accountLockoutMax)
	h.twoFactorFails.CapLockout(accountLockoutMax)
	h.recoveryFails.CapLockout(accountLockoutMax)
	return h
}

// accountLockoutMax bounds how long the account-wide guess cap stays on.
const accountLockoutMax = 15 * time.Minute

// MountPublic registers the unauthenticated auth routes.
func (h *Handler) MountPublic(r chi.Router) {
	r.With(h.loginPerIP.PerIP).Post("/auth/login", httpx.Handle(h.login))
	r.With(h.registerPerSite.By(httpx.ClientAggregateKey), h.registerPerIP.PerIP).Post("/auth/register", httpx.Handle(h.register))
	r.Post("/auth/logout", httpx.Handle(h.logout))
	r.With(h.loginPerIP.PerIP).Post("/auth/login/2fa", httpx.Handle(h.login2fa))
	h.mountRecovery(r)
}

// MountAuthenticated registers routes that need a session.
func (h *Handler) MountAuthenticated(r chi.Router) {
	r.Get("/auth/me", httpx.Handle(h.me))
	r.Post("/auth/logout-all", httpx.Handle(h.logoutAll))
	r.Get("/auth/sessions", httpx.Handle(h.listSessions))
	r.Post("/auth/sessions/revoke-others", httpx.Handle(h.revokeOtherSessions))
	r.Delete("/auth/sessions/{id}", httpx.Handle(h.revokeSession))
	r.Put("/auth/password", httpx.Handle(h.changePassword))
	r.Get("/users/{userID}", httpx.Handle(h.getUser))
	r.Put("/users/me/profile", httpx.Handle(h.updateProfile))
	r.Put("/users/me/locale", httpx.Handle(h.setLocale))
	r.Put("/users/me/presence", httpx.Handle(h.setPresence))
	r.Put("/users/me/status", httpx.Handle(h.setStatus))
	h.mountAccount(r)
}

// login handles POST /api/auth/login.
//
// @Summary Sign in
// @Description Sets the session cookie and a signed known-device cookie. Rate limited per IP (60 per 15 min, IPv6 per /64) and by escalating lockouts per address+username. Once the account-wide guess cap trips, devices without the known-device cookie get about one password check per minute per client address (and about ten per minute for the whole account); everything else is answered 429 without a check. The cap lasts at most 15 minutes. Limited requests answer 429 with Retry-After.
// @ID login
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body LoginRequest true "Request body."
// @Success 200 {object} UserEnvelope "Signed in."
// @Success 202 {object} TwoFactorChallenge "Correct password, but the account has two-factor sign-in: finish with POST /api/auth/login/2fa. No session is set yet."
// @Header 200 {string} Set-Cookie "Session cookie (HttpOnly, SameSite=Lax, Path=/; Secure on HTTPS)."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "INVALID_CREDENTIALS: unknown user or wrong password."
// @Failure 403 {object} httpx.ErrorResponse "ACCOUNT_DISABLED (correct password, disabled account) or cross-origin request rejected."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Failure 503 {object} httpx.ErrorResponse "UNAVAILABLE: too many password checks are queued; retry after the Retry-After seconds."
// @Header 503 {integer} Retry-After "Seconds until the client may retry."
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
		setRetryIfBusy(w, err)
		return err
	}
	if challenged, err := h.challengeIfNeeded(r.Context(), w, u, tv); challenged || err != nil {
		return err
	}
	h.auditUser(r.Context(), u, "auth.login")
	if err := h.Sessions.Start(w, u, tv); err != nil {
		return err
	}
	h.Sessions.markDevice(w, strings.ToLower(strings.TrimSpace(req.Username)), u.ID, tv)
	httpx.WriteJSON(w, http.StatusOK, UserEnvelope{User: h.withRulesPending(r.Context(), u)})
	return nil
}

// validateUsername rejects names that can never match an account. They get
// no bcrypt work (it would buy no timing protection, only attacker-controlled
// CPU) and never become a limiter key.
func validateUsername(username string) error {
	if !usernamePattern.MatchString(username) {
		return ErrInvalidCredentials
	}
	return nil
}

func (h *Handler) isUnknownDeviceThrottled(r *http.Request, account string) (time.Duration, bool) {
	locked, retry := h.accountFailures.IsLockedOut(account)
	if !locked || h.isKnownDevice(r, account) {
		return 0, false
	}
	// Own address first: a client that keeps asking only exhausts itself, and
	// its refused requests never reach the shared ceiling.
	if ok, deviceRetry := h.unknownDevice.Allow(httpx.ClientIPKey(r) + "|" + account); !ok {
		return max(deviceRetry, retry), true
	}
	if ok, totalRetry := h.unknownDeviceTotal.TryAllow(account); !ok {
		return max(totalRetry, retry), true
	}
	return 0, false
}

func (h *Handler) handleInvalidCredentials(ctx context.Context, key, account string) (time.Duration, error) {
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
		h.auditLoginFailure(ctx, account, "auth.login_locked")
		return retry, nil
	}
	h.auditLoginFailure(ctx, account, "auth.login_failed")
	return 0, ErrInvalidCredentials
}

// verifyLogin is the shared browser/native credential and lockout policy.
// Both transports must use this Handler's per-IP route limiter as well. It
// returns the token version captured by password verification; callers must
// never reload and substitute a newer version before issuing a session.
func (h *Handler) verifyLogin(r *http.Request, req LoginRequest) (*User, int, time.Duration, error) {
	username := strings.TrimSpace(req.Username)
	if err := validateUsername(username); err != nil {
		return nil, 0, 0, err
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
	if retry, throttled := h.isUnknownDeviceThrottled(r, account); throttled {
		return nil, 0, retry, nil
	}

	u, tv, err := h.verifyCredentials(r.Context(), username, req.Password)
	if err == ErrInvalidCredentials {
		retry, failErr := h.handleInvalidCredentials(r.Context(), key, account)
		return nil, 0, retry, failErr
	}
	if err != nil {
		return nil, 0, 0, err
	}
	h.loginFailures.ResetFailures(key)
	h.accountFailures.ResetFailures(account)
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

// setRetryIfBusy adds Retry-After to the 503 of a saturated hashing queue.
func setRetryIfBusy(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrServerBusy) {
		w.Header().Set("Retry-After", "2")
	}
}
