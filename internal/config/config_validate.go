package config

import (
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"strings"

	"github.com/msch128/mnema-talk/internal/debugsrv"
)

// maxSessionExpiryHours caps SESSION_EXPIRY_HOURS at one year.
const maxSessionExpiryHours = 8760

// validLogLevels are the accepted LOG_LEVEL values ("" = by environment).
var validLogLevels = map[string]bool{"": true, "debug": true, "info": true, "warn": true, "error": true}

func (c *Config) validate(portMin, portMax uint16) error {
	if err := c.validateBasics(portMin, portMax); err != nil {
		return err
	}
	if err := c.normalizeAppEnv(); err != nil {
		return err
	}
	if err := c.validateOptionalSecrets(); err != nil {
		return err
	}
	if err := c.validateUpdater(); err != nil {
		return err
	}
	if c.IsProduction() {
		if err := c.refuseBroadTrustedProxies(); err != nil {
			return err
		}
		return c.validateProductionSecrets()
	}
	if c.JWTSecret == "" {
		// Development only: a per-process random secret (sessions reset on restart).
		c.JWTSecret = RandomHex(32)
		slog.Warn("JWT_SECRET not set, using a random secret for this process (development only)")
	}
	return nil
}

// validateOptionalSecrets checks secrets that are optional until the feature
// they unlock is turned on.
func (c *Config) validateOptionalSecrets() error {
	if len(c.WebRTCTURNURLs) > 0 && len(c.WebRTCTURNSecret) < 16 {
		return fmt.Errorf("WEBRTC_TURN_SECRET must be at least 16 characters when WEBRTC_TURN_URLS is set")
	}
	if c.MetricsToken != "" && len(c.MetricsToken) < 24 {
		return fmt.Errorf("METRICS_TOKEN must be at least 24 characters when set")
	}
	if c.PprofAddr != "" {
		if err := debugsrv.CheckAddr(c.PprofAddr); err != nil {
			return fmt.Errorf("PPROF_ADDR %q %w", c.PprofAddr, err)
		}
	}
	return c.validateSMTP()
}

// validateBasics checks the settings every environment needs, stores the
// WebRTC UDP port range and then checks the limits.
func (c *Config) validateBasics(portMin, portMax uint16) error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	if err := c.resolveBindAddr(); err != nil {
		return err
	}
	if isPlaceholder(c.AdminInitialPassword) {
		return fmt.Errorf("ADMIN_INITIAL_PASSWORD still uses the public example value; set your own or leave it empty to generate one")
	}
	if _, ok := parseAbsoluteHTTP(c.PublicURL); !ok {
		return fmt.Errorf("PUBLIC_URL must be an absolute http(s) URL, got %q", c.PublicURL)
	}
	if portMin < 1 || portMin > portMax {
		return fmt.Errorf("invalid WebRTC UDP port range %d-%d", portMin, portMax)
	}
	c.WebRTCUDPPortMin, c.WebRTCUDPPortMax = portMin, portMax
	if err := c.validateBuiltinTURN(); err != nil {
		return err
	}
	if c.Port == "0" {
		return fmt.Errorf("PORT must be a port number (1-65535), got 0")
	}
	return c.validateLimits()
}

// parseAbsoluteHTTP parses raw as an absolute http or https URL.
func parseAbsoluteHTTP(raw string) (*url.URL, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, false
	}
	return u, true
}

// validateLimits checks the numeric and list settings.
func (c *Config) validateLimits() error {
	if c.SessionExpiryHours < 1 || c.SessionExpiryHours > maxSessionExpiryHours {
		return fmt.Errorf("SESSION_EXPIRY_HOURS must be between 1 and %d", maxSessionExpiryHours)
	}
	if !validLogLevels[c.LogLevel] {
		return fmt.Errorf("LOG_LEVEL must be debug, info, warn or error (or empty), got %q", c.LogLevel)
	}
	if err := c.validateRetention(); err != nil {
		return err
	}
	if c.EmojiMax < 0 || c.EmojiMax > 2000 {
		return fmt.Errorf("EMOJI_MAX must be between 0 and 2000")
	}
	if c.MaxUploadMB < 1 || c.MaxUploadMB > 2048 {
		return fmt.Errorf("MAX_UPLOAD_SIZE_MB must be between 1 and 2048")
	}
	if c.UploadQuotaMBPerDay < 0 || c.MediaMaxTotalMB < 0 || c.MediaMinFreeMB < 0 {
		return fmt.Errorf("UPLOAD_QUOTA_MB_PER_DAY, MEDIA_MAX_TOTAL_MB and MEDIA_MIN_FREE_MB must be 0 (off) or positive")
	}
	if c.InitialLanguage != "" && c.InitialLanguage != "en" && c.InitialLanguage != "de" {
		return fmt.Errorf("INITIAL_LANGUAGE must be en or de, got %q", c.InitialLanguage)
	}
	if err := c.validatePush(); err != nil {
		return err
	}
	if err := validateAllowedOrigins(c.AllowedOrigins); err != nil {
		return err
	}
	if err := validateLANIPs(c.WebRTCLANIPs); err != nil {
		return err
	}
	return validateTrustedProxies(c.TrustedProxies)
}

