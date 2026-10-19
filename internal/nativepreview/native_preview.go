package nativepreview

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/server"
)

// This constructor is an isolated transport preview. NewRouter never invokes
// it and production never mounts its native namespace. Authentication grants
// identify a session, not an E2EE device or permission to transfer content.
const NativePreviewPrefix = "/api/native/v1"

type NativeCompatibility string

const (
	NativeSupported   NativeCompatibility = "supported"
	NativeDeprecated  NativeCompatibility = "deprecated"
	NativeUnsupported NativeCompatibility = "unsupported"
)

type NativePreviewOptions struct {
	CommunityID    string
	InstanceOrigin string
	Compatibility  NativeCompatibility
}

type NativePreviewRouter struct {
	*server.Router
	Native   *auth.NativeHandler
	Sessions *auth.NativeSessions
	options  NativePreviewOptions
}

type NativePreviewInstance struct {
	CommunityID      string
	Origin           string
	FamilyID         uuid.UUID
	ClientInstanceID uuid.UUID
}
type nativePreviewInstanceKey struct{}

func NativePreviewInstanceFrom(ctx context.Context) (NativePreviewInstance, bool) {
	value, ok := ctx.Value(nativePreviewInstanceKey{}).(NativePreviewInstance)
	return value, ok
}

func validateNativePreviewOptions(options NativePreviewOptions) error {
	if options.CommunityID == "" || len(options.CommunityID) > 128 {
		return errors.New("invalid native community identifier")
	}
	for _, c := range []byte(options.CommunityID) {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return errors.New("invalid native community identifier")
		}
	}
	origin, err := url.Parse(options.InstanceOrigin)
	if err != nil || origin.Scheme != "https" || origin.Hostname() == "" || origin.User != nil || origin.RawQuery != "" || origin.ForceQuery || origin.Fragment != "" || origin.Path != "" || origin.RawPath != "" || origin.String() != options.InstanceOrigin {
		return errors.New("invalid native instance origin")
	}
	if options.Compatibility != NativeSupported && options.Compatibility != NativeDeprecated && options.Compatibility != NativeUnsupported {
		return errors.New("invalid native API compatibility")
	}
	return nil
}

// NewNativePreviewRouter assembles real browser/native handlers over the same
// account lockout budgets, current-user DB checks, role checks and Hub. Only
// explicitly listed metadata routes are installed; content handlers are absent.
func NewNativePreviewRouter(deps server.Deps, options NativePreviewOptions) (*NativePreviewRouter, error) {
	if err := validateNativePreviewOptions(options); err != nil {
		return nil, err
	}
	if deps.Config == nil || deps.DB == nil {
		return nil, errors.New("invalid native preview dependencies")
	}
	browser, err := server.NewRouter(deps)
	if err != nil {
		return nil, err
	}
	native, sessions, err := newNativeAuth(deps, browser)
	if err != nil {
		browser.Close()
		return nil, err
	}
	result := &NativePreviewRouter{Router: browser, Native: native, Sessions: sessions, options: options}
	nativeRoutes, err := result.nativeRoutes(deps)
	if err != nil {
		browser.Close()
		return nil, err
	}
	// Keep the browser handler object and routes completely intact. Only this
	// explicit preview wrapper intercepts the two fixed native/discovery paths.
	base := browser.Handler
	trusted := browser.PreviewDeps().Trusted
	discoveryHandler := httpx.SecurityHeaders(trusted, deps.Config.IsProduction())(http.HandlerFunc(result.discovery))
	result.Router.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/mnema" {
			discoveryHandler.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == NativePreviewPrefix || strings.HasPrefix(r.URL.Path, NativePreviewPrefix+"/") {
			http.StripPrefix(NativePreviewPrefix, nativeRoutes).ServeHTTP(w, r)
			return
		}
		base.ServeHTTP(w, r)
	})
	return result, nil
}

// newNativeAuth builds the native sessions and handler over the browser
// router's account handler and binds the hub's family control. The caller
// closes the browser router on error.
func newNativeAuth(deps server.Deps, browser *server.Router) (*auth.NativeHandler, *auth.NativeSessions, error) {
	lifetime := time.Duration(deps.Config.SessionExpiryHours) * time.Hour
	sessions, err := auth.NewNativeSessions(deps.DB, auth.NativePolicy{SessionLifetime: lifetime, FamilyLifetime: min(lifetime, 30*24*time.Hour)})
	if err != nil {
		return nil, nil, err
	}
	native, err := auth.NewNativeHandler(browser.PreviewDeps().Accounts, sessions)
	if err != nil {
		return nil, nil, err
	}
	if err = sessions.BindNativeFamilyControl(browser.Hub); err != nil {
		return nil, nil, err
	}
	return native, sessions, nil
}

