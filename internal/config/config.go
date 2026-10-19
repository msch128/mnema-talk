// Package config loads all runtime settings from environment variables (or a
// local .env during development). Real values never live in the repository.
package config

import (
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
	"github.com/msch128/mnema-talk/internal/ssrf"
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
	// AuditRetentionDays > 0 deletes audit log entries older than that many
	// days; 0 (default) keeps them forever.
	AuditRetentionDays int
	MaxUploadMB        int
	// TURN relay for clients behind restrictive NATs (coturn with
	// use-auth-secret / static-auth-secret = WebRTCTURNSecret).
	WebRTCTURNURLs   []string
	WebRTCTURNSecret string
	// MetricsToken enables GET /api/metrics for Prometheus (Bearer token);
	// empty keeps the endpoint off.
	MetricsToken string
	// WebhookAllowPrivateTargets are local-network address ranges outgoing
	// webhooks may contact (WEBHOOK_ALLOW_PRIVATE_TARGETS, comma-separated
	// addresses and CIDR ranges). Empty (the default) allows public addresses
	// only, which is what keeps a member-supplied URL from reaching the
	// server's own network.
	WebhookAllowPrivateTargets []netip.Prefix
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
	// WebRTCUDPMuxPort is the one UDP port all media peers share (default
	// WEBRTC_UDP_PORT_MIN). 0 gives every peer its own port(s) from the range
	// instead, which caps how many people can be in calls at once.
	WebRTCUDPMuxPort uint16
	// WebRTCAnnounce lists the IPs or host names announced to browsers for
	// media (WEBRTC_NAT_1TO1_IP, comma-separated).
	WebRTCAnnounce       []string
	WebRTCSTUNURLs       []string
	LegalOperatorName    string
	LegalOperatorEmail   string
	LegalOperatorCountry string
	LegalProjectNotice   string
}

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
	auditRetention, err := getInt("AUDIT_RETENTION_DAYS", 0)
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
	muxPort, err := getPort("WEBRTC_UDP_MUX_PORT", portMin)
	if err != nil {
		return nil, err
	}
	if muxPort != 0 && (muxPort < portMin || muxPort > portMax) {
		return nil, fmt.Errorf("WEBRTC_UDP_MUX_PORT must be 0 or inside WEBRTC_UDP_PORT_MIN/MAX")
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
	webhookAllow, err := ssrf.ParseAllow(get("WEBHOOK_ALLOW_PRIVATE_TARGETS", ""))
	if err != nil {
		return nil, fmt.Errorf("WEBHOOK_ALLOW_PRIVATE_TARGETS: %w", err)
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
		Port:                       strconv.Itoa(int(httpPort)),
		BindAddr:                   get("BIND_ADDR", ""),
		AppEnv:                     strings.ToLower(get("APP_ENV", "development")),
		LogLevel:                   strings.ToLower(get("LOG_LEVEL", "")),
		PublicURL:                  normalizeURL(get("PUBLIC_URL", "http://localhost:8080")),
		TrustedProxies:             SplitList(get("TRUSTED_PROXY_CIDRS", defaultTrustedProxies)),
		JWTSecret:                  get("JWT_SECRET", ""),
		SessionExpiryHours:         expiryHours,
		AdminUsername:              get("ADMIN_USERNAME", "Herzog"),
		AdminInitialPassword:       get("ADMIN_INITIAL_PASSWORD", ""),
		DatabaseURL:                get("DATABASE_URL", ""),
		S3Endpoint:                 get("S3_ENDPOINT", "http://localhost:8333"),
		S3Region:                   get("S3_REGION", "us-east-1"),
		S3Bucket:                   get("S3_BUCKET", "mnema-media"),
		S3AccessKey:                get("S3_ACCESS_KEY", ""),
		S3SecretKey:                get("S3_SECRET_KEY", ""),
		S3ForcePathStyle:           forcePathStyle,
		MediaRetentionDays:         retentionDays,
		AuditRetentionDays:         auditRetention,
		MaxUploadMB:                maxUpload,
		LinkPreviews:               linkPreviews,
		WebhookAllowPrivateTargets: webhookAllow,
		APIDocs:                    apiDocs,
		UpdateCheck:                updateCheck,
		UpdaterURL:                 get("UPDATER_URL", ""),
		UpdaterToken:               get("UPDATER_TOKEN", ""),
		AppImage:                   get("MNEMA_IMAGE", ""),
		WebRTCTURNURLs:             SplitList(get("WEBRTC_TURN_URLS", "")),
		WebRTCTURNSecret:           get("WEBRTC_TURN_SECRET", ""),
		WebRTCUDPMuxPort:           muxPort,
		MetricsToken:               get("METRICS_TOKEN", ""),
		WebRTCAnnounce:             SplitList(get("WEBRTC_NAT_1TO1_IP", "")),
		WebRTCSTUNURLs:             SplitList(get("WEBRTC_STUN_URLS", "")),
		LegalOperatorName:          get("LEGAL_OPERATOR_NAME", "Community Operator"),
		LegalOperatorEmail:         get("LEGAL_OPERATOR_EMAIL", "admin@example.com"),
		LegalOperatorCountry:       get("LEGAL_OPERATOR_COUNTRY", "Deutschland"),
		LegalProjectNotice:         get("LEGAL_PROJECT_NOTICE", "Privates, nicht-kommerzielles Projekt"),
	}
	for _, o := range SplitList(get("CORS_ALLOWED_ORIGINS", cfg.PublicURL)) {
		cfg.AllowedOrigins = append(cfg.AllowedOrigins, normalizeURL(o))
	}

	if err := cfg.validate(portMin, portMax); err != nil {
		return nil, err
	}
	return cfg, nil
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
