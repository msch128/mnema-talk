//go:build integration

package server

import (
	"net/http"
	"testing"

	"github.com/msch128/mnema-talk/internal/config"
)

// Every native path that is not an explicitly installed metadata route is
// answered by the closed catch-all (401 without a token, 503 "content
// unavailable" with one), whatever the method: nothing falls through to the
// browser router or the web app.
func TestNativePreviewUnknownRoutesNeverReachBrowserRoutes(t *testing.T) {
	a := newPreviewTestApp(t, NativeSupported, false)
	grant := a.login(t, a.user)
	a.request(t, http.MethodGet, NativePreviewPrefix+"/auth/login", nil, "", nil, http.StatusUnauthorized)
	a.request(t, http.MethodGet, NativePreviewPrefix+"/no-such-route", nil, "", nil, http.StatusUnauthorized)
	a.request(t, http.MethodGet, NativePreviewPrefix+"/no-such-route", nil, grant.Access, nil, http.StatusServiceUnavailable)
	a.request(t, http.MethodPost, NativePreviewPrefix+"/members", nil, grant.Access, nil, http.StatusServiceUnavailable)
}

// A corrupted metadata registry stops assembly of the whole preview router
// instead of serving a partial one.
func TestNativePreviewAssemblyFailsOnCorruptRegistry(t *testing.T) {
	a := newPreviewTestApp(t, NativeSupported, false)
	cfg, err := config.FromEnv(func(key string) (string, bool) {
		v, ok := map[string]string{"DATABASE_URL": "unused", "PUBLIC_URL": a.server.URL, "JWT_SECRET": "native-preview-fixture-secret-native-preview", "LINK_PREVIEWS_ENABLED": "false"}[key]
		return v, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	original := nativeMetadataRoutes
	defer func() { nativeMetadataRoutes = original }()
	nativeMetadataRoutes = append(append([]NativeMetadataRoute(nil), original...), NativeMetadataRoute{Method: http.MethodGet, Path: "/not-installed"})
	options := NativePreviewOptions{CommunityID: "native-preview-fixture", InstanceOrigin: a.server.URL, Compatibility: NativeSupported}
	if router, err := NewNativePreviewRouter(Deps{Config: cfg, DB: a.pool}, options); err == nil {
		router.Close()
		t.Fatal("router assembled from a corrupt registry")
	}
}