// nativeRoutes is the router for everything under NativePreviewPrefix: the
// metadata WebSocket, the native auth endpoints and the metadata routes.
func (p *NativePreviewRouter) nativeRoutes(deps server.Deps) (chi.Router, error) {
	browser, native := p.Router, p.Native
	trusted := browser.PreviewDeps().Trusted
	routes := chi.NewRouter()
	routes.Use(httpx.RequestID, httpx.ClientIPMiddleware(trusted), httpx.Logger, httpx.Recover(deps.Config.IsProduction()), httpx.SecurityHeaders(trusted, deps.Config.IsProduction()), p.boundary)
	routes.Get("/ws", browser.Hub.NativeMetadataWebSocketHandler(native).ServeHTTP)
	nativeAuth := native.Handler()
	authDispatch := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A fresh chi route context preserves the fixed relative /auth path and
		// existing request/IP context without inheriting the enclosing matcher.
		ctx := context.WithValue(r.Context(), chi.RouteCtxKey, (*chi.Context)(nil))
		nativeAuth.ServeHTTP(w, r.WithContext(ctx))
	})
	routes.Method(http.MethodPost, "/auth/login", authDispatch)
	routes.Method(http.MethodPost, "/auth/refresh", authDispatch)
	routes.Method(http.MethodGet, "/auth/me", authDispatch)
	routes.Method(http.MethodPost, "/auth/logout", authDispatch)
	metadataHandlers, err := nativeMetadataHandlers(browser.PreviewDeps())
	if err != nil {
		return nil, err
	}
	routes.Group(func(metadata chi.Router) {
		metadata.Use(native.RequireUser, p.instanceContext, browser.PreviewDeps().PerUser.By(auth.UserKey), httpx.MaxBody(httpx.DefaultMaxBody))
		metadata.Method(http.MethodGet, "/health", nativeMetadataEmptyBody(server.HealthHandler(deps)))
		metadata.Method(http.MethodGet, "/legal", nativeMetadataEmptyBody(server.LegalHandler(deps.Config)))
		for _, route := range nativeMetadataRoutes {
			handler := metadataHandlers[route.Method+" "+route.Path]
			if strings.HasPrefix(route.Path, "/admin/") {
				handler = auth.RequireOwner(handler)
			}
			metadata.Method(route.Method, route.Path, handler)
		}
		metadata.Handle("/*", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			httpx.WriteError(w, httpx.ErrUnavailable("native content authorization unavailable"))
		}))
	})
	return routes, nil
}

func nativePreviewHeaderPresent(r *http.Request, name string) bool {
	for key := range r.Header {
		if strings.EqualFold(key, name) {
			return true
		}
	}
	return false
}
func (r *NativePreviewRouter) boundary(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		if nativePreviewHeaderPresent(request, "Origin") || nativePreviewHeaderPresent(request, "Cookie") || request.URL.RawQuery != "" || request.URL.ForceQuery || request.URL.User != nil || request.URL.Fragment != "" {
			httpx.WriteError(w, httpx.ErrForbidden("native transport request rejected"))
			return
		}
		if r.options.Compatibility == NativeUnsupported {
			httpx.WriteError(w, httpx.ErrUnavailable("native API version unsupported"))
			return
		}
		next.ServeHTTP(w, request)
	})
}
func (r *NativePreviewRouter) instanceContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		principal, ok := auth.NativePrincipalFrom(request.Context())
		if !ok {
			httpx.WriteError(w, httpx.ErrUnauthorized("authentication required"))
			return
		}
		instance := NativePreviewInstance{CommunityID: r.options.CommunityID, Origin: r.options.InstanceOrigin, FamilyID: principal.FamilyID(), ClientInstanceID: principal.ClientInstanceID()}
		next.ServeHTTP(w, request.WithContext(context.WithValue(request.Context(), nativePreviewInstanceKey{}, instance)))
	})
}