// validateLANIPs requires WEBRTC_LAN_IP entries to be IPv4 addresses a
// neighbour on the network could reach: not loopback, unspecified or multicast.
func validateLANIPs(ips []string) error {
	for _, e := range ips {
		a, err := netip.ParseAddr(e)
		if err != nil || !a.Is4() || !a.IsGlobalUnicast() {
			return fmt.Errorf("WEBRTC_LAN_IP: %q is not the IPv4 address of this host on your network, e.g. 192.168.1.20", e)
		}
	}
	return nil
}

// validateRetention checks the retention periods.
func (c *Config) validateRetention() error {
	if c.AuditRetentionDays < 0 {
		return fmt.Errorf("AUDIT_RETENTION_DAYS must be 0 (keep forever) or a positive number of days")
	}
	if c.EditHistoryRetentionDays < 0 {
		return fmt.Errorf("EDIT_HISTORY_RETENTION_DAYS must be 0 (keep up to 50 versions per message) or a positive number of days")
	}
	if c.MediaRetentionDays < 0 {
		return fmt.Errorf("MEDIA_RETENTION_DAYS must be 0 (disabled) or a positive number of days")
	}
	return nil
}

// validateAllowedOrigins requires every CORS_ALLOWED_ORIGINS entry to be a
// bare http(s) origin. Anything else never equals a browser Origin header, so
// cross-origin and WebSocket checks would fail without a hint at startup.
func validateAllowedOrigins(origins []string) error {
	for _, o := range origins {
		u, ok := parseAbsoluteHTTP(o)
		if !ok || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			return fmt.Errorf("CORS_ALLOWED_ORIGINS entries must be origins like https://chat.example.com (scheme and host, no path), got %q", o)
		}
	}
	return nil
}

// validatePush checks the Web Push settings.
func (c *Config) validatePush() error {
	if c.PushPreviews != "user" && c.PushPreviews != "off" {
		return fmt.Errorf("PUSH_PREVIEWS must be user or off, got %q", c.PushPreviews)
	}
	subject := c.PushVAPIDSubject
	if subject == "" {
		return nil
	}
	if u, ok := parseAbsoluteHTTP(subject); ok && u.Scheme == "https" {
		return nil
	}
	if addr, ok := strings.CutPrefix(subject, "mailto:"); ok && strings.Contains(addr, "@") {
		return nil
	}
	return fmt.Errorf("PUSH_VAPID_SUBJECT must be an https:// URL or a mailto: address, got %q", subject)
}

func validateTrustedProxies(proxies []string) error {
	for _, p := range proxies {
		if !validProxyEntry(p) {
			return fmt.Errorf("TRUSTED_PROXY_CIDRS contains an invalid entry %q", p)
		}
	}
	return nil
}

// validProxyEntry reports whether p is a CIDR range or a single IP address.
func validProxyEntry(p string) bool {
	if _, _, err := net.ParseCIDR(p); err == nil {
		return true
	}
	return net.ParseIP(p) != nil
}

// normalizeAppEnv expands the short APP_ENV names and rejects unknown ones.
func (c *Config) normalizeAppEnv() error {
	switch c.AppEnv {
	case "prod":
		c.AppEnv = "production"
	case "dev":
		c.AppEnv = "development"
	case "production", "staging", "development", "test":
	default:
		return fmt.Errorf("APP_ENV must be production, staging, development or test, got %q", c.AppEnv)
	}
	return nil
}

// validateProductionSecrets rejects missing, short and public example
// secrets in production-like deployments.
func (c *Config) validateProductionSecrets() error {
	if len(c.JWTSecret) < 32 || isPlaceholder(c.JWTSecret) {
		return fmt.Errorf("JWT_SECRET must be set to a random string of at least 32 characters in production")
	}
	if err := c.validateProductionS3(); err != nil {
		return err
	}
	if err := c.validateProductionDatabase(); err != nil {
		return err
	}
	if isPlaceholder(c.MetricsToken) || isPlaceholder(c.WebRTCTURNSecret) {
		return fmt.Errorf("METRICS_TOKEN or WEBRTC_TURN_SECRET still uses a public example value")
	}
	return nil
}

func (c *Config) validateProductionS3() error {
	if c.S3AccessKey == "" || c.S3SecretKey == "" {
		return fmt.Errorf("S3_ACCESS_KEY and S3_SECRET_KEY are required in production")
	}
	if isPlaceholder(c.S3AccessKey) || isPlaceholder(c.S3SecretKey) {
		return fmt.Errorf("S3_ACCESS_KEY and S3_SECRET_KEY still use the public example values; set your own")
	}
	return nil
}

// validateProductionDatabase rejects the public example password when the
// URL names one. An unparsable URL or a URL without a user is left for the
// database driver.
func (c *Config) validateProductionDatabase() error {
	u, err := url.Parse(c.DatabaseURL)
	if err != nil || u.User == nil {
		return nil
	}
	pw, ok := u.User.Password()
	if ok && isPlaceholder(pw) {
		return fmt.Errorf("DATABASE_URL still uses the public example password; set POSTGRES_PASSWORD to your own")
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
	if !validUpdaterURL(c.UpdaterURL) {
		return fmt.Errorf("UPDATER_URL must be an http(s) URL without credentials, query or fragment, got %q", c.UpdaterURL)
	}
	return nil
}

// validUpdaterURL reports whether raw is an http(s) URL with a host and no
// credentials, query or fragment.
func validUpdaterURL(raw string) bool {
	u, ok := parseAbsoluteHTTP(raw)
	if !ok {
		return false
	}
	if u.User != nil {
		return false
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	return true
}
