// Package config loads all runtime settings from environment variables (or a
// local .env during development). Real values never live in the repository.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	// LogLevel overrides the log level: "debug", "info", "warn" or "error"
	// (empty: info in production, debug otherwise).
	LogLevel             string
	Port                 string
	BindAddr             string
	AppEnv               string
	PublicURL            string
	AllowedOrigins       []string
	TrustedProxies       []string
	JWTSecret            string
	SessionExpiryHours   int
	AdminUsername        string
	AdminInitialPassword string
	DatabaseURL          string
	S3Endpoint           string
	S3Region             string
	S3Bucket             string
	S3AccessKey          string
	S3SecretKey          string
	S3ForcePathStyle     bool
	MediaRetentionDays   int
	MaxUploadMB          int
	// TURN relay for clients behind restrictive NATs (coturn with
	// use-auth-secret / static-auth-secret = WebRTCTURNSecret).
	WebRTCTURNURLs   []string
	WebRTCTURNSecret string
	// MetricsToken enables GET /api/metrics for Prometheus (Bearer token);
	// empty keeps the endpoint off.
	MetricsToken string
	// LinkPreviews lets the server fetch public web pages for link cards.
	LinkPreviews bool
	// APIDocs serves the API reference (/api/docs, /api/openapi.json) to
	// signed-in members. Off by default.
	APIDocs bool
	// UpdateCheck lets the server ask GitHub every 30 minutes for the latest
	// release (admin System tab). false = no outbound request at all.
	UpdateCheck bool
	// UpdaterURL and UpdaterToken reach the optional updater sidecar
	// (compose profile "autoupdate"). Self-update from the admin UI is only
	// offered when the token is set. The app itself never touches Docker.
	UpdaterURL   string
	UpdaterToken string
	// AppImage is the image reference the app container runs (MNEMA_IMAGE),
	// used to tell admins whether a re-pull can reach the latest release.
	AppImage         string
	WebRTCUDPPortMin uint16
	WebRTCUDPPortMax uint16
	// WebRTCUDPMuxPort optionally shares one port across media peers (0 = off).
	WebRTCUDPMuxPort uint16
	// WebRTCMaxRoomPeers caps the members of one voice room (0 = no limit).
	WebRTCMaxRoomPeers int
	// WebRTCAnnounce lists the IPs or host names announced to browsers for
	// media (WEBRTC_NAT_1TO1_IP, comma-separated).
	WebRTCAnnounce       []string
	WebRTCSTUNURLs       []string
	LegalOperatorName    string
	LegalOperatorEmail   string
	LegalOperatorCountry string
	LegalProjectNotice   string
}

// Placeholder values shipped in .env.example (and older versions of it). They
// are public, so they must never be accepted as real secrets. Any value
// starting with "replace_with_" counts as a placeholder as well.
var examplePlaceholders = map[string]bool{
	"replace_with_a_secure_random_string_in_production": true,
	"change_this_password_immediately":                  true,
	"replace_with_access_key":                           true,
	"replace_with_secret_key":                           true,
	"replace_with_a_random_database_password":           true,
}

// isPlaceholder reports whether v is one of the public example values.
func isPlaceholder(v string) bool {
	return examplePlaceholders[v] || strings.HasPrefix(strings.ToLower(v), "replace_with_")
}

// defaultTrustedProxies is loopback only: a reverse proxy on the same host.
// A proxy in a Docker network or elsewhere in the LAN must be listed
// explicitly in TRUSTED_PROXY_CIDRS, otherwise any host in those private
// ranges could spoof X-Forwarded-For.
var defaultTrustedProxies = "127.0.0.0/8,::1/128"

// maxSessionExpiryHours caps SESSION_EXPIRY_HOURS at one year.
const maxSessionExpiryHours = 8760

// validLogLevels are the accepted LOG_LEVEL values ("" = by environment).
var validLogLevels = map[string]bool{"": true, "debug": true, "info": true, "warn": true, "error": true}

func Load() (*Config, error) {
	// A local .env is optional; in production the variables come from the environment.
	_ = godotenv.Load()
	return FromEnv(os.LookupEnv)
}

