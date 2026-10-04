package config

import (
	"fmt"
	"os"
	"strconv"

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
	LegalOperatorName    string
	LegalOperatorEmail   string
	LegalOperatorCountry string
	LegalProjectNotice   string
}

func Load() (*Config, error) {
	// Attempt to load .env file if present (ignored in production if not present)
	_ = godotenv.Load()

	jwtSecret := getEnv("JWT_SECRET", "")
	if jwtSecret == "" {
		jwtSecret = "dev-secret-change-me-in-production-random-bytes"
	}

	dbURL := getEnv("DATABASE_URL", "postgres://mnema:secret@localhost:5432/mnema_talk?sslmode=disable")

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
		JWTSecret:            jwtSecret,
		SessionExpiryHours:   expiryHours,
		AdminUsername:        getEnv("ADMIN_USERNAME", "Herzog"),
		AdminInitialPassword: getEnv("ADMIN_INITIAL_PASSWORD", "HerzogTalk2026!"),
		DatabaseURL:          dbURL,
		S3Endpoint:           getEnv("S3_ENDPOINT", "http://localhost:8333"),
		S3Region:             getEnv("S3_REGION", "us-east-1"),
		S3Bucket:             getEnv("S3_BUCKET", "mnema-media"),
		S3AccessKey:          getEnv("S3_ACCESS_KEY", "mnema_admin"),
		S3SecretKey:          getEnv("S3_SECRET_KEY", "mnema_secret"),
		S3ForcePathStyle:     forcePathStyle,
		MediaRetentionDays:   retentionDays,
		WebRTCUDPPortMin:     uint16(portMin),
		WebRTCUDPPortMax:     uint16(portMax),
		WebRTCNAT1to1IP:      getEnv("WEBRTC_NAT_1TO1_IP", ""),
		LegalOperatorName:    getEnv("LEGAL_OPERATOR_NAME", "Community Operator"),
		LegalOperatorEmail:   getEnv("LEGAL_OPERATOR_EMAIL", "admin@example.com"),
		LegalOperatorCountry: getEnv("LEGAL_OPERATOR_COUNTRY", "Deutschland"),
		LegalProjectNotice:   getEnv("LEGAL_PROJECT_NOTICE", "Privates, nicht-kommerzielles Projekt"),
	}

	if cfg.Port == "" {
		return nil, fmt.Errorf("PORT is required")
	}

	return cfg, nil
}

func getEnv(key, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return defaultVal
}
