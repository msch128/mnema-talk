package config

import (
	"bufio"
	"encoding/hex"
	"os"
	"path/filepath"
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
	if cfg.InitialLanguage != "en" {
		t.Errorf("a new community is named in English unless INITIAL_LANGUAGE says otherwise, got %q", cfg.InitialLanguage)
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
		"missing db":               {},
		"bad public url":           {"DATABASE_URL": "x", "PUBLIC_URL": "chat.example.com"},
		"bad port range":           {"DATABASE_URL": "x", "WEBRTC_UDP_PORT_MIN": "50050", "WEBRTC_UDP_PORT_MAX": "50000"},
		"port too large":           {"DATABASE_URL": "x", "WEBRTC_UDP_PORT_MAX": "65536"},
		"port zero":                {"DATABASE_URL": "x", "WEBRTC_UDP_PORT_MIN": "0"},
		"negative retention":       {"DATABASE_URL": "x", "MEDIA_RETENTION_DAYS": "-1"},
		"negative audit retention": {"DATABASE_URL": "x", "AUDIT_RETENTION_DAYS": "-1"},
		"bad audit retention":      {"DATABASE_URL": "x", "AUDIT_RETENTION_DAYS": "forever"},
		"negative edit history":    {"DATABASE_URL": "x", "EDIT_HISTORY_RETENTION_DAYS": "-1"},
		"non-numeric int":          {"DATABASE_URL": "x", "SESSION_EXPIRY_HOURS": "soon"},
		"bad proxy cidr":           {"DATABASE_URL": "x", "TRUSTED_PROXY_CIDRS": "10.0.0.0/99"},
		"bad bool":                 {"DATABASE_URL": "x", "LINK_PREVIEWS_ENABLED": "yes please"},
		"bad path style":           {"DATABASE_URL": "x", "S3_FORCE_PATH_STYLE": "maybe"},
		"bad api docs":             {"DATABASE_URL": "x", "API_DOCS_ENABLED": "on"},
		"unknown log level":        {"DATABASE_URL": "x", "LOG_LEVEL": "verbose"},
		"expiry too long":          {"DATABASE_URL": "x", "SESSION_EXPIRY_HOURS": "8761"},
		"expiry zero":              {"DATABASE_URL": "x", "SESSION_EXPIRY_HOURS": "0"},
		"http port zero":           {"DATABASE_URL": "x", "PORT": "0"},
		"http port too big":        {"DATABASE_URL": "x", "PORT": "70000"},
		"http port text":           {"DATABASE_URL": "x", "PORT": "http"},
		"bad push switch":          {"DATABASE_URL": "x", "PUSH_ENABLED": "sometimes"},
		"bad push previews":        {"DATABASE_URL": "x", "PUSH_PREVIEWS": "always"},
		"bad initial language":     {"DATABASE_URL": "x", "INITIAL_LANGUAGE": "fr"},
		"push subject is plain":    {"DATABASE_URL": "x", "PUSH_VAPID_SUBJECT": "ops@example.com"},
		"push subject is http":     {"DATABASE_URL": "x", "PUSH_VAPID_SUBJECT": "http://chat.example.com"},
		"push subject has no addr": {"DATABASE_URL": "x", "PUSH_VAPID_SUBJECT": "mailto:nobody"},
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
		{"production without address or TURN", Config{AppEnv: "production", WebRTCBuiltinTURN: BuiltinTURNOff}, true},
		{"production without address but with the built-in relay", Config{AppEnv: "production", WebRTCBuiltinTURN: BuiltinTURNAuto}, false},
		{"production with announced address", Config{AppEnv: "production", WebRTCAnnounce: []string{"203.0.113.7"}}, false},
		{"production with TURN", Config{AppEnv: "production", WebRTCTURNURLs: []string{"turn:turn.example.com:3478"}}, false},
	}
	for _, tc := range cases {
		if got := tc.cfg.WebRTCReachabilityWarning() != ""; got != tc.warn {
			t.Errorf("%s: warning %v, want %v", tc.name, got, tc.warn)
		}
	}
}