// FromEnv builds a Config from a lookup function, which keeps it testable.
func FromEnv(lookup func(string) (string, bool)) (*Config, error) {
	get := func(key, def string) string {
		if v, ok := lookup(key); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
		return def
	}
	getInt := func(key string, def int) (int, error) {
		raw := get(key, strconv.Itoa(def))
		v, err := strconv.Atoi(raw)
		if err != nil {
			return 0, fmt.Errorf("%s must be an integer, got %q", key, raw)
		}
		return v, nil
	}
	getBool := func(key string, def bool) (bool, error) {
		raw := get(key, strconv.FormatBool(def))
		v, err := strconv.ParseBool(raw)
		if err != nil {
			return false, fmt.Errorf("%s must be true or false, got %q", key, raw)
		}
		return v, nil
	}
	getPort := func(key string, def uint16) (uint16, error) {
		raw := get(key, strconv.Itoa(int(def)))
		v, err := strconv.ParseUint(raw, 10, 16)
		if err != nil {
			return 0, fmt.Errorf("%s must be a port number (1-65535), got %q", key, raw)
		}
		return uint16(v), nil
	}

	expiryHours, err := getInt("SESSION_EXPIRY_HOURS", 720)
	if err != nil {
		return nil, err
	}
	// 0 (default) disables automatic media pruning entirely.
	retentionDays, err := getInt("MEDIA_RETENTION_DAYS", 0)
	if err != nil {
		return nil, err
	}
	maxUpload, err := getInt("MAX_UPLOAD_SIZE_MB", 50)
	if err != nil {
		return nil, err
	}
	portMin, err := getPort("WEBRTC_UDP_PORT_MIN", 50000)
	if err != nil {
		return nil, err
	}
	portMax, err := getPort("WEBRTC_UDP_PORT_MAX", 50050)
	if err != nil {
		return nil, err
	}
	muxPort, err := getPort("WEBRTC_UDP_MUX_PORT", 0)
	if err != nil {
		return nil, err
	}
	if muxPort != 0 && (muxPort < portMin || muxPort > portMax) {
		return nil, fmt.Errorf("WEBRTC_UDP_MUX_PORT must be 0 or inside WEBRTC_UDP_PORT_MIN/MAX")
	}
	maxRoomPeers, err := getInt("WEBRTC_MAX_ROOM_PEERS", 0)
	if err != nil {
		return nil, err
	}
	if maxRoomPeers < 0 {
		return nil, fmt.Errorf("WEBRTC_MAX_ROOM_PEERS must be 0 (no limit) or positive, got %d", maxRoomPeers)
	}
	httpPort, err := getPort("PORT", 8080)
	if err != nil {
		return nil, err
	}
	forcePathStyle, err := getBool("S3_FORCE_PATH_STYLE", true)
	if err != nil {
		return nil, err
	}
	linkPreviews, err := getBool("LINK_PREVIEWS_ENABLED", true)
	if err != nil {
		return nil, err
	}
	apiDocs, err := getBool("API_DOCS_ENABLED", false)
	if err != nil {
		return nil, err
	}
	updateCheck, err := getBool("UPDATE_CHECK_ENABLED", true)
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		Port:                 strconv.Itoa(int(httpPort)),
		BindAddr:             get("BIND_ADDR", "0.0.0.0"),
		AppEnv:               strings.ToLower(get("APP_ENV", "development")),
		LogLevel:             strings.ToLower(get("LOG_LEVEL", "")),
		PublicURL:            normalizeURL(get("PUBLIC_URL", "http://localhost:8080")),
		TrustedProxies:       SplitList(get("TRUSTED_PROXY_CIDRS", defaultTrustedProxies)),
		JWTSecret:            get("JWT_SECRET", ""),
		SessionExpiryHours:   expiryHours,
		AdminUsername:        get("ADMIN_USERNAME", "Herzog"),
		AdminInitialPassword: get("ADMIN_INITIAL_PASSWORD", ""),
		DatabaseURL:          get("DATABASE_URL", ""),
		S3Endpoint:           get("S3_ENDPOINT", "http://localhost:8333"),
		S3Region:             get("S3_REGION", "us-east-1"),
		S3Bucket:             get("S3_BUCKET", "mnema-media"),
		S3AccessKey:          get("S3_ACCESS_KEY", ""),
		S3SecretKey:          get("S3_SECRET_KEY", ""),
		S3ForcePathStyle:     forcePathStyle,
		MediaRetentionDays:   retentionDays,
		MaxUploadMB:          maxUpload,
		LinkPreviews:         linkPreviews,
		APIDocs:              apiDocs,
		UpdateCheck:          updateCheck,
		UpdaterURL:           get("UPDATER_URL", ""),
		UpdaterToken:         get("UPDATER_TOKEN", ""),
		AppImage:             get("MNEMA_IMAGE", ""),
		WebRTCTURNURLs:       SplitList(get("WEBRTC_TURN_URLS", "")),
		WebRTCTURNSecret:     get("WEBRTC_TURN_SECRET", ""),
		WebRTCUDPMuxPort:     muxPort,
		WebRTCMaxRoomPeers:   maxRoomPeers,
		MetricsToken:         get("METRICS_TOKEN", ""),
		WebRTCAnnounce:       SplitList(get("WEBRTC_NAT_1TO1_IP", "")),
		WebRTCSTUNURLs:       SplitList(get("WEBRTC_STUN_URLS", "")),
		LegalOperatorName:    get("LEGAL_OPERATOR_NAME", "Community Operator"),
		LegalOperatorEmail:   get("LEGAL_OPERATOR_EMAIL", "admin@example.com"),
		LegalOperatorCountry: get("LEGAL_OPERATOR_COUNTRY", "Deutschland"),
		LegalProjectNotice:   get("LEGAL_PROJECT_NOTICE", "Privates, nicht-kommerzielles Projekt"),
	}
	for _, o := range SplitList(get("CORS_ALLOWED_ORIGINS", cfg.PublicURL)) {
		cfg.AllowedOrigins = append(cfg.AllowedOrigins, normalizeURL(o))
	}

	if err := cfg.validate(portMin, portMax); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) validate(portMin, portMax uint16) error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	if isPlaceholder(c.AdminInitialPassword) {
		return fmt.Errorf("ADMIN_INITIAL_PASSWORD still uses the public example value; set your own or leave it empty to generate one")
	}
	u, err := url.Parse(c.PublicURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("PUBLIC_URL must be an absolute http(s) URL, got %q", c.PublicURL)
	}
	if portMin < 1 || portMin > portMax {
		return fmt.Errorf("invalid WebRTC UDP port range %d-%d", portMin, portMax)
	}
	c.WebRTCUDPPortMin, c.WebRTCUDPPortMax = portMin, portMax
	if c.Port == "0" {
		return fmt.Errorf("PORT must be a port number (1-65535), got 0")
	}
	if c.SessionExpiryHours < 1 || c.SessionExpiryHours > maxSessionExpiryHours {
		return fmt.Errorf("SESSION_EXPIRY_HOURS must be between 1 and %d", maxSessionExpiryHours)
	}
	if !validLogLevels[c.LogLevel] {
		return fmt.Errorf("LOG_LEVEL must be debug, info, warn or error (or empty), got %q", c.LogLevel)
	}
	if c.MediaRetentionDays < 0 {
		return fmt.Errorf("MEDIA_RETENTION_DAYS must be 0 (disabled) or a positive number of days")
	}
	if c.MaxUploadMB < 1 || c.MaxUploadMB > 2048 {
		return fmt.Errorf("MAX_UPLOAD_SIZE_MB must be between 1 and 2048")
	}
	for _, p := range c.TrustedProxies {
		if _, _, err := net.ParseCIDR(p); err != nil && net.ParseIP(p) == nil {
			return fmt.Errorf("TRUSTED_PROXY_CIDRS contains an invalid entry %q", p)
		}
	}

	switch c.AppEnv {
	case "prod":
		c.AppEnv = "production"
	case "dev":
		c.AppEnv = "development"
	case "production", "staging", "development", "test":
	default:
		return fmt.Errorf("APP_ENV must be production, staging, development or test, got %q", c.AppEnv)
	}

	if len(c.WebRTCTURNURLs) > 0 && len(c.WebRTCTURNSecret) < 16 {
		return fmt.Errorf("WEBRTC_TURN_SECRET must be at least 16 characters when WEBRTC_TURN_URLS is set")
	}

	if c.MetricsToken != "" && len(c.MetricsToken) < 24 {
		return fmt.Errorf("METRICS_TOKEN must be at least 24 characters when set")
	}

	if err := c.validateUpdater(); err != nil {
		return err
	}

	if c.IsProduction() {
		if len(c.JWTSecret) < 32 || isPlaceholder(c.JWTSecret) {
			return fmt.Errorf("JWT_SECRET must be set to a random string of at least 32 characters in production")
		}
		if c.S3AccessKey == "" || c.S3SecretKey == "" {
			return fmt.Errorf("S3_ACCESS_KEY and S3_SECRET_KEY are required in production")
		}
		if isPlaceholder(c.S3AccessKey) || isPlaceholder(c.S3SecretKey) {
			return fmt.Errorf("S3_ACCESS_KEY and S3_SECRET_KEY still use the public example values; set your own")
		}
		if u, err := url.Parse(c.DatabaseURL); err == nil && u.User != nil {
			if pw, ok := u.User.Password(); ok && isPlaceholder(pw) {
				return fmt.Errorf("DATABASE_URL still uses the public example password; set POSTGRES_PASSWORD to your own")
			}
		}
		if isPlaceholder(c.MetricsToken) || isPlaceholder(c.WebRTCTURNSecret) {
			return fmt.Errorf("METRICS_TOKEN or WEBRTC_TURN_SECRET still uses a public example value")
		}
	} else if c.JWTSecret == "" {
		// Development only: a per-process random secret (sessions reset on restart).
		c.JWTSecret = RandomHex(32)
		slog.Warn("JWT_SECRET not set, using a random secret for this process (development only)")
	}
	return nil
}

