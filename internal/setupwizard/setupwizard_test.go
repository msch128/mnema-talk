package setupwizard

import (
	"bytes"
	"context"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"

	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/setupcheck"
)

const repoCompose = "../../docker-compose.yml"

var fixedNow = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

func fakeProbe() Probe {
	return Probe{
		Resolve: func(context.Context, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("203.0.113.7")}, nil
		},
		PublicIP: func(context.Context) (netip.Addr, error) { return netip.MustParseAddr("203.0.113.7"), nil },
		Dial:     func(context.Context, string) error { return nil },
		Skew:     func(context.Context) (time.Duration, error) { return time.Second, nil },
	}
}

func repoTemplate(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(repoCompose)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// asProductionConfig loads the generated .env the way docker compose feeds it
// to the app container.
func asProductionConfig(t *testing.T, envFile string) *config.Config {
	t.Helper()
	env, err := godotenv.Unmarshal(envFile)
	if err != nil {
		t.Fatal(err)
	}
	env["APP_ENV"] = "production"
	env["DATABASE_URL"] = "postgres://mnema:" + env["POSTGRES_PASSWORD"] + "@postgres:5432/mnema_talk"
	cfg, err := config.FromEnv(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	if err != nil {
		t.Fatalf("the generated .env is refused by the server: %v\n%s", err, envFile)
	}
	return cfg
}

func TestGeneratedEnvIsAcceptedByTheServer(t *testing.T) {
	o, err := Options{PublicURL: "Chat.Example.com", AutoUpdate: true, AdminUsername: "boss"}.normalize()
	if err != nil {
		t.Fatal(err)
	}
	cfg := asProductionConfig(t, RenderEnv(o, NewSecrets(), fixedNow))
	if cfg.PublicURL != "https://chat.example.com" || strings.Join(cfg.AllowedOrigins, ",") != "https://chat.example.com" || !cfg.SecureCookies() {
		t.Errorf("derived settings: %q %v %v", cfg.PublicURL, cfg.AllowedOrigins, cfg.SecureCookies())
	}
	if cfg.AdminUsername != "boss" || cfg.InitialLanguage != "en" || !cfg.SelfUpdateConfigured() {
		t.Errorf("admin %q language %q selfupdate %v", cfg.AdminUsername, cfg.InitialLanguage, cfg.SelfUpdateConfigured())
	}
}

func TestEnvHasMinimalRequiredSettings(t *testing.T) {
	o, _ := Options{PublicURL: "https://chat.example.com"}.normalize()
	env, err := godotenv.Unmarshal(RenderEnv(o, NewSecrets(), fixedNow))
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"PUBLIC_URL", "MNEMA_IMAGE", "JWT_SECRET", "POSTGRES_PASSWORD", "S3_ACCESS_KEY", "S3_SECRET_KEY", "ADMIN_INITIAL_PASSWORD", "PUBLISH_ADDR"} {
		if env[k] == "" {
			t.Errorf("%s missing from the generated .env", k)
		}
	}
	if env["PUBLISH_ADDR"] != "127.0.0.1" {
		t.Errorf("an https install sits behind a proxy on the host: PUBLISH_ADDR=%s", env["PUBLISH_ADDR"])
	}
	for _, k := range []string{"SMTP_HOST", "WEBRTC_NAT_1TO1_IP", "COMPOSE_PROFILES", "UPDATER_TOKEN", "TRUSTED_PROXY_CIDRS"} {
		if _, ok := env[k]; ok {
			t.Errorf("%s should not be active by default", k)
		}
	}
	plain, _ := Options{PublicURL: "http://192.168.1.20:8080"}.normalize()
	if env, _ = godotenv.Unmarshal(RenderEnv(plain, NewSecrets(), fixedNow)); env["PUBLISH_ADDR"] != "0.0.0.0" {
		t.Errorf("a plain http install is reached directly: PUBLISH_ADDR=%s", env["PUBLISH_ADDR"])
	}
}

func TestSecretsAreFreshAndStrong(t *testing.T) {
	a, b := NewSecrets(), NewSecrets()
	if a == b || len(a.JWT) < 32 || len(a.AdminPassword) < 14 || len(a.UpdaterToken) < 32 {
		t.Fatalf("weak or repeated secrets: %+v", a)
	}
}

func TestMailSettingsRoundTripWithSpecialCharacters(t *testing.T) {
	o, err := Options{PublicURL: "https://chat.example.com", SMTP: SMTP{Host: "smtp.example.org", Port: "465", TLS: "tls", Username: "me", Password: `pa ss#"w$rd'x`, From: "Mnema Talk <noreply@example.org>"}}.normalize()
	if err != nil {
		t.Fatal(err)
	}
	text := RenderEnv(o, NewSecrets(), fixedNow)
	env, err := godotenv.Unmarshal(text)
	if err != nil {
		t.Fatal(err)
	}
	if env["SMTP_PASSWORD"] != `pa ss#"w$rd'x` || env["SMTP_FROM"] != "Mnema Talk <noreply@example.org>" || env["SMTP_PORT"] != "465" {
		t.Fatalf("mail settings did not survive: %q %q", env["SMTP_PASSWORD"], env["SMTP_FROM"])
	}
	if cfg := asProductionConfig(t, text); !cfg.SMTP.Enabled() {
		t.Error("server does not see the mail setup")
	}
}

func TestRenderComposeDropsTheBuildSection(t *testing.T) {
	out, err := RenderCompose(repoTemplate(t), "ghcr.io/msch128/mnema-talk:1.4")
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, bad := range []string{"build:", "dockerfile:", "mnema-talk:local"} {
		if strings.Contains(s, bad) {
			t.Errorf("generated compose still contains %q", bad)
		}
	}
	if strings.Count(s, "${MNEMA_IMAGE:-ghcr.io/msch128/mnema-talk:1.4}") < 2 {
		t.Error("the app and updater services must default to the released image")
	}
	for _, keep := range []string{"  postgres:", "  seaweedfs:", "  coturn:", "  updater:", "  recreator:", "profiles:"} {
		if !strings.Contains(s, keep) {
			t.Errorf("generated compose lost %q", keep)
		}
	}
	if _, err := RenderCompose([]byte("services: {}\n"), "x"); err == nil {
		t.Error("a foreign template must be refused")
	}
}

// The app container only sees what the compose file lists, so every value the
// wizard writes must be referenced there.
func TestComposePassesEveryVariableTheWizardWrites(t *testing.T) {
	o, err := Options{PublicURL: "https://chat.example.com", AutoUpdate: true, SMTP: SMTP{Host: "smtp.example.org", From: "a@example.org"}}.normalize()
	if err != nil {
		t.Fatal(err)
	}
	env, err := godotenv.Unmarshal(RenderEnv(o, NewSecrets(), fixedNow))
	if err != nil {
		t.Fatal(err)
	}
	compose := string(repoTemplate(t))
	for key := range env {
		if key == "COMPOSE_PROFILES" { // read by docker compose itself
			continue
		}
		if !strings.Contains(compose, "${"+key) {
			t.Errorf("docker-compose.yml never references %s", key)
		}
	}
}

func TestUpdaterOutlivesItsSwapWait(t *testing.T) {
	if !strings.Contains(string(repoTemplate(t)), "stop_grace_period: 3m") {
		t.Error("the updater needs a stop grace period above its 2 minute swap wait")
	}
}

func TestWriteCreatesPrivateFilesAndNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	o := Options{Dir: filepath.Join(dir, "mnema"), PublicURL: "https://chat.example.com"}
	g, err := Write(o, repoTemplate(t), NewSecrets(), fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(g.EnvPath); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf(".env mode: %v %v", fi, err)
	}
	if _, err := os.Stat(g.ComposePath); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(g.EnvPath)
	if _, err := Write(o, repoTemplate(t), NewSecrets(), fixedNow); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second run must refuse, got %v", err)
	}
	if after, _ := os.ReadFile(g.EnvPath); !bytes.Equal(before, after) {
		t.Fatal("a refused run changed the existing .env")
	}
	o.Force = true
	if _, err := Write(o, repoTemplate(t), NewSecrets(), fixedNow); err != nil {
		t.Fatalf("--force: %v", err)
	}
	if after, _ := os.ReadFile(g.EnvPath); bytes.Equal(before, after) {
		t.Fatal("--force kept the old secrets")
	}
}

