package server

import (
	"errors"
	"github.com/msch128/mnema-talk/internal/httpx"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// This fixed registry installs actual browser handlers only for reviewed
// metadata. Cookie-issuing password/global-logout, content/media, destructive
// storage and server-updater handlers intentionally have no native route.
type NativeMetadataRoute struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}

var nativeMetadataRoutes = []NativeMetadataRoute{
	{http.MethodGet, "/users/{userID}"},
	{http.MethodPut, "/users/me/profile"},
	{http.MethodPut, "/users/me/locale"},
	{http.MethodPut, "/users/me/presence"},
	{http.MethodPut, "/users/me/status"},
	{http.MethodGet, "/members"},
	{http.MethodGet, "/channels"},
	{http.MethodGet, "/read-state"},
	{http.MethodGet, "/admin/invites"},
	{http.MethodPost, "/admin/invites"},
	{http.MethodDelete, "/admin/invites/{id}"},
	{http.MethodGet, "/admin/users"},
	{http.MethodPost, "/admin/users/{id}/disable"},
	{http.MethodPost, "/admin/users/{id}/enable"},
	{http.MethodPost, "/admin/users/{id}/sessions/revoke"},
	{http.MethodPost, "/admin/users/{id}/kick"},
	{http.MethodPut, "/admin/users/{id}/status"},
	{http.MethodPost, "/admin/categories"},
	{http.MethodDelete, "/admin/categories/{id}"},
	{http.MethodPatch, "/admin/categories/{id}"},
	{http.MethodPost, "/admin/channels"},
	{http.MethodDelete, "/admin/channels/{id}"},
	{http.MethodPatch, "/admin/channels/{id}"},
	{http.MethodPost, "/admin/channels/{id}/duplicate"},
	{http.MethodPut, "/admin/layout"},
	{http.MethodGet, "/admin/system"},
	{http.MethodGet, "/admin/system/update"},
}

func NativeMetadataRoutes() []NativeMetadataRoute {
	return append([]NativeMetadataRoute(nil), nativeMetadataRoutes...)
}

func nativeMetadataHandlers(deps nativePreviewDependencies) (map[string]http.Handler, error) {
	routes := chi.NewRouter()
	deps.accounts.MountAuthenticated(routes)
	deps.chat.Mount(routes)
	routes.Route("/admin", func(admin chi.Router) {
		deps.accounts.MountAdmin(admin)
		deps.chat.MountAdmin(admin)
		deps.system.mountAdmin(admin)
	})
	available := map[string]http.Handler{}
	err := chi.Walk(routes, func(method, route string, handler http.Handler, _ ...func(http.Handler) http.Handler) error {
		available[method+" "+route] = handler
		return nil
	})
	if err != nil {
		return nil, err
	}
	selected := make(map[string]http.Handler, len(nativeMetadataRoutes))
	for _, route := range nativeMetadataRoutes {
		key := route.Method + " " + route.Path
		if available[key] == nil {
			return nil, errors.New("native metadata route has no existing handler")
		}
		if selected[key] != nil {
			return nil, errors.New("duplicate native metadata route")
		}
		handler := available[key]
		if route.Method == http.MethodGet || route.Method == http.MethodDelete || (route.Method == http.MethodPost && route.Path != "/admin/invites" && route.Path != "/admin/categories" && route.Path != "/admin/channels") {
			handler = nativeMetadataEmptyBody(handler)
		}
		selected[key] = handler
	}
	return selected, nil
}

// Fixed metadata reads and action-only writes never consume a caller payload.
// Reject even a single byte so the native contract cannot hide ignored content.
func nativeMetadataEmptyBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			var one [1]byte
			n, err := r.Body.Read(one[:])
			if n != 0 || err != io.EOF {
				httpx.WriteError(w, httpx.ErrInvalidInput("native metadata body must be empty"))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