// DefaultUpdaterURL is the updater sidecar's address on the compose network.
const DefaultUpdaterURL = "http://mnema-updater:8080"

// minUpdaterTokenLen is the least UPDATER_TOKEN length: the token lets its
// holder restart the app with a freshly pulled image.
const minUpdaterTokenLen = 32

var updaterTokenPattern = regexp.MustCompile(`^[A-Za-z0-9._~+/=-]+$`)

// validateUpdater checks the self-update settings. Without UPDATER_TOKEN the
// feature is off and UPDATER_URL is ignored.
func (c *Config) validateUpdater() error {
	if c.UpdaterToken == "" {
		c.UpdaterURL = ""
		return nil
	}
	if isPlaceholder(c.UpdaterToken) {
		return fmt.Errorf("UPDATER_TOKEN still uses the public example value; generate one with: openssl rand -hex 32")
	}
	if len(c.UpdaterToken) < minUpdaterTokenLen || !updaterTokenPattern.MatchString(c.UpdaterToken) {
		return fmt.Errorf("UPDATER_TOKEN must be at least %d characters of letters, digits and . _ ~ + / = -", minUpdaterTokenLen)
	}
	if c.UpdaterURL == "" {
		c.UpdaterURL = DefaultUpdaterURL
	}
	u, err := url.Parse(c.UpdaterURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("UPDATER_URL must be an http(s) URL without credentials, query or fragment, got %q", c.UpdaterURL)
	}
	return nil
}

