package config

import (
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"regexp"
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
		return c.validateProductionSecrets()
	}
	if c.JWTSecret == "" {
		// Development only: a per-process random secret (sessions reset on restart).
		c.JWTSecret = RandomHex(32)
		slog.Warn("JWT_SECRET not set, using a random secret for this process (development only)")
	}
	return nil
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
	return c.validateLimits()
}

// validateLimits checks the numeric and list settings.
func (c *Config) validateLimits() error {
	if c.SessionExpiryHours < 1 || c.SessionExpiryHours > maxSessionExpiryHours {
		return fmt.Errorf("SESSION_EXPIRY_HOURS must be between 1 and %d", maxSessionExpiryHours)
	}
	if !validLogLevels[c.LogLevel] {
		return fmt.Errorf("LOG_LEVEL must be debug, info, warn or error (or empty), got %q", c.LogLevel)
	}
	if c.AuditRetentionDays < 0 {
		return fmt.Errorf("AUDIT_RETENTION_DAYS must be 0 (keep forever) or a positive number of days")
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
	return nil
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
