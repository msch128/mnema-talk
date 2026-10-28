package nativepreview

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/msch128/mnema-talk/internal/server"
)

// NewNativeRegistrationPreviewRouter adds invite-only account creation to the
// isolated native relay preview. Production NewRouter remains unchanged. A
// created account is not a transport session, device or encrypted-content grant.
func NewNativeRegistrationPreviewRouter(deps server.Deps, options NativePreviewOptions) (*NativeCiphertextPreviewRouter, error) {
	preview, err := NewNativeCiphertextPreviewRouter(deps, options)
	if err != nil {
		return nil, err
	}
	registration, err := preview.Native.RegistrationHandler()
	if err != nil {
		preview.Close()
		return nil, err
	}
	trusted := preview.PreviewDeps().Trusted
	handler := chi.Chain(append(nativeBaseMiddleware(deps, trusted), preview.boundary)...).Handler(http.StripPrefix(NativePreviewPrefix, authDispatch(registration)))
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
