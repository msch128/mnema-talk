package auth

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// RegistrationHandler is an explicit native-only account creation preview.
// It shares invite consumption and the browser registration IP budget. It
// creates neither a cookie nor a native session; a separate login is required.
func (h *NativeHandler) RegistrationHandler() (http.Handler, error) {
	if h == nil || h.accounts == nil || h.accounts.Sessions == nil || h.accounts.Sessions.DB == nil || h.accounts.registerPerIP == nil {
		return nil, errors.New("invalid native registration dependencies")
	}
	r := chi.NewRouter()
	r.Use(nativeRequestBoundary)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			if request.URL.ForceQuery || request.URL.RawPath != "" {
				httpx.WriteError(w, httpx.ErrInvalidInput("invalid native registration target"))
				return
			}
			if err := noNativeAuthorization(request); err != nil {
				httpx.WriteError(w, err)
				return
			}
			next.ServeHTTP(w, request)
		})
	})
	r.NotFound(func(w http.ResponseWriter, request *http.Request) {
		httpx.WriteError(w, httpx.ErrNotFound("native registration route not found"))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, request *http.Request) {
		httpx.WriteError(w, httpx.NewAPIError(http.StatusMethodNotAllowed, httpx.CodeInvalidInput, "method not allowed"))
	})
	r.With(h.accounts.registerPerIP.PerIP).Post("/auth/register", httpx.Handle(h.registerNative))
	return r, nil
}
func (h *NativeHandler) registerNative(w http.ResponseWriter, request *http.Request) error {
	fields, err := decodeNativeStrings(w, request, "username", "display_name", "password", "invite_code")
	if err != nil {
		return nativeHandlerError(err)
	}
	user, err := Register(request.Context(), h.accounts.Sessions.DB, strings.TrimSpace(fields["username"]), fields["display_name"], fields["password"], fields["invite_code"])
	if err != nil {
		return nativeHandlerError(err)
	}
	if h.accounts.Events != nil {
		h.accounts.Events.Broadcast("member_joined", user.Public())
	}
	httpx.WriteJSON(w, http.StatusCreated, UserEnvelope{User: *user})
	return nil
}
