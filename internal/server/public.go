package server

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/api"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// turnCredentialTTL is how long a TURN credential handed to a client is valid.
const turnCredentialTTL = 12 * time.Hour

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
		"turn_servers":         nonNil(cfg.WebRTCTURNURLs),
		"legal_version":        LegalVersion,
	}
	return func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, body)
	}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// turnCredentials returns time-limited TURN credentials in coturn's REST
// format: username "<expiry unix>:<user id>", password
// base64(HMAC-SHA1(secret, username)). The secret never leaves the server.
func turnCredentials(secret string, userID uuid.UUID, now time.Time, ttl time.Duration) (string, string) {
	username := strconv.FormatInt(now.Add(ttl).Unix(), 10) + ":" + userID.String()
	mac := hmac.New(sha1.New, []byte(secret))
	mac.Write([]byte(username))
	return username, base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// webrtcConfig gives a signed-in browser its ICE servers: STUN when
// WEBRTC_STUN_URLS is set, and a TURN relay with fresh credentials when
// WEBRTC_TURN_URLS is set. Without either the list is empty.
func webrtcConfig(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ice := []map[string]any{}
		if len(cfg.WebRTCSTUNURLs) > 0 {
			ice = append(ice, map[string]any{"urls": cfg.WebRTCSTUNURLs})
		}
		if len(cfg.WebRTCTURNURLs) > 0 {
			user, cred := turnCredentials(cfg.WebRTCTURNSecret, auth.UserFrom(r.Context()).ID, time.Now(), turnCredentialTTL)
			ice = append(ice, map[string]any{"urls": cfg.WebRTCTURNURLs, "username": user, "credential": cred})
		}
		w.Header().Set("Cache-Control", "no-store")
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ice_servers": ice})
	}
}

// openAPISpec serves the OpenAPI description of the REST API.
func openAPISpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(api.Spec)
}
