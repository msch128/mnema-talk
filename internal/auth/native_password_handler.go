package auth

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/msch128/mnema-talk/internal/httpx"
)

func nativeAccountsReady(h *NativeHandler) bool {
	if h == nil || h.accounts == nil {
		return false
	}
	return h.accounts.Sessions != nil && h.accounts.Sessions.DB != nil
}

func invalidPasswordHandler(h *NativeHandler) bool {
	if !nativeAccountsReady(h) || h.sessions == nil {
		return true
	}
	if h.accounts.passwordFails == nil {
		return true
	}
	return h.sessions.pool != h.accounts.Sessions.DB
}

// PasswordChangeHandler returns an unmounted native-only credential transition.
// It shares the browser account's wrong-password lockout. A success is a fresh
// initial native family, never a cookie, refresh successor or E2EE grant.
func (h *NativeHandler) PasswordChangeHandler() (http.Handler, error) {
	if invalidPasswordHandler(h) {
		return nil, errors.New("invalid native password dependencies")
	}
	control, ok := h.accounts.Live.(NativeCredentialControl)
	if !ok || control == nil {
		return nil, errors.New("native credential cutoff unavailable")
	}
	return nativePasswordRouter(h, control), nil
}

// passNative is the guard of a native router that needs none beyond the boundary.
func passNative(next http.Handler) http.Handler { return next }

func nativeRouter(guard func(http.Handler) http.Handler, missing string) *chi.Mux {
	r := chi.NewRouter()
	r.Use(nativeRequestBoundary)
	r.Use(guard)
	r.NotFound(nativeMissing(missing))
	r.MethodNotAllowed(nativeMethodRejected)
	return r
}

func nativePasswordRouter(h *NativeHandler, control NativeCredentialControl) http.Handler {
	r := nativeRouter(passNative, "native password route not found")
	// These local request budgets complement the shared wrong-password account
	// lockout; constructing the router once preserves their state across calls.
	ip := httpx.NewRateLimiter(60, time.Minute)
	user := httpx.NewRateLimiter(60, time.Minute)
	r.With(ip.PerIP, h.RequireUser, user.By(nativePasswordUser)).
		Put("/auth/password", httpx.Handle(h.changePassword(control)))
	return r
}

func nativeMissing(message string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteError(w, httpx.ErrNotFound(message))
	}
}

func nativeMethodRejected(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteError(w, httpx.NewAPIError(http.StatusMethodNotAllowed, httpx.CodeInvalidInput, "method not allowed"))
}

func nativePasswordUser(r *http.Request) string {
	return UserFrom(r.Context()).ID.String()
}

// changePassword is the PUT /auth/password handler: it counts wrong passwords
// toward the account lockout and answers with a fresh native grant.
func (h *NativeHandler) changePassword(control NativeCredentialControl) func(http.ResponseWriter, *http.Request) error {
	return func(w http.ResponseWriter, request *http.Request) error {
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
			return nativeFail(w, err)
		}
		h.accounts.passwordFails.ResetFailures(key)
		user := principal.User()
		h.accounts.auditUser(request.Context(), &user, "auth.password_change")
		writeNativeGrant(w, grant)
		return nil
	}
}
