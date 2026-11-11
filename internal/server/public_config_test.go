package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/config"
)

func TestPublicConfigurationEmptyListsAndSTUN(t *testing.T) {
	rec := httptest.NewRecorder()
	LegalHandler(&config.Config{SessionExpiryHours: 25}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/legal", nil))
	if !strings.Contains(rec.Body.String(), `"stun_servers":[]`) || !strings.Contains(rec.Body.String(), `"session_expiry_days":2`) {
		t.Fatal(rec.Body.String())
	}
	rec = httptest.NewRecorder()
	webrtcConfig(&config.Config{WebRTCSTUNURLs: []string{"stun:stun.example.test"}}, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/webrtc/config", nil))
	if !strings.Contains(rec.Body.String(), "stun:stun.example.test") || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(rec.Body.String())
	}
}
