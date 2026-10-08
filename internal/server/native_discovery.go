package server

import (
	"net/http"

	"github.com/msch128/mnema-talk/internal/httpx"
)

type nativeAPIDescriptor struct {
	Protocol             string              `json:"protocol"`
	APIVersion           int                 `json:"api_version"`
	Prefix               string              `json:"prefix"`
	Compatibility        NativeCompatibility `json:"compatibility"`
	Authentication       string              `json:"authentication"`
	Capabilities         []string            `json:"capabilities"`
	ContentAuthorization string              `json:"content_authorization"`
}
type nativeDiscoveryDocument struct {
	Protocol     string              `json:"protocol"`
	CommunityID  string              `json:"community_id"`
	APIVersions  []int               `json:"api_versions"`
	E2EERequired bool                `json:"e2ee_required"`
	NativeAPI    nativeAPIDescriptor `json:"native_api"`
}

func (r *NativePreviewRouter) discovery(w http.ResponseWriter, request *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	if request.Method != http.MethodGet {
		httpx.WriteError(w, httpx.NewAPIError(http.StatusMethodNotAllowed, httpx.CodeInvalidInput, "method not allowed"))
		return
	}
	if nativePreviewHeaderPresent(request, "Origin") || nativePreviewHeaderPresent(request, "Cookie") || nativePreviewHeaderPresent(request, "Authorization") || request.URL.RawQuery != "" || request.URL.ForceQuery || request.URL.User != nil || request.URL.Fragment != "" {
		httpx.WriteError(w, httpx.ErrForbidden("native discovery request rejected"))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, nativeDiscoveryDocument{Protocol: "mnema-desktop-discovery-v1", CommunityID: r.options.CommunityID, APIVersions: []int{1}, E2EERequired: true, NativeAPI: nativeAPIDescriptor{
		Protocol: "mnema-native-preview-v1", APIVersion: 1, Prefix: NativePreviewPrefix, Compatibility: r.options.Compatibility, Authentication: "opaque-bearer-v1",
		Capabilities: []string{"authentication", "profile", "presence", "members", "channels", "admin_metadata"}, ContentAuthorization: "unavailable",
	}})
}
