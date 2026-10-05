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
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
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
	// LinkPreviews lets the server fetch public web pages for link cards.
	LinkPreviews         bool
	WebRTCUDPPortMin     uint16
	WebRTCUDPPortMax     uint16
	WebRTCNAT1to1IP      string
	WebRTCSTUNURLs       []string
	LegalOperatorName    string
	LegalOperatorEmail   string
	LegalOperatorCountry string
	LegalProjectNotice   string
}

// Placeholder values shipped in .env.example. They are public, so they must
// never be accepted as real secrets.
var examplePlaceholders = map[string]bool{
	"replace_with_a_secure_random_string_in_production": true,
	"change_this_password_immediately":                  true,
}

// defaultTrustedProxies covers loopback and private networks, where a reverse
// proxy (Caddy) or the Docker bridge sits in front of the app.
var defaultTrustedProxies = "127.0.0.0/8,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,fc00::/7"

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

	cfg := &Config{
		Port:                 get("PORT", "8080"),
		BindAddr:             get("BIND_ADDR", "0.0.0.0"),
		AppEnv:               strings.ToLower(get("APP_ENV", "development")),
		PublicURL:            strings.TrimRight(get("PUBLIC_URL", "http://localhost:8080"), "/"),
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
		S3ForcePathStyle:     get("S3_FORCE_PATH_STYLE", "true") == "true",
		MediaRetentionDays:   retentionDays,
		MaxUploadMB:          maxUpload,
		LinkPreviews:         get("LINK_PREVIEWS_ENABLED", "true") == "true",
		WebRTCNAT1to1IP:      get("WEBRTC_NAT_1TO1_IP", ""),
		WebRTCSTUNURLs:       SplitList(get("WEBRTC_STUN_URLS", "")),
		LegalOperatorName:    get("LEGAL_OPERATOR_NAME", "Community Operator"),
		LegalOperatorEmail:   get("LEGAL_OPERATOR_EMAIL", "admin@example.com"),
		LegalOperatorCountry: get("LEGAL_OPERATOR_COUNTRY", "Deutschland"),
		LegalProjectNotice:   get("LEGAL_PROJECT_NOTICE", "Privates, nicht-kommerzielles Projekt"),
	}
	cfg.AllowedOrigins = SplitList(get("CORS_ALLOWED_ORIGINS", cfg.PublicURL))

	if err := cfg.validate(portMin, portMax); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) validate(portMin, portMax uint16) error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	if examplePlaceholders[c.AdminInitialPassword] {
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
	if c.SessionExpiryHours < 1 {
		return fmt.Errorf("SESSION_EXPIRY_HOURS must be at least 1")
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

	if c.IsProduction() {
		if len(c.JWTSecret) < 32 || examplePlaceholders[c.JWTSecret] {
			return fmt.Errorf("JWT_SECRET must be set to a random string of at least 32 characters in production")
		}
		if c.S3AccessKey == "" || c.S3SecretKey == "" {
			return fmt.Errorf("S3_ACCESS_KEY and S3_SECRET_KEY are required in production")
		}
	} else if c.JWTSecret == "" {
		// Development only: a per-process random secret (sessions reset on restart).
		c.JWTSecret = RandomHex(32)
		slog.Warn("JWT_SECRET not set, using a random secret for this process (development only)")
	}
	return nil
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