func TestWebRTCRelayNotice(t *testing.T) {
	cases := []struct {
		name string
		urls []string
		note bool
	}{
		{"no TURN", nil, true},
		{"UDP only", []string{"turn:turn.example.com:3478?transport=udp"}, true},
		{"UDP by default", []string{"turn:turn.example.com:3478"}, true},
		{"TCP", []string{"turn:turn.example.com:3478?transport=udp", "turn:turn.example.com:3478?transport=tcp"}, false},
		{"TLS", []string{"TURNS:turn.example.com:443?transport=tcp"}, false},
	}
	for _, tc := range cases {
		cfg := Config{WebRTCTURNURLs: tc.urls}
		if got := cfg.WebRTCRelayNotice() != ""; got != tc.note {
			t.Errorf("%s: notice %v, want %v", tc.name, got, tc.note)
		}
	}
}

func TestWebRTCRelayNoticeKnowsTheBuiltinRelay(t *testing.T) {
	cfg := Config{WebRTCTURNURLs: []string{"turn:turn.example.com:3478?transport=udp"}}
	if cfg.WebRTCRelayNoticeWith(true) != "" {
		t.Error("noticed a missing TCP path although the built-in relay listens on TCP")
	}
	if cfg.WebRTCRelayNoticeWith(false) == "" {
		t.Error("no notice without any TCP path")
	}
}

func TestInsecurePublicURLWarning(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		warn bool
	}{
		{"production over http", Config{AppEnv: "production", PublicURL: "http://chat.example.com"}, true},
		{"production default localhost", Config{AppEnv: "production", PublicURL: "http://localhost:8080"}, true},
		{"staging over http", Config{AppEnv: "staging", PublicURL: "http://chat.example.com"}, true},
		{"production over https", Config{AppEnv: "production", PublicURL: "https://chat.example.com"}, false},
		{"development over http", Config{AppEnv: "development", PublicURL: "http://localhost:8080"}, false},
	}
	for _, tc := range cases {
		if got := tc.cfg.InsecurePublicURLWarning() != ""; got != tc.warn {
			t.Errorf("%s: warning %v, want %v", tc.name, got, tc.warn)
		}
	}
}

func TestInsecurePublicURLIsNotARefusal(t *testing.T) {
	env := base()
	env["APP_ENV"] = "production"
	env["PUBLIC_URL"] = "http://chat.example.com"
	env["JWT_SECRET"] = strings.Repeat("k", 40)
	env["S3_ACCESS_KEY"], env["S3_SECRET_KEY"] = "access-key-value", strings.Repeat("s", 40)
	cfg, err := FromEnv(lookup(env))
	if err != nil {
		t.Fatalf("an http PUBLIC_URL must start with a warning, not refuse to start: %v", err)
	}
	if cfg.InsecurePublicURLWarning() == "" {
		t.Fatal("no warning for an http PUBLIC_URL in production")
	}
}

