package auth

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
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
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
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
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"user": u})
	return nil
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		Password    string `json:"password"`
		InviteCode  string `json:"invite_code"`
	}
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
	h.Events.Broadcast("member_joined", u)
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"user": u})
	return nil
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) error {
	h.Sessions.End(w)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// logoutAll ends every session of the user, including copied cookies.
func (h *Handler) logoutAll(w http.ResponseWriter, r *http.Request) error {
	if err := RevokeSessions(r.Context(), h.Sessions.DB, UserFrom(r.Context()).ID); err != nil {
		return err
	}
	h.Sessions.End(w)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *Handler) setLocale(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Locale string `json:"locale"`
	}
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

func (h *Handler) me(w http.ResponseWriter, r *http.Request) error {
	httpx.WriteJSON(w, http.StatusOK, UserFrom(r.Context()))
	return nil
}

func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request) error {
	u := UserFrom(r.Context())
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
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

func (h *Handler) getUser(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "userID")
	if err != nil {
		return err
	}
	u, err := GetUser(r.Context(), h.Sessions.DB, id)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, u)
	return nil
}

func (h *Handler) updateProfile(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		DisplayName string `json:"display_name"`
		Bio         string `json:"bio"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	u, err := UpdateProfile(r.Context(), h.Sessions.DB, UserFrom(r.Context()).ID, req.DisplayName, req.Bio)
	if err != nil {
		return err
	}
	h.Events.Broadcast("user_update", u)
	httpx.WriteJSON(w, http.StatusOK, u)
	return nil
}
