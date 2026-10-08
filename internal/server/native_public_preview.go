package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/msch128/mnema-talk/internal/httpx"
)

const (
	NativePublicLegalPath  = NativePreviewPrefix + "/public/legal"
	NativePublicHealthPath = NativePreviewPrefix + "/public/health"
)

// NewNativePublicPreviewRouter adds only the operator's already-public legal
// facts and dependency health to the explicit private transport preview. These
// responses precede passwords and require no access token. A client must first
// select its verified TLS profile; this constructor never grants auth or Core
// readiness, and production NewRouter does not invoke it.
func NewNativePublicPreviewRouter(deps Deps, options NativePreviewOptions) (*NativePreviewRouter, error) {
	if deps.Config == nil {
		return nil, errors.New("invalid native public preview dependencies")
	}
	trusted, err := httpx.ParseCIDRs(deps.Config.TrustedProxies)
	if err != nil {
		return nil, errors.New("invalid native public preview trust")
	}
	preview, err := NewNativePreviewRouter(deps, options)
	if err != nil {
		return nil, err
	}
	routes := chi.NewRouter()
	routes.Use(httpx.RequestID, httpx.ClientIPMiddleware(trusted), httpx.Logger, httpx.Recover(deps.Config.IsProduction()), httpx.SecurityHeaders(trusted, deps.Config.IsProduction()), preview.boundary, nativePublicPreviewBoundary)
	// Real cached health handler bounds DB/storage checks independently of callers;
	// the closed public pair has a separate native-only per-IP request ceiling.
	budget := httpx.NewRateLimiter(120, time.Minute)
	routes.Use(budget.PerIP)
	routes.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, httpx.NewAPIError(http.StatusMethodNotAllowed, httpx.CodeInvalidInput, "method not allowed"))
	})
	routes.Get(NativePublicLegalPath, legal(deps.Config))
	routes.Get(NativePublicHealthPath, health(newHealthCheck(deps.DB, deps.StorageReady, healthCacheTTL), deps.Version))
	base := preview.Router.Handler
	preview.Router.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == NativePublicLegalPath || r.URL.Path == NativePublicHealthPath {
			routes.ServeHTTP(w, r)
			return
		}
		base.ServeHTTP(w, r)
	})
	return preview, nil
}
func nativePublicPreviewBoundary(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Origin/Cookie/query/unsupported-version rejection and no-store/CSP already
		// come from the unchanged preview boundary. No Authorization header, even an
		// empty one, belongs to the pre-login resource operation.
		if nativePreviewHeaderPresent(r, "Authorization") {
			httpx.WriteError(w, httpx.ErrForbidden("native public metadata request rejected"))
			return
		}
		// The wire accepts known-empty bodies only. Reject framed/chunked bodies
		// without waiting for bytes, so a pre-login client cannot hold a DB check or
		// legal response hostage with an unfinished body.
		if r.ContentLength != 0 || len(r.TransferEncoding) != 0 || (r.Body != nil && r.Body != http.NoBody) {
			httpx.WriteError(w, httpx.ErrInvalidInput("native public metadata body must be empty"))
			return
		}
		next.ServeHTTP(w, r)
	})
}