func TestBindAddrDefaultsAndDevelopmentRule(t *testing.T) {
	cfg, err := FromEnv(lookup(base()))
	if err != nil || cfg.BindAddr != "127.0.0.1" {
		t.Fatalf("development default must be loopback, got %q err=%v", cfg.BindAddr, err)
	}
	for _, addr := range []string{"0.0.0.0", "192.168.1.5", "::"} {
		m := base()
		m["BIND_ADDR"] = addr
		if _, err := FromEnv(lookup(m)); err == nil || !strings.Contains(err.Error(), "BIND_ADDR") {
			t.Errorf("development must refuse BIND_ADDR=%s, got %v", addr, err)
		}
	}
	for _, addr := range []string{"localhost", "127.0.0.2", "::1", "[::1]"} {
		m := base()
		m["BIND_ADDR"] = addr
		if _, err := FromEnv(lookup(m)); err != nil {
			t.Errorf("development must accept BIND_ADDR=%s: %v", addr, err)
		}
	}
	for _, appEnv := range []string{"test", "dev"} {
		m := base()
		m["APP_ENV"] = appEnv
		if cfg, err := FromEnv(lookup(m)); err != nil || cfg.BindAddr != "127.0.0.1" {
			t.Errorf("APP_ENV=%s default must be loopback, got %q %v", appEnv, cfg.BindAddr, err)
		}
		m["BIND_ADDR"] = "0.0.0.0"
		if _, err := FromEnv(lookup(m)); err == nil || !strings.Contains(err.Error(), "BIND_ADDR") {
			t.Errorf("APP_ENV=%s must refuse a wildcard listener, got %v", appEnv, err)
		}
	}
	env := prodEnv()
	if cfg, err := FromEnv(lookup(env)); err != nil || cfg.BindAddr != "0.0.0.0" {
		t.Errorf("production default must be 0.0.0.0, got %v", err)
	}
}

func TestBroadTrustedProxiesCoversIPv4MappedRange(t *testing.T) {
	c := &Config{TrustedProxies: []string{"::ffff:0:0/96", "0::ffff:0.0.0.0/96", "::ffff:10.0.0.0/104", "::ffff:10.0.0.0/120", "::ffff:10.0.0.5/128"}}
	got := strings.Join(c.BroadTrustedProxies(), ",")
	if got != "::ffff:0:0/96,0::ffff:0.0.0.0/96,::ffff:10.0.0.0/104" {
		t.Fatalf("broad = %q", got)
	}
}

func TestCORSAllowedOriginsAreValidated(t *testing.T) {
	for _, bad := range []string{"chat.example.com", "https://chat.example.com/app", "https://chat.example.com?x=1", "ftp://chat.example.com", "https://a.example.com,chat.example.com"} {
		env := base()
		env["CORS_ALLOWED_ORIGINS"] = bad
		if _, err := FromEnv(lookup(env)); err == nil || !strings.Contains(err.Error(), "CORS_ALLOWED_ORIGINS") {
			t.Errorf("CORS_ALLOWED_ORIGINS=%q accepted: %v", bad, err)
		}
	}
	env := base()
	env["CORS_ALLOWED_ORIGINS"] = "https://chat.example.com/, http://localhost:3000"
	if _, err := FromEnv(lookup(env)); err != nil {
		t.Fatalf("valid origins refused: %v", err)
	}
}

func TestBroadTrustedProxies(t *testing.T) {
	c := &Config{TrustedProxies: []string{"127.0.0.0/8", "::1/128", "172.16.0.0/12", "192.168.0.20", "10.0.0.0/24", "fc00::/7", "fd00::/64", "bad"}}
	got := strings.Join(c.BroadTrustedProxies(), ",")
	if got != "172.16.0.0/12,fc00::/7" {
		t.Fatalf("broad = %q", got)
	}
}

