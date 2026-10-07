package config

import (
	"bufio"
	"os"
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
	if !cfg.UpdateCheck {
		t.Error("the update check should default to on")
	}
}

func TestUpdaterSettings(t *testing.T) {
	cfg, err := FromEnv(lookup(base()))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SelfUpdateConfigured() {
		t.Fatal("self-update must be off without UPDATER_TOKEN")
	}

	env := base()
	env["UPDATER_URL"] = "http://elsewhere:9000"
	if cfg, err := FromEnv(lookup(env)); err != nil || cfg.UpdaterURL != "" || cfg.SelfUpdateConfigured() {
		t.Fatalf("URL without token: err=%v url=%q", err, cfg.UpdaterURL)
	}

	token := strings.Repeat("a1", 16)
	env = base()
	env["UPDATER_TOKEN"] = token
	cfg, err = FromEnv(lookup(env))
	if err != nil || !cfg.SelfUpdateConfigured() || cfg.UpdaterURL != DefaultUpdaterURL {
		t.Fatalf("token only: err=%v url=%q", err, cfg.UpdaterURL)
	}

	for name, kv := range map[string][2]string{
		"short token":       {"UPDATER_TOKEN", "too-short"},
		"placeholder token": {"UPDATER_TOKEN", "replace_with_a_random_updater_token_of_32_chars"},
		"token with space":  {"UPDATER_TOKEN", strings.Repeat("a", 32) + " b"},
		"bad url scheme":    {"UPDATER_URL", "ftp://mnema-updater:8080"},
		"url credentials":   {"UPDATER_URL", "http://u:p@mnema-updater:8080"},
		"url query":         {"UPDATER_URL", "http://mnema-updater:8080/?x=1"},
	} {
		env := base()
		env["UPDATER_TOKEN"] = token
		env[kv[0]] = kv[1]
		if _, err := FromEnv(lookup(env)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestUpdateCheckCanBeDisabled(t *testing.T) {
	env := base()
	env["UPDATE_CHECK_ENABLED"] = "false"
	cfg, err := FromEnv(lookup(env))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UpdateCheck {
		t.Fatal("UPDATE_CHECK_ENABLED=false ignored")
	}
	env["UPDATE_CHECK_ENABLED"] = "maybe"
	if _, err := FromEnv(lookup(env)); err == nil {
		t.Fatal("invalid UPDATE_CHECK_ENABLED accepted")
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
		"port too large":     {"DATABASE_URL": "x", "WEBRTC_UDP_PORT_MAX": "65536"},
		"port zero":          {"DATABASE_URL": "x", "WEBRTC_UDP_PORT_MIN": "0"},
		"negative retention": {"DATABASE_URL": "x", "MEDIA_RETENTION_DAYS": "-1"},
		"non-numeric int":    {"DATABASE_URL": "x", "SESSION_EXPIRY_HOURS": "soon"},
		"bad proxy cidr":     {"DATABASE_URL": "x", "TRUSTED_PROXY_CIDRS": "10.0.0.0/99"},
		"bad bool":           {"DATABASE_URL": "x", "LINK_PREVIEWS_ENABLED": "yes please"},
		"bad path style":     {"DATABASE_URL": "x", "S3_FORCE_PATH_STYLE": "maybe"},
		"bad api docs":       {"DATABASE_URL": "x", "API_DOCS_ENABLED": "on"},
		"unknown log level":  {"DATABASE_URL": "x", "LOG_LEVEL": "verbose"},
		"expiry too long":    {"DATABASE_URL": "x", "SESSION_EXPIRY_HOURS": "8761"},
		"expiry zero":        {"DATABASE_URL": "x", "SESSION_EXPIRY_HOURS": "0"},
		"http port zero":     {"DATABASE_URL": "x", "PORT": "0"},
		"http port too big":  {"DATABASE_URL": "x", "PORT": "70000"},
		"http port text":     {"DATABASE_URL": "x", "PORT": "http"},
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

func TestAppEnvAliasesUseProductionChecks(t *testing.T) {
	for _, alias := range []string{"prod", "Production", "staging"} {
		env := base()
		env["APP_ENV"] = alias
		if _, err := FromEnv(lookup(env)); err == nil || !strings.Contains(err.Error(), "JWT_SECRET") {
			t.Fatalf("APP_ENV=%s skipped the production checks: %v", alias, err)
		}
	}
}

func TestUnknownAppEnvRejected(t *testing.T) {
	env := base()
	env["APP_ENV"] = "prdocution"
	if _, err := FromEnv(lookup(env)); err == nil || !strings.Contains(err.Error(), "APP_ENV") {
		t.Fatalf("typo in APP_ENV accepted: %v", err)
	}
}

func prodEnv() map[string]string {
	return map[string]string{
		"APP_ENV":       "production",
		"DATABASE_URL":  "postgres://mnema:s3cret-db-pass@postgres:5432/mnema_talk",
		"JWT_SECRET":    strings.Repeat("k", 32),
		"S3_ACCESS_KEY": "mnema_admin",
		"S3_SECRET_KEY": "a-real-secret-key",
	}
}

func TestProductionRejectsExamplePlaceholders(t *testing.T) {
	if _, err := FromEnv(lookup(prodEnv())); err != nil {
		t.Fatalf("valid production config rejected: %v", err)
	}
	cases := map[string]string{
		"S3_ACCESS_KEY":      "replace_with_access_key",
		"S3_SECRET_KEY":      "replace_with_secret_key",
		"DATABASE_URL":       "postgres://mnema:replace_with_a_random_database_password@localhost:5432/mnema_talk?sslmode=disable",
		"JWT_SECRET":         "replace_with_a_secure_random_string_in_production",
		"METRICS_TOKEN":      "replace_with_a_long_metrics_token_value",
		"WEBRTC_TURN_SECRET": "replace_with_turn_secret_value",
	}
	for key, val := range cases {
		t.Run(key, func(t *testing.T) {
			env := prodEnv()
			env[key] = val
			if key == "WEBRTC_TURN_SECRET" {
				env["WEBRTC_TURN_URLS"] = "turn:turn.example.com:3478"
			}
			if _, err := FromEnv(lookup(env)); err == nil {
				t.Fatalf("placeholder %s=%s accepted in production", key, val)
			}
		})
	}
}

// Every placeholder value shipped in .env.example must be recognised, so a
// copied but unedited .env can never start in production.
func TestEnvExamplePlaceholdersAreKnown(t *testing.T) {
	f, err := os.Open("../../.env.example")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	seen := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		key, val, ok := strings.Cut(line, "=")
		if !ok || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(val, "replace_with") || strings.Contains(val, "change_this") {
			seen++
			if key == "DATABASE_URL" {
				env := prodEnv()
				env["DATABASE_URL"] = val
				if _, err := FromEnv(lookup(env)); err == nil {
					t.Errorf("example DATABASE_URL accepted in production")
				}
				continue
			}
			if !isPlaceholder(val) {
				t.Errorf("%s=%s from .env.example is not treated as a placeholder", key, val)
			}
		}
	}
	if seen < 4 {
		t.Fatalf("expected the example placeholders in .env.example, found %d", seen)
	}
	// The unedited example file itself must be refused in production.
	env := map[string]string{}
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	sc = bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if key, val, ok := strings.Cut(line, "="); ok && !strings.HasPrefix(line, "#") {
			env[key] = val
		}
	}
	env["JWT_SECRET"] = strings.Repeat("k", 32) // isolate the other placeholders
	if _, err := FromEnv(lookup(env)); err == nil {
		t.Fatal("unedited .env.example accepted in production")
	}
}

func TestBooleansParse(t *testing.T) {
	env := base()
	env["LINK_PREVIEWS_ENABLED"] = "0"
	env["API_DOCS_ENABLED"] = "TRUE"
	env["S3_FORCE_PATH_STYLE"] = "false"
	cfg, err := FromEnv(lookup(env))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LinkPreviews || !cfg.APIDocs || cfg.S3ForcePathStyle {
		t.Fatalf("previews=%v docs=%v pathStyle=%v", cfg.LinkPreviews, cfg.APIDocs, cfg.S3ForcePathStyle)
	}
}

func TestLogLevels(t *testing.T) {
	for _, lvl := range []string{"", "debug", "INFO", "warn", "error"} {
		env := base()
		env["LOG_LEVEL"] = lvl
		if _, err := FromEnv(lookup(env)); err != nil {
			t.Errorf("LOG_LEVEL=%q rejected: %v", lvl, err)
		}
	}
}

func TestTrustedProxiesDefaultToLoopback(t *testing.T) {
	cfg, err := FromEnv(lookup(base()))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range cfg.TrustedProxies {
		if p != "127.0.0.0/8" && p != "::1/128" {
			t.Fatalf("default trusts more than loopback: %v", cfg.TrustedProxies)
		}
	}
}

func TestPublicURLDropsDefaultPort(t *testing.T) {
	cases := map[string]string{
		"https://Chat.Example.com:443/": "https://chat.example.com",
		"http://chat.example.com:80":    "http://chat.example.com",
		"https://chat.example.com:8443": "https://chat.example.com:8443",
		"http://chat.example.com:443":   "http://chat.example.com:443",
		"https://[::1]:443":             "https://[::1]",
	}
	for in, want := range cases {
		env := base()
		env["PUBLIC_URL"] = in
		cfg, err := FromEnv(lookup(env))
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if cfg.PublicURL != want || cfg.AllowedOrigins[0] != want {
			t.Errorf("%s: public=%q origins=%v, want %q", in, cfg.PublicURL, cfg.AllowedOrigins, want)
		}
	}
	env := base()
	env["CORS_ALLOWED_ORIGINS"] = "https://a.example.com:443, http://localhost:3000"
	cfg, err := FromEnv(lookup(env))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(cfg.AllowedOrigins, ",") != "https://a.example.com,http://localhost:3000" {
		t.Fatalf("origins=%v", cfg.AllowedOrigins)
	}
}

func TestUDPMuxIsDefault(t *testing.T) {
	cfg, err := FromEnv(lookup(base()))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WebRTCUDPMuxPort != cfg.WebRTCUDPPortMin {
		t.Errorf("WEBRTC_UDP_MUX_PORT must default to WEBRTC_UDP_PORT_MIN (%d), got %d", cfg.WebRTCUDPPortMin, cfg.WebRTCUDPMuxPort)
	}
	env := base()
	env["WEBRTC_UDP_PORT_MIN"], env["WEBRTC_UDP_PORT_MAX"] = "51000", "51010"
	if cfg, err = FromEnv(lookup(env)); err != nil || cfg.WebRTCUDPMuxPort != 51000 {
		t.Fatalf("got %v, %v; want the moved range's first port", cfg, err)
	}
	env["WEBRTC_UDP_MUX_PORT"] = "0"
	if cfg, err = FromEnv(lookup(env)); err != nil || cfg.WebRTCUDPMuxPort != 0 {
		t.Fatalf("explicit 0 must keep per-peer ports: %v, %v", cfg, err)
	}
	env["WEBRTC_UDP_MUX_PORT"] = "51005"
	if cfg, err = FromEnv(lookup(env)); err != nil || cfg.WebRTCUDPMuxPort != 51005 {
		t.Fatalf("got %v, %v; want 51005", cfg, err)
	}
}

func TestWebRTCReachabilityWarning(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		warn bool
	}{
		{"development", Config{AppEnv: "development"}, false},
		{"production without address or TURN", Config{AppEnv: "production"}, true},
		{"production with announced address", Config{AppEnv: "production", WebRTCAnnounce: []string{"203.0.113.7"}}, false},
		{"production with TURN", Config{AppEnv: "production", WebRTCTURNURLs: []string{"turn:turn.example.com:3478"}}, false},
	}
	for _, tc := range cases {
		if got := tc.cfg.WebRTCReachabilityWarning() != ""; got != tc.warn {
			t.Errorf("%s: warning %v, want %v", tc.name, got, tc.warn)
		}
	}
}
