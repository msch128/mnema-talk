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
	"github.com/msch128/mnema-talk/web"
)

// turnCredentialTTL is how long a TURN credential handed to a client is valid.
const turnCredentialTTL = 12 * time.Hour

// LegalVersion changes whenever recipients, retention or rights in the
// privacy policy change.
const LegalVersion = "1.4"

// Legal is the response of GET /api/legal: the operator details and the facts
// the privacy policy must state.
type Legal struct {
	OperatorName    string `json:"operator_name"`
	OperatorEmail   string `json:"operator_email"`
	OperatorCountry string `json:"operator_country"`
	ProjectNotice   string `json:"project_notice"`
	// MediaRetentionDays is 0 when media is kept until deleted manually.
	MediaRetentionDays int      `json:"media_retention_days"`
	SessionExpiryDays  int      `json:"session_expiry_days"`
	STUNServers        []string `json:"stun_servers"`
	TURNServers        []string `json:"turn_servers"`
	// UpdateCheck is true when the server asks GitHub for new releases
	// (UPDATE_CHECK_ENABLED); the privacy policy then names the connection.
	UpdateCheck  bool   `json:"update_check"`
	LegalVersion string `json:"legal_version"`
}

// IceServer is one entry of the WebRTC ICE configuration.
type IceServer struct {
	URLs []string `json:"urls"`
	// Username is set for TURN only.
	Username string `json:"username,omitempty" binding:"optional"`
	// Credential is set for TURN only and valid for 12 hours.
	Credential string `json:"credential,omitempty" binding:"optional"`
}

// ICEConfig is the response of GET /api/webrtc/config.
type ICEConfig struct {
	ICEServers []IceServer `json:"ice_servers"`
}

// legal exposes the operator details and the facts the privacy policy must
// state (Art. 13 GDPR). Everything comes from env so the public repo stays generic.
//
// @Summary Operator details and privacy-policy facts
// @ID getLegal
// @Tags System
// @Produce json
// @Success 200 {object} Legal "Legal information."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/legal [get]
func legal(cfg *config.Config) http.HandlerFunc {
	stun := cfg.WebRTCSTUNURLs
	if stun == nil {
		stun = []string{}
	}
	body := Legal{
		OperatorName:       cfg.LegalOperatorName,
		OperatorEmail:      cfg.LegalOperatorEmail,
		OperatorCountry:    cfg.LegalOperatorCountry,
		ProjectNotice:      cfg.LegalProjectNotice,
		MediaRetentionDays: cfg.MediaRetentionDays,
		SessionExpiryDays:  (cfg.SessionExpiryHours + 23) / 24,
		STUNServers:        stun,
		TURNServers:        nonNil(cfg.WebRTCTURNURLs),
		UpdateCheck:        cfg.UpdateCheck,
		LegalVersion:       LegalVersion,
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
//
// @Summary ICE servers for voice
// @Description STUN servers and, when configured, a TURN relay with fresh time-limited credentials. The list is empty when neither is configured.
// @ID getWebrtcConfig
// @Tags Realtime
// @Produce json
// @Security cookieAuth
// @Success 200 {object} ICEConfig "ICE configuration."
// @Header 200 {string} Cache-Control "no-store"
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/webrtc/config [get]
func webrtcConfig(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ice := []IceServer{}
		if len(cfg.WebRTCSTUNURLs) > 0 {
			ice = append(ice, IceServer{URLs: cfg.WebRTCSTUNURLs})
		}
		if len(cfg.WebRTCTURNURLs) > 0 {
			user, cred := turnCredentials(cfg.WebRTCTURNSecret, auth.UserFrom(r.Context()).ID, time.Now(), turnCredentialTTL)
			ice = append(ice, IceServer{URLs: cfg.WebRTCTURNURLs, Username: user, Credential: cred})
		}
		w.Header().Set("Cache-Control", "no-store")
		httpx.WriteJSON(w, http.StatusOK, ICEConfig{ICEServers: ice})
	}
}

// openAPISpec serves the OpenAPI description of the REST API.
//
// @Summary This API description
// @Description Returns this OpenAPI 3.1 document to signed-in members. Only mounted when API_DOCS_ENABLED=true.
// @ID getOpenAPI
// @Tags System
// @Produce json
// @Security cookieAuth
// @Success 200 {object} map[string]any "The OpenAPI document."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Router /api/openapi.json [get]
func openAPISpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(api.Spec)
}

// apiDocs serves the API reference page (Swagger UI) to signed-in members;
// anyone else is sent to the app to sign in.
//
// @Summary API reference
// @Description HTML page with Swagger UI over /api/openapi.json. Without a session it redirects to the app (/) to sign in. Only mounted when API_DOCS_ENABLED=true.
// @ID getAPIDocs
// @Tags System
// @Produce html
// @Success 200 {string} string "The API reference page."
// @Success 302 "No session: redirect to /."
// @Router /api/docs [get]
func apiDocs(sessions *auth.Sessions) http.HandlerFunc {
	page := web.APIDocsPage()
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := sessions.Authenticate(r); err != nil {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		if page == nil {
			httpx.WriteError(w, httpx.ErrUnavailable("web app not built: run `make web`"))
			return
		}
		// A page under /api: it loads the app's scripts and styles, so it gets
		// the app's policy instead of the API's default-src 'none'.
		w.Header().Set("Content-Security-Policy", httpx.SPAContentSecurityPolicy)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(page)
	}
}