// SelfUpdateConfigured reports whether the updater sidecar may be called.
func (c *Config) SelfUpdateConfigured() bool { return c.UpdaterToken != "" && c.UpdaterURL != "" }

// WebRTCReachabilityWarning explains why members outside the server's own
// network will likely get no voice, or returns "". In production without
// WEBRTC_NAT_1TO1_IP the SFU announces the container's own (Docker bridge)
// address, which no browser elsewhere can reach, and without TURN there is
// no fallback either.
func (c *Config) WebRTCReachabilityWarning() string {
	if !c.IsProduction() || len(c.WebRTCAnnounce) > 0 || len(c.WebRTCTURNURLs) > 0 {
		return ""
	}
	return "WEBRTC_NAT_1TO1_IP and WEBRTC_TURN_URLS are empty: browsers are told the container's own address, so voice and video will likely fail for members outside this host; set WEBRTC_NAT_1TO1_IP to the public (and LAN) address"
}

// IsProduction is the single definition of a production-like deployment:
// strict secret checks, HSTS, short panic stacks and info-level logs.
func (c *Config) IsProduction() bool { return c.AppEnv == "production" || c.AppEnv == "staging" }

// SecureCookies reports whether cookies must carry the Secure flag (HTTPS deployments).
func (c *Config) SecureCookies() bool { return strings.HasPrefix(c.PublicURL, "https://") }

// RandomHex returns a cryptographically random hex string of n bytes.
func RandomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return hex.EncodeToString(b)
}

// normalizeURL trims the URL and a trailing slash, lower-cases scheme and host
// and drops the default port (:443 for https, :80 for http), so it compares
// equal to the Origin header browsers send. Unparsable input is returned
// trimmed; validation reports it.
func normalizeURL(raw string) string {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return raw
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	if p := u.Port(); (u.Scheme == "https" && p == "443") || (u.Scheme == "http" && p == "80") {
		u.Host = strings.TrimSuffix(u.Host, ":"+p)
	}
	return strings.TrimRight(u.String(), "/")
}

// SplitList splits a comma-separated list, dropping empty entries.
func SplitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}
