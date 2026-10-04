package config

import (
	"strings"
	"testing"
)

func lookup(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

func base() map[string]string {
	return map[string]string{"DATABASE_URL": "postgres://x"}
}

func TestDefaults(t *testing.T) {
	cfg, err := FromEnv(lookup(base()))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MediaRetentionDays != 0 {
		t.Errorf("media retention must default to 0 (off), got %d", cfg.MediaRetentionDays)
	}
	if cfg.JWTSecret == "" {
		t.Error("development should generate a random JWT secret")
	}
	if len(cfg.AllowedOrigins) != 1 || cfg.AllowedOrigins[0] != cfg.PublicURL {
		t.Errorf("allowed origins should default to PUBLIC_URL, got %v", cfg.AllowedOrigins)
	}
	if cfg.AdminInitialPassword != "" {
		t.Error("there must be no built-in admin password")
	}
	if cfg.SecureCookies() {
		t.Error("http PUBLIC_URL must not force Secure cookies")
	}
}

func TestDevSecretIsRandomPerProcess(t *testing.T) {
	a, _ := FromEnv(lookup(base()))
	b, _ := FromEnv(lookup(base()))
	if a.JWTSecret == b.JWTSecret {
		t.Fatal("development JWT secret must be random, not a constant")
	}
}

func TestProductionRequiresSecrets(t *testing.T) {
	env := base()
	env["APP_ENV"] = "production"
	if _, err := FromEnv(lookup(env)); err == nil || !strings.Contains(err.Error(), "JWT_SECRET") {
		t.Fatalf("expected JWT_SECRET error, got %v", err)
	}

	env["JWT_SECRET"] = "replace_with_a_secure_random_string_in_production"
	if _, err := FromEnv(lookup(env)); err == nil {
		t.Fatal("example placeholder accepted as JWT secret")
	}

	env["JWT_SECRET"] = strings.Repeat("k", 32)
	if _, err := FromEnv(lookup(env)); err == nil || !strings.Contains(err.Error(), "S3_") {
		t.Fatalf("expected S3 credentials error, got %v", err)
	}

	env["S3_ACCESS_KEY"], env["S3_SECRET_KEY"] = "a", "b"
	if _, err := FromEnv(lookup(env)); err != nil {
		t.Fatalf("valid production config rejected: %v", err)
	}
}

func TestRejectsPlaceholderAdminPassword(t *testing.T) {
	env := base()
	env["ADMIN_INITIAL_PASSWORD"] = "change_this_password_immediately"
	if _, err := FromEnv(lookup(env)); err == nil {
		t.Fatal("public example admin password accepted")
	}
}

func TestValidation(t *testing.T) {
	cases := map[string]map[string]string{
		"missing db":         {},
		"bad public url":     {"DATABASE_URL": "x", "PUBLIC_URL": "chat.example.com"},
		"bad port range":     {"DATABASE_URL": "x", "WEBRTC_UDP_PORT_MIN": "50050", "WEBRTC_UDP_PORT_MAX": "50000"},
		"negative retention": {"DATABASE_URL": "x", "MEDIA_RETENTION_DAYS": "-1"},
		"non-numeric int":    {"DATABASE_URL": "x", "SESSION_EXPIRY_HOURS": "soon"},
		"bad proxy cidr":     {"DATABASE_URL": "x", "TRUSTED_PROXY_CIDRS": "10.0.0.0/99"},
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := FromEnv(lookup(env)); err == nil {
				t.Fatal("expected a validation error")
			}
		})
	}
}

func TestHTTPSPublicURLForcesSecureCookies(t *testing.T) {
	env := base()
	env["PUBLIC_URL"] = "https://chat.example.com/"
	cfg, err := FromEnv(lookup(env))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.SecureCookies() || cfg.PublicURL != "https://chat.example.com" {
		t.Fatalf("secure=%v public=%q", cfg.SecureCookies(), cfg.PublicURL)
	}
}
