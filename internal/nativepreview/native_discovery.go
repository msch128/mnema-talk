package nativepreview

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

func discoveryTransportRejected(request *http.Request) bool {
	if nativePreviewHeaderPresent(request, "Origin") || nativePreviewHeaderPresent(request, "Cookie") {
		return true
	}
	if nativePreviewHeaderPresent(request, "Authorization") || request.URL.RawQuery != "" {
		return true
	}
	if request.URL.ForceQuery || request.URL.User != nil {
		return true
	}
	return request.URL.Fragment != ""
}

func (r *NativePreviewRouter) discovery(w http.ResponseWriter, request *http.Request) {
	nativeNoStoreHeaders(w)
	if request.Method != http.MethodGet {
		nativeMethodNotAllowed(w, request)
		return
	}
	if discoveryTransportRejected(request) {
		httpx.WriteError(w, httpx.ErrForbidden("native discovery request rejected"))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, nativeDiscoveryDocument{Protocol: "mnema-desktop-discovery-v1", CommunityID: r.options.CommunityID, APIVersions: []int{1}, E2EERequired: true, NativeAPI: nativeAPIDescriptor{
		Protocol: "mnema-native-preview-v1", APIVersion: 1, Prefix: NativePreviewPrefix, Compatibility: r.options.Compatibility, Authentication: "opaque-bearer-v1",
		Capabilities: []string{"authentication", "profile", "presence", "members", "channels", "admin_metadata"}, ContentAuthorization: "unavailable",
	}})
}
