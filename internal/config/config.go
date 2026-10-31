// Package config loads all runtime settings from environment variables (or a
// local .env during development). Real values never live in the repository.
package config

import (
	"net/netip"
	"net/url"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	// LogLevel overrides the log level: "debug", "info", "warn" or "error"
	// (empty: info in production, debug otherwise).
	LogLevel           string
	Port               string
	BindAddr           string
	AppEnv             string
	PublicURL          string
	AllowedOrigins     []string
	TrustedProxies     []string
	JWTSecret          string
	SessionExpiryHours int
	AdminUsername      string
	// InitialLanguage (INITIAL_LANGUAGE, en or de) names the default categories and channels of a brand-new installation.
	InitialLanguage      string
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
	// EditHistoryRetentionDays > 0 deletes kept earlier versions of edited
	// messages that were replaced longer ago; 0 (default) keeps them.
	EditHistoryRetentionDays int
	MaxUploadMB              int
	// EmojiMax is how many custom emoji the instance holds (0: none can be added).
	EmojiMax int
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
	// WebRTCBuiltinTURN switches the TURN relay built into the server
	// (WEBRTC_BUILTIN_TURN): "auto" (default) runs it only while no external
	// TURN is configured (WEBRTC_TURN_URLS empty), "on" always, "off" never.
	// WebRTCBuiltinTURNPort is the UDP and TCP port it listens on.
	// WebRTCBuiltinTURNHost (WEBRTC_BUILTIN_TURN_HOST) overrides the host name
	// members are told to reach it at; empty = the host of PUBLIC_URL.
	WebRTCBuiltinTURN     string
	WebRTCBuiltinTURNPort uint16
	WebRTCBuiltinTURNHost string
	// MetricsToken enables GET /api/metrics for Prometheus (Bearer token);
	// empty keeps the endpoint off.
	MetricsToken string
	// PprofAddr (PPROF_ADDR) serves Go's pprof profiles on that loopback
	// host:port for diagnosing load; empty (the default) serves nothing.
	PprofAddr string
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
	WebRTCAnnounce []string
	// WebRTCLANIPs (WEBRTC_LAN_IP, comma-separated IPv4 addresses) are the
	// host's LAN addresses announced next to the automatically found public
	// address, so members on the same network reach voice without NAT loopback.
	WebRTCLANIPs []string
	// WebRTCAutoAnnounce (WEBRTC_AUTO_ANNOUNCE, default true) lets a
	// production server find its public IPv4 address with a STUN request when
	// WEBRTC_NAT_1TO1_IP is empty. WebRTCDetectSTUN lists the STUN servers
	// asked (WEBRTC_DETECT_STUN_URLS, default: public Google and Cloudflare).
	WebRTCAutoAnnounce bool
	WebRTCDetectSTUN   []string
	WebRTCSTUNURLs     []string
	// ClockCheck (CLOCK_CHECK_ENABLED, default true) compares the server clock
	// with an NTP server for the setup checks; false = no outbound request.
	ClockCheck           bool
	LegalOperatorName    string
	LegalOperatorEmail   string
	LegalOperatorCountry string
	LegalProjectNotice   string
	// SMTP is the optional outgoing mail setup (SMTP_* variables).
	SMTP SMTPConfig
}

// PublicHost is the host name or IP address of PUBLIC_URL, without port.
func (c *Config) PublicHost() string {
	u, err := url.Parse(c.PublicURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// PublicIPv4 returns the address when PUBLIC_URL names a plain IPv4 address
// (a LAN install opened by IP), which is then also the address voice must be
// announced on.
func (c *Config) PublicIPv4() (netip.Addr, bool) {
	a, err := netip.ParseAddr(c.PublicHost())
	if err != nil || !a.Is4() || a.IsLoopback() || a.IsUnspecified() {
		return netip.Addr{}, false
	}
	return a, true
}

// DetectsPublicAddress reports whether the server may ask public STUN servers
// for its own address: production, no WEBRTC_NAT_1TO1_IP and detection on.
func (c *Config) DetectsPublicAddress() bool {
	return c.IsProduction() && len(c.WebRTCAnnounce) == 0 && c.WebRTCAutoAnnounce
}

// PublicURLIsLocal reports whether PUBLIC_URL names this machine only
// (localhost or a loopback address), which no other device can open.
func (c *Config) PublicURLIsLocal() bool { return IsLoopbackAddr(c.PublicHost()) }

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
//
// The built-in TURN relay reaches the SFU through the server's own address, so
// while it runs the announced address is no longer the only way in.
func (c *Config) WebRTCReachabilityWarning() string {
	return c.WebRTCReachabilityWarningWith(c.BuiltinTURNEnabled())
}

// WebRTCReachabilityWarningWith is WebRTCReachabilityWarning for a known state
// of the built-in TURN relay: builtinRunning is whether it is actually
// listening, not only whether the settings ask for it.
func (c *Config) WebRTCReachabilityWarningWith(builtinRunning bool) string {
	if !c.IsProduction() || len(c.WebRTCAnnounce) > 0 || len(c.WebRTCTURNURLs) > 0 || builtinRunning {
		return ""
	}
	return "WEBRTC_NAT_1TO1_IP and WEBRTC_TURN_URLS are empty: browsers are told the container's own address, so voice and video will likely fail for members outside this host; set WEBRTC_NAT_1TO1_IP to the public (and LAN) address"
}

// WebRTCRelayNotice says what members whose network blocks UDP can expect, or
// returns "" when a TURN relay offers them a TCP or TLS path. Direct media
// needs UDP to the announced address; only TURN over TCP (turn:...?transport=tcp)
// or TLS (turns:...) crosses a network that drops UDP.
//
// It looks at WEBRTC_TURN_URLS only; WebRTCRelayNoticeWith knows about the
// relay built into the server.
func (c *Config) WebRTCRelayNotice() string { return c.WebRTCRelayNoticeWith(false) }

// WebRTCRelayNoticeWith is WebRTCRelayNotice when the built-in TURN relay is
// listening over TCP (builtinTCP): that is a TCP path of its own.
func (c *Config) WebRTCRelayNoticeWith(builtinTCP bool) string {
	if builtinTCP {
		return ""
	}
	if len(c.WebRTCTURNURLs) == 0 {
		return "WEBRTC_TURN_URLS is empty: members whose network blocks UDP (strict firewalls, some VPNs, Opera) cannot join voice; see the TURN section of doc/operations.md"
	}
	for _, u := range c.WebRTCTURNURLs {
		u = strings.ToLower(u)
		if strings.HasPrefix(u, "turns:") || strings.Contains(u, "transport=tcp") {
			return ""
		}
	}
	return "WEBRTC_TURN_URLS has no TCP or TLS URL (turn:...?transport=tcp or turns:...): members whose network blocks UDP still cannot join voice"
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
