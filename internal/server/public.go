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
	"github.com/msch128/mnema-talk/internal/turnrelay"
)

// turnCredentialTTL is how long a TURN credential handed to a client is valid.
const turnCredentialTTL = 12 * time.Hour

// LegalVersion changes whenever recipients, retention or rights in the
// privacy policy change.
const LegalVersion = "1.6"

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
	UpdateCheck bool `json:"update_check"`
	// AddressDetection is true when the server may ask public STUN servers
	// for its own address at startup (WEBRTC_AUTO_ANNOUNCE); ClockCheck is
	// true when it compares its clock with NTP servers (CLOCK_CHECK_ENABLED).
	// Both reveal only the server's IP address.
	AddressDetection bool   `json:"address_detection"`
	ClockCheck       bool   `json:"clock_check"`
	LegalVersion     string `json:"legal_version"`
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
// @Description Public, no session needed. The operator named by the environment, the retention and session lifetimes, the STUN and TURN servers in use and whether the release check is on: the facts the in-app privacy policy states.
// @ID getLegal
// @Tags System
// @Produce json
// @Success 200 {object} Legal "Legal information."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/legal [get]
func LegalHandler(cfg *config.Config) http.HandlerFunc {
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
		TURNServers:        httpx.NonNil(cfg.WebRTCTURNURLs),
		UpdateCheck:        cfg.UpdateCheck,
		AddressDetection:   cfg.DetectsPublicAddress(),
		ClockCheck:         cfg.ClockCheck,
		LegalVersion:       LegalVersion,
	}
	return func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, body)
	}
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
// WEBRTC_TURN_URLS is set, and the built-in TURN relay with credentials of its
// own while it runs and the member may join voice. Without any the list is
// empty.
//
// @Summary ICE servers for voice
// @Description STUN servers and, when configured, TURN relays with fresh time-limited credentials: the external one named by WEBRTC_TURN_URLS and the one built into the server while it runs (members who may join voice only). The list is empty when none is configured.
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
func webrtcConfig(cfg *config.Config, relay *turnrelay.Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ice := []IceServer{}
		if len(cfg.WebRTCSTUNURLs) > 0 {
			ice = append(ice, IceServer{URLs: cfg.WebRTCSTUNURLs})
		}
		// The route is behind RequireUser; without a member there is no
		// credential to mint.
		if member := auth.UserFrom(r.Context()); member != nil {
			if len(cfg.WebRTCTURNURLs) > 0 {
				user, cred := turnCredentials(cfg.WebRTCTURNSecret, member.ID, time.Now(), turnCredentialTTL)
				ice = append(ice, IceServer{URLs: cfg.WebRTCTURNURLs, Username: user, Credential: cred})
			}
			if s := builtinTURN(cfg, relay, member.ID); s != nil {
				ice = append(ice, *s)
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		httpx.WriteJSON(w, http.StatusOK, ICEConfig{ICEServers: ice})
	}
}

// builtinTURN is the ICE entry of the TURN relay built into the server, or nil
// when it is not running, listens nowhere reachable by name, or the member may
// not join voice: credentials are minted only for members who could use them.
func builtinTURN(cfg *config.Config, relay *turnrelay.Server, userID uuid.UUID) *IceServer {
	if relay == nil || !relay.MayRelay(userID) {
		return nil
	}
	host := cfg.TURNPublicHost()
	if host == "" {
		return nil
	}
	urls := relay.URLs(host)
	if len(urls) == 0 {
		return nil
	}
	user, cred, _ := relay.Credentials(userID)
	return &IceServer{URLs: urls, Username: user, Credential: cred}
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
	if _, err := w.Write(api.Spec); err != nil {
		return
	}
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
func apiDocs(sessions *auth.Sessions, page []byte) http.HandlerFunc {
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
		if _, err := w.Write(page); err != nil {
			return
		}
	}
}