func TestProductionRefusesBroadTrustedProxies(t *testing.T) {
	env := prodEnv()
	env["TRUSTED_PROXY_CIDRS"] = "172.16.0.0/12,10.1.2.3"
	_, err := FromEnv(lookup(env))
	if err == nil || !strings.Contains(err.Error(), "172.16.0.0/12") || strings.Contains(err.Error(), "10.1.2.3") {
		t.Fatalf("broad range: %v", err)
	}

	env["APP_ENV"] = "staging"
	if _, err := FromEnv(lookup(env)); err == nil || !strings.Contains(err.Error(), "TRUSTED_PROXY_CIDRS") {
		t.Fatalf("staging must refuse the same range: %v", err)
	}

	env = prodEnv()
	env["TRUSTED_PROXY_CIDRS"] = "172.21.0.1,10.0.0.0/24,fd00::/64"
	if _, err := FromEnv(lookup(env)); err != nil {
		t.Fatalf("a proxy address and a /24 must be allowed: %v", err)
	}

	env["BIND_ADDR"] = "127.0.0.1"
	env["TRUSTED_PROXY_CIDRS"] = "172.16.0.0/12"
	if _, err := FromEnv(lookup(env)); err != nil {
		t.Fatalf("loopback listener must keep a broad range: %v", err)
	}

	env = prodEnv()
	env["APP_ENV"] = "test"
	env["TRUSTED_PROXY_CIDRS"] = "172.16.0.0/12"
	if _, err := FromEnv(lookup(env)); err != nil {
		t.Fatalf("test env must still start: %v", err)
	}
}

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
		"upload zero":            {"MAX_UPLOAD_SIZE_MB": "0"},
		"upload oversized":       {"MAX_UPLOAD_SIZE_MB": "2049"},
		"negative quota":         {"UPLOAD_QUOTA_MB_PER_DAY": "-1"},
		"negative total cap":     {"MEDIA_MAX_TOTAL_MB": "-1"},
		"negative free floor":    {"MEDIA_MIN_FREE_MB": "-1"},
		"TURN weak secret":       {"WEBRTC_TURN_URLS": "turn:relay.example.com:3478", "WEBRTC_TURN_SECRET": "short"},
		"metrics weak token":     {"METRICS_TOKEN": "short"},
		"pprof on all addresses": {"PPROF_ADDR": "0.0.0.0:6060"},
		"pprof without a host":   {"PPROF_ADDR": ":6060"},
		"pprof public host":      {"PPROF_ADDR": "192.0.2.1:6060"},
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
	env["PPROF_ADDR"] = "127.0.0.1:6060"
	cfg, err := FromEnv(lookup(env))
	if err != nil || cfg.AppEnv != "development" || cfg.MaxUploadMB != 2048 || cfg.PprofAddr != "127.0.0.1:6060" {
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

func TestStorageBudgetDefaults(t *testing.T) {
	cfg, err := FromEnv(lookup(base()))
	if err != nil || cfg.UploadQuotaMBPerDay != 2048 || cfg.MediaMaxTotalMB != 0 || cfg.MediaMinFreeMB != 1024 || cfg.MediaDiskPath != "" {
		t.Fatalf("defaults: %+v %v", cfg, err)
	}
	env := base()
	env["UPLOAD_QUOTA_MB_PER_DAY"] = "0"
	env["MEDIA_DISK_PATH"] = "/data"
	if cfg, err := FromEnv(lookup(env)); err != nil || cfg.UploadQuotaMBPerDay != 0 || cfg.MediaDiskPath != "/data" {
		t.Fatalf("explicit: %+v %v", cfg, err)
	}
}

func TestWebPushSettings(t *testing.T) {
	cfg, err := FromEnv(lookup(base()))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.PushEnabled || cfg.PushPreviews != "user" || cfg.PushVAPIDSubject != "" {
		t.Fatalf("defaults: enabled=%v previews=%q subject=%q", cfg.PushEnabled, cfg.PushPreviews, cfg.PushVAPIDSubject)
	}
	for _, subject := range []string{"https://chat.example.com/contact", "mailto:ops@example.com"} {
		env := base()
		env["PUSH_ENABLED"] = "false"
		env["PUSH_PREVIEWS"] = "OFF"
		env["PUSH_VAPID_SUBJECT"] = subject
		cfg, err := FromEnv(lookup(env))
		if err != nil {
			t.Fatalf("%s: %v", subject, err)
		}
		if cfg.PushEnabled || cfg.PushPreviews != "off" || cfg.PushVAPIDSubject != subject {
			t.Fatalf("%s: enabled=%v previews=%q subject=%q", subject, cfg.PushEnabled, cfg.PushPreviews, cfg.PushVAPIDSubject)
		}
	}
}
