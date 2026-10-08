package server

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// NewNativeRegistrationPreviewRouter adds invite-only account creation to the
// isolated native relay preview. Production NewRouter remains unchanged. A
// created account is not a transport session, device or encrypted-content grant.
func NewNativeRegistrationPreviewRouter(deps Deps, options NativePreviewOptions) (*NativeCiphertextPreviewRouter, error) {
	preview, err := NewNativeCiphertextPreviewRouter(deps, options)
	if err != nil {
		return nil, err
	}
	registration, err := preview.Native.RegistrationHandler()
	if err != nil {
		preview.Close()
		return nil, err
	}
	dispatch := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		ctx := context.WithValue(request.Context(), chi.RouteCtxKey, (*chi.Context)(nil))
		registration.ServeHTTP(w, request.WithContext(ctx))
	})
	trusted, err := httpx.ParseCIDRs(deps.Config.TrustedProxies)
	if err != nil {
		preview.Close()
		return nil, err
	}
	handler := httpx.RequestID(httpx.ClientIPMiddleware(trusted)(httpx.Logger(httpx.Recover(deps.Config.IsProduction())(httpx.SecurityHeaders(trusted, deps.Config.IsProduction())(preview.boundary(http.StripPrefix(NativePreviewPrefix, dispatch)))))))
	base := preview.Router.Handler
	preview.Router.Handler = http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == NativePreviewPrefix+"/auth/register" {
			handler.ServeHTTP(w, request)
			return
		}
		base.ServeHTTP(w, request)
	})
	return preview, nil
}
