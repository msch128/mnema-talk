package auth

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// PasswordChangeHandler returns an unmounted native-only credential transition.
// It shares the browser account's wrong-password lockout. A success is a fresh
// initial native family, never a cookie, refresh successor or E2EE grant.
func (h *NativeHandler) PasswordChangeHandler() (http.Handler, error) {
	if h == nil || h.accounts == nil || h.accounts.Sessions == nil || h.accounts.Sessions.DB == nil || h.accounts.passwordFails == nil || h.sessions == nil || h.sessions.pool != h.accounts.Sessions.DB {
		return nil, errors.New("invalid native password dependencies")
	}
	control, ok := h.accounts.Live.(NativeCredentialControl)
	if !ok || control == nil {
		return nil, errors.New("native credential cutoff unavailable")
	}
	r := chi.NewRouter()
	r.Use(nativeRequestBoundary)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			if request.URL.ForceQuery || request.URL.RawPath != "" {
				httpx.WriteError(w, httpx.ErrInvalidInput("invalid native password target"))
				return
			}
			next.ServeHTTP(w, request)
		})
	})
	r.NotFound(func(w http.ResponseWriter, request *http.Request) {
		httpx.WriteError(w, httpx.ErrNotFound("native password route not found"))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, request *http.Request) {
		httpx.WriteError(w, httpx.NewAPIError(http.StatusMethodNotAllowed, httpx.CodeInvalidInput, "method not allowed"))
	})
	// These local request budgets complement the shared wrong-password account
	// lockout; constructing the router once preserves their state across calls.
	ip := httpx.NewRateLimiter(60, time.Minute)
	user := httpx.NewRateLimiter(60, time.Minute)
	r.With(ip.PerIP, h.RequireUser, user.By(func(r *http.Request) string { return UserFrom(r.Context()).ID.String() })).Put("/auth/password", httpx.Handle(func(w http.ResponseWriter, request *http.Request) error {
		fields, err := decodeNativeStrings(w, request, "current_password", "new_password")
		if err != nil {
			return nativeHandlerError(err)
		}
		principal, ok := NativePrincipalFrom(request.Context())
		if !ok {
			return ErrNativeUnauthorized
		}
		key := principal.User().ID.String()
		if locked, retry := h.accounts.passwordFails.IsLockedOut(key); locked {
			httpx.WriteRateLimited(w, retry)
			return nil
		}
		grant, err := h.sessions.changePasswordAndReissue(request.Context(), principal, fields["current_password"], fields["new_password"], control)
		if errors.Is(err, ErrWrongPassword) {
			h.accounts.passwordFails.RecordFailure(key)
		}
		if err != nil {
			return nativeHandlerError(err)
		}
		h.accounts.passwordFails.ResetFailures(key)
		writeNativeGrant(w, grant)
		return nil
	}))
	return r, nil
}
