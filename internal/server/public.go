package server

import (
	"net/http"

	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// LegalVersion changes whenever recipients, retention or rights in the
// privacy policy change.
const LegalVersion = "1.3"

// legal exposes the operator details and the facts the privacy policy must
// state (Art. 13 GDPR). Everything comes from env so the public repo stays generic.
func legal(cfg *config.Config) http.HandlerFunc {
	stun := cfg.WebRTCSTUNURLs
	if stun == nil {
		stun = []string{}
	}
	body := map[string]any{
		"operator_name":        cfg.LegalOperatorName,
		"operator_email":       cfg.LegalOperatorEmail,
		"operator_country":     cfg.LegalOperatorCountry,
		"project_notice":       cfg.LegalProjectNotice,
		"media_retention_days": cfg.MediaRetentionDays, // 0 = kept until deleted manually
		"session_expiry_days":  (cfg.SessionExpiryHours + 23) / 24,
		"stun_servers":         stun,
		"legal_version":        LegalVersion,
	}
	return func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, body)
	}
}

// webrtcConfig gives the browser its ICE servers (none unless WEBRTC_STUN_URLS is set).
func webrtcConfig(cfg *config.Config) http.HandlerFunc {
	ice := []map[string]any{}
	if len(cfg.WebRTCSTUNURLs) > 0 {
		ice = append(ice, map[string]any{"urls": cfg.WebRTCSTUNURLs})
	}
	body := map[string]any{"ice_servers": ice}
	return func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, body)
	}
}
