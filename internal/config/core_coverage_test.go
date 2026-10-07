package config

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadReadsOptionalDotenvWithoutOverridingEnvironment(t *testing.T) {
	// Clear configuration keys while preserving the surrounding test process.
	// A temporary working directory keeps the real workspace .env untouched.
	example, err := os.ReadFile("../../.env.example")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(example), "\n") {
		key, _, ok := strings.Cut(line, "=")
		if ok && !strings.HasPrefix(strings.TrimSpace(line), "#") {
			t.Setenv(strings.TrimSpace(key), "")
		}
	}
	t.Setenv("DATABASE_URL", "postgres://test-only-environment")
	t.Setenv("APP_ENV", "test")
	t.Setenv("JWT_SECRET", "test-only-configuration-secret")
	t.Setenv("ADMIN_USERNAME", "")
	if err := os.Unsetenv("ADMIN_USERNAME"); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("DATABASE_URL=postgres://test-only-file\nADMIN_USERNAME=FileAdmin\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DatabaseURL != "postgres://test-only-environment" || cfg.AdminUsername != "FileAdmin" {
		t.Fatalf("dotenv precedence: database=%q admin=%q", cfg.DatabaseURL, cfg.AdminUsername)
	}
	if err := os.Remove(filepath.Join(dir, ".env")); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err != nil {
		t.Fatalf("optional .env absence rejected: %v", err)
	}
}

func TestMalformedNumericSettingsNameTheirField(t *testing.T) {
	for _, key := range []string{"MEDIA_RETENTION_DAYS", "MAX_UPLOAD_SIZE_MB", "WEBRTC_UDP_PORT_MIN"} {
		t.Run(key, func(t *testing.T) {
			env := base()
			env[key] = "not-a-number"
			cfg, err := FromEnv(lookup(env))
			if cfg != nil || err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("malformed %s: cfg=%v err=%v", key, cfg, err)
			}
		})
	}
}

func TestUploadRelayAndMetricsValidation(t *testing.T) {
	for name, changes := range map[string]map[string]string{
		"upload zero":        {"MAX_UPLOAD_SIZE_MB": "0"},
		"upload oversized":   {"MAX_UPLOAD_SIZE_MB": "2049"},
		"TURN weak secret":   {"WEBRTC_TURN_URLS": "turn:relay.example.com:3478", "WEBRTC_TURN_SECRET": "short"},
		"metrics weak token": {"METRICS_TOKEN": "short"},
	} {
		t.Run(name, func(t *testing.T) {
			env := base()
			for k, v := range changes {
				env[k] = v
			}
			if cfg, err := FromEnv(lookup(env)); cfg != nil || err == nil {
				t.Fatalf("invalid %s accepted: cfg=%v err=%v", name, cfg, err)
			}
		})
	}
	env := base()
	env["APP_ENV"] = "dev"
	env["MAX_UPLOAD_SIZE_MB"] = "2048"
	env["WEBRTC_TURN_URLS"] = "turn:relay.example.com:3478"
	env["WEBRTC_TURN_SECRET"] = strings.Repeat("t", 16)
	env["METRICS_TOKEN"] = strings.Repeat("m", 24)
	cfg, err := FromEnv(lookup(env))
	if err != nil || cfg.AppEnv != "development" || cfg.MaxUploadMB != 2048 {
		t.Fatalf("valid boundary settings: cfg=%v err=%v", cfg, err)
	}
}

func TestRandomHexReturnsRequestedCryptographicByteLength(t *testing.T) {
	for _, n := range []int{0, 1, 32} {
		value := RandomHex(n)
		decoded, err := hex.DecodeString(value)
		if err != nil || len(decoded) != n {
			t.Fatalf("RandomHex(%d) length/encoding: %d %v", n, len(decoded), err)
		}
	}
}