func TestOptionsValidation(t *testing.T) {
	for name, o := range map[string]Options{
		"no url":         {},
		"path in url":    {PublicURL: "https://chat.example.com/talk"},
		"ftp":            {PublicURL: "ftp://chat.example.com"},
		"bad language":   {PublicURL: "https://a.example.com", Language: "fr"},
		"mail no sender": {PublicURL: "https://a.example.com", SMTP: SMTP{Host: "smtp.example.org"}},
		"mail bad tls":   {PublicURL: "https://a.example.com", SMTP: SMTP{Host: "h", From: "a@b.c", TLS: "ssl"}},
	} {
		if _, err := o.normalize(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	for in, want := range map[string]string{
		"chat.example.com":          "https://chat.example.com",
		"HTTP://192.168.1.2:8080/":  "http://192.168.1.2:8080",
		" https://Chat.example.com": "https://chat.example.com",
	} {
		if got, err := NormalizePublicURL(in); err != nil || got != want {
			t.Errorf("NormalizePublicURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for v, want := range map[string]string{"1.4.2": ImageRepository + ":1.4", "dev": ImageRepository + ":latest", "1.5.0-rc.1": ImageRepository + ":1.5.0-rc.1"} {
		if got := defaultImage(v); got != want {
			t.Errorf("defaultImage(%q) = %q, want %q", v, got, want)
		}
	}
}

func TestPreflightFindings(t *testing.T) {
	o, _ := Options{PublicURL: "https://chat.example.com"}.normalize()
	level := func(fs []Finding, title string) setupcheck.Level {
		for _, f := range fs {
			if f.Title == title {
				return f.Level
			}
		}
		return ""
	}
	all := Preflight(context.Background(), o, fakeProbe())
	for _, title := range []string{"Public address", "DNS", "Web port", "Clock"} {
		if level(all, title) != setupcheck.OK {
			t.Errorf("%s = %s in %+v", title, level(all, title), all)
		}
	}
	if level(all, "Voice ports") != LevelNote {
		t.Error("the UDP reminder is missing")
	}

	p := fakeProbe()
	p.Resolve = func(context.Context, string) ([]netip.Addr, error) { return nil, errors.New("NXDOMAIN") }
	p.PublicIP = func(context.Context) (netip.Addr, error) { return netip.Addr{}, errors.New("no stun") }
	p.Dial = func(context.Context, string) error { return errors.New("refused") }
	p.Skew = func(context.Context) (time.Duration, error) { return -time.Minute, nil }
	bad := Preflight(context.Background(), o, p)
	for _, title := range []string{"Public address", "DNS", "Web port", "Clock"} {
		if level(bad, title) != setupcheck.Warn {
			t.Errorf("%s = %s, want warn", title, level(bad, title))
		}
	}

	p = fakeProbe()
	p.Resolve = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("198.51.100.9")}, nil
	}
	mismatch := Preflight(context.Background(), o, p)
	if level(mismatch, "DNS") != setupcheck.Warn {
		t.Errorf("a record pointing elsewhere = %s", level(mismatch, "DNS"))
	}

	lan, _ := Options{PublicURL: "http://192.168.1.20:8080"}.normalize()
	if level(Preflight(context.Background(), lan, fakeProbe()), "DNS") != "" {
		t.Error("an IP address needs no DNS check")
	}
}
