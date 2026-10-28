package nativepreview

import (
	"context"
	"errors"
	"net"
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

func communityRune(c byte) bool {
	if c >= 'a' && c <= 'z' {
		return true
	}
	if c >= 'A' && c <= 'Z' {
		return true
	}
	if c >= '0' && c <= '9' {
		return true
	}
	return c == '-' || c == '_'
}

func communityIDOK(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, c := range []byte(id) {
		if !communityRune(c) {
			return false
		}
	}
	return true
}

func originPartsOK(origin *url.URL) bool {
	if origin.Scheme != "https" || origin.Hostname() == "" {
		return false
	}
	if origin.User != nil || origin.RawQuery != "" {
		return false
	}
	if origin.ForceQuery || origin.Fragment != "" {
		return false
	}
	return origin.Path == "" && origin.RawPath == ""
}

func instanceOriginOK(raw string) bool {
	origin, err := url.Parse(raw)
	if err != nil || origin == nil {
		return false
	}
	if !originPartsOK(origin) {
		return false
	}
	return origin.String() == raw
}

func compatibilityOK(value NativeCompatibility) bool {
	if value == NativeSupported || value == NativeDeprecated {
		return true
	}
	return value == NativeUnsupported
}

func validateNativePreviewOptions(options NativePreviewOptions) error {
	if !communityIDOK(options.CommunityID) {
		return errors.New("invalid native community identifier")
	}
	if !instanceOriginOK(options.InstanceOrigin) {
		return errors.New("invalid native instance origin")
	}
	if !compatibilityOK(options.Compatibility) {
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
	result.Router.Handler = previewDispatch(base, discoveryHandler, nativeRoutes)
	return result, nil
}

func previewNativePath(path string) bool {
	if path == NativePreviewPrefix {
		return true
	}
	return strings.HasPrefix(path, NativePreviewPrefix+"/")
}

func previewDispatch(base http.Handler, discovery http.Handler, native http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/mnema" {
			discovery.ServeHTTP(w, r)
			return
		}
		if previewNativePath(r.URL.Path) {
			http.StripPrefix(NativePreviewPrefix, native).ServeHTTP(w, r)
			return
		}
		base.ServeHTTP(w, r)
	})
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
	routes.Use(append(nativeBaseMiddleware(deps, trusted), p.boundary)...)
	routes.Get("/ws", browser.Hub.NativeMetadataWebSocketHandler(native).ServeHTTP)
	dispatch := authDispatch(native.Handler())
	routes.Method(http.MethodPost, "/auth/login", dispatch)
	routes.Method(http.MethodPost, "/auth/refresh", dispatch)
	routes.Method(http.MethodGet, "/auth/me", dispatch)
	routes.Method(http.MethodPost, "/auth/logout", dispatch)
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
		// The catch-all matches every method, so chi never reaches its own
		// MethodNotAllowed: a wrong verb on a registered path is answered here.
		metadata.Handle("/*", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if nativeMetadataPathRegistered(r.URL.Path) {
				nativeMethodNotAllowed(w, r)
				return
			}
			httpx.WriteError(w, httpx.ErrUnavailable("native content authorization unavailable"))
		}))
	})
	return routes, nil
}

// nativeBaseMiddleware is the chain every native route set starts with.
func nativeBaseMiddleware(deps server.Deps, trusted []*net.IPNet) []func(http.Handler) http.Handler {
	return []func(http.Handler) http.Handler{
		httpx.RequestID, httpx.ClientIPMiddleware(trusted), httpx.Logger, httpx.Recover(deps.Config.IsProduction()), httpx.SecurityHeaders(trusted, deps.Config.IsProduction()),
	}
}

// nativeMetadataPathRegistered reports whether path is a metadata route under
// some method (the health and legal pair included).
func nativeMetadataPathRegistered(path string) bool {
	known := chi.NewRouter()
	noop := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	known.Method(http.MethodGet, "/health", noop)
	known.Method(http.MethodGet, "/legal", noop)
	for _, route := range nativeMetadataRoutes {
		known.Method(route.Method, route.Path, noop)
	}
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		if known.Match(chi.NewRouteContext(), method, path) {
			return true
		}
	}
	return false
}

// nativeNoStoreHeaders marks a native response as uncacheable and unframeable.
func nativeNoStoreHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
}

func nativeMethodNotAllowed(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteError(w, httpx.NewAPIError(http.StatusMethodNotAllowed, httpx.CodeInvalidInput, "method not allowed"))
}

// nativeBodyPresent reports whether the request declares or carries a body.
// The native wire accepts known-empty bodies only on these routes.
func nativeBodyPresent(r *http.Request) bool {
	if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		return true
	}
	return r.Body != nil && r.Body != http.NoBody
}

// authDispatch runs h with a fresh chi route context, which preserves the
// fixed relative path and the request/IP context without inheriting the
// enclosing matcher.
func authDispatch(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), chi.RouteCtxKey, (*chi.Context)(nil))
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

func nativePreviewHeaderPresent(r *http.Request, name string) bool {
	for key := range r.Header {
		if strings.EqualFold(key, name) {
			return true
		}
	}
	return false
}
func previewHeadersRejected(r *http.Request) bool {
	if nativePreviewHeaderPresent(r, "Origin") {
		return true
	}
	return nativePreviewHeaderPresent(r, "Cookie")
}

func previewQueryRejected(u *url.URL) bool {
	if u.RawQuery != "" || u.ForceQuery {
		return true
	}
	return u.User != nil || u.Fragment != ""
}

func previewTransportRejected(r *http.Request) bool {
	if previewHeadersRejected(r) {
		return true
	}
	return previewQueryRejected(r.URL)
}

func (r *NativePreviewRouter) boundary(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		nativeNoStoreHeaders(w)
		if previewTransportRejected(request) {
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
