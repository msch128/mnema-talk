package auth

import (
	"errors"
	"net/http"
	"strings"

	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
)

func invalidRegistrationHandler(h *NativeHandler) bool {
	if !nativeAccountsReady(h) {
		return true
	}
	return h.accounts.registerPerIP == nil
}

func requireAnonymousNative(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if err := noNativeAuthorization(request); err != nil {
			httpx.WriteError(w, err)
			return
		}
		next.ServeHTTP(w, request)
	})
}

// RegistrationHandler is an explicit native-only account creation preview.
// It shares invite consumption and the browser registration IP budget. It
// creates neither a cookie nor a native session; a separate login is required.
func (h *NativeHandler) RegistrationHandler() (http.Handler, error) {
	if invalidRegistrationHandler(h) {
		return nil, errors.New("invalid native registration dependencies")
	}
	r := nativeRouter(requireAnonymousNative, "native registration route not found")
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
		h.accounts.Events.Broadcast(events.MemberJoined, user.Public())
	}
	httpx.WriteJSON(w, http.StatusCreated, UserEnvelope{User: h.accounts.withRulesPending(request.Context(), user)})
	return nil
}
