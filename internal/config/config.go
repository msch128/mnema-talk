// Package config loads all runtime settings from environment variables (or a
// local .env during development). Real values never live in the repository.
package config

import (
	"net/netip"
	"os"
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
	// AuditRetentionDays > 0 deletes audit log entries older than that many
	// days; 0 (default) keeps them forever.
	AuditRetentionDays int
	MaxUploadMB        int
	// UploadQuotaMBPerDay is what one account may upload in a rolling 24
	// hours; 0 disables the budget. MediaMaxTotalMB caps all stored media
	// (0 = no cap). MediaMinFreeMB pauses uploads while the filesystem at
	// MediaDiskPath has less free space (needs MediaDiskPath).
	UploadQuotaMBPerDay int
	MediaMaxTotalMB     int
	MediaMinFreeMB      int
	MediaDiskPath       string
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
	// PushEnabled turns Web Push on (PUSH_ENABLED, default true). PushPreviews
	// is "user" (members choose whether a push carries the message text) or
	// "off" (never). PushVAPIDSubject is the contact in the VAPID header: an
	// https:// URL or a mailto: address; empty uses PUBLIC_URL when that is
	// https.
	PushEnabled      bool
	PushPreviews     string
	PushVAPIDSubject string
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

// InsecurePublicURLWarning explains the cost of a production-like deployment
// whose PUBLIC_URL is not https://, or returns "". Session cookies then lose
// Secure and the __Host- prefix while HSTS is still sent, so a network
// attacker on a first plain-HTTP visit can read or overwrite them. It is a
// warning, not a refusal: a LAN-only install over http stays possible.
func (c *Config) InsecurePublicURLWarning() string {
	if !c.IsProduction() || c.SecureCookies() {
		return ""
	}
	return "PUBLIC_URL is not https://: session cookies are sent without the Secure flag and the __Host- prefix, so anyone on the network path can read or replace them; set PUBLIC_URL to the https:// URL users open (behind a TLS-terminating reverse proxy) unless this is a trusted LAN"
}

// IsProduction is the single definition of a production-like deployment:
// strict secret checks, HSTS, short panic stacks and info-level logs.
func (c *Config) IsProduction() bool { return c.AppEnv == "production" || c.AppEnv == "staging" }

// SecureCookies reports whether cookies must carry the Secure flag (HTTPS deployments).
func (c *Config) SecureCookies() bool { return strings.HasPrefix(c.PublicURL, "https://") }
