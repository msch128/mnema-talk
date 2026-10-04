package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
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

func Load() (*Config, error) {
	// Attempt to load .env file if present (ignored in production if not present)
	_ = godotenv.Load()

	expiryHours, _ := strconv.Atoi(getEnv("SESSION_EXPIRY_HOURS", "720"))
	retentionDays, _ := strconv.Atoi(getEnv("MEDIA_RETENTION_DAYS", "30"))

	portMin, _ := strconv.ParseUint(getEnv("WEBRTC_UDP_PORT_MIN", "50000"), 10, 16)
	portMax, _ := strconv.ParseUint(getEnv("WEBRTC_UDP_PORT_MAX", "50050"), 10, 16)

	forcePathStyle := getEnv("S3_FORCE_PATH_STYLE", "true") == "true"

	cfg := &Config{
		Port:                 getEnv("PORT", "8080"),
		BindAddr:             getEnv("BIND_ADDR", "0.0.0.0"),
		AppEnv:               getEnv("APP_ENV", "development"),
		PublicURL:            getEnv("PUBLIC_URL", "http://localhost:8080"),
		JWTSecret:            getEnv("JWT_SECRET", ""),
		SessionExpiryHours:   expiryHours,
		AdminUsername:        getEnv("ADMIN_USERNAME", "Herzog"),
		AdminInitialPassword: getEnv("ADMIN_INITIAL_PASSWORD", ""),
		DatabaseURL:          getEnv("DATABASE_URL", ""),
		S3Endpoint:           getEnv("S3_ENDPOINT", "http://localhost:8333"),
		S3Region:             getEnv("S3_REGION", "us-east-1"),
		S3Bucket:             getEnv("S3_BUCKET", "mnema-media"),
		S3AccessKey:          getEnv("S3_ACCESS_KEY", ""),
		S3SecretKey:          getEnv("S3_SECRET_KEY", ""),
		S3ForcePathStyle:     forcePathStyle,
		MediaRetentionDays:   retentionDays,
		WebRTCUDPPortMin:     uint16(portMin),
		WebRTCUDPPortMax:     uint16(portMax),
		WebRTCNAT1to1IP:      getEnv("WEBRTC_NAT_1TO1_IP", ""),
		WebRTCSTUNURLs:       splitList(getEnv("WEBRTC_STUN_URLS", "")),
		LegalOperatorName:    getEnv("LEGAL_OPERATOR_NAME", "Community Operator"),
		LegalOperatorEmail:   getEnv("LEGAL_OPERATOR_EMAIL", "admin@example.com"),
		LegalOperatorCountry: getEnv("LEGAL_OPERATOR_COUNTRY", "Deutschland"),
		LegalProjectNotice:   getEnv("LEGAL_PROJECT_NOTICE", "Privates, nicht-kommerzielles Projekt"),
	}

	if cfg.Port == "" {
		return nil, fmt.Errorf("PORT is required")
	}
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if examplePlaceholders[cfg.AdminInitialPassword] {
		return nil, fmt.Errorf("ADMIN_INITIAL_PASSWORD still uses the public example value; set your own or leave it empty to generate one")
	}

	if cfg.AppEnv == "production" {
		if len(cfg.JWTSecret) < 32 || examplePlaceholders[cfg.JWTSecret] {
			return nil, fmt.Errorf("JWT_SECRET must be set to a random string of at least 32 characters in production")
		}
		if cfg.S3AccessKey == "" || cfg.S3SecretKey == "" {
			return nil, fmt.Errorf("S3_ACCESS_KEY and S3_SECRET_KEY are required in production")
		}
	} else if cfg.JWTSecret == "" {
		// Development only: a per-process random secret (sessions reset on restart)
		cfg.JWTSecret = RandomString(32)
		log.Println("Warning: JWT_SECRET not set, using a random secret for this process (development only)")
	}

	return cfg, nil
}

// RandomString returns a cryptographically random hex string of n bytes.
func RandomString(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return hex.EncodeToString(b)
}

func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func getEnv(key, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return defaultVal
}
