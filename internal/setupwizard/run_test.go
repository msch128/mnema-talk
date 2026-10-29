package setupwizard

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"
)

func runSetup(t *testing.T, input string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := Run(context.Background(), args, Env{In: strings.NewReader(input), Out: &out, Probe: fakeProbe(), Now: func() time.Time { return fixedNow }})
	return out.String(), err
}

func TestNonInteractiveSetupWritesBothFiles(t *testing.T) {
	dir := t.TempDir()
	out, err := runSetup(t, "", "-y", "--dir", dir, "--public-url", "chat.example.com", "--compose-template", repoCompose, "--smtp-host", "smtp.example.org", "--smtp-from", "noreply@example.org")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := asProductionConfig(t, string(raw))
	if !cfg.SMTP.Enabled() || cfg.PublicURL != "https://chat.example.com" {
		t.Errorf("config from generated .env: %+v", cfg.SMTP)
	}
	env, _ := godotenv.Unmarshal(string(raw))
	for _, want := range []string{"[ ok ] DNS", "[note] Voice ports", env["ADMIN_INITIAL_PASSWORD"], "docker compose up -d --wait", "docker compose pull && docker compose up -d"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, env["JWT_SECRET"]) || strings.Contains(out, env["POSTGRES_PASSWORD"]) {
		t.Error("the summary prints secrets other than the Owner's first password")
	}
}

func TestNonInteractiveNeedsThePublicURL(t *testing.T) {
	if _, err := runSetup(t, "", "-y", "--dir", t.TempDir()); err == nil || !strings.Contains(err.Error(), "--public-url") {
		t.Fatalf("err = %v", err)
	}
}

func TestInteractiveSetupAsksForTheRest(t *testing.T) {
	dir := t.TempDir()
	answers := strings.Join([]string{
		"not a url ://", // rejected, asked again
		"https://talk.example.org",
		"",                                                                             // owner name: default
		"de",                                                                           // language
		"y",                                                                            // mail
		"smtp.example.org", "", "", "mailer", "Talk <talk@example.org>", "s3cret-smtp", // host, port, tls, user, from, password
		"y", // updater sidecar
	}, "\n") + "\n"
	out, err := runSetup(t, answers, "--dir", dir, "--compose-template", repoCompose, "--skip-preflight")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, ".env"))
	cfg := asProductionConfig(t, string(raw))
	if cfg.PublicURL != "https://talk.example.org" || cfg.AdminUsername != "admin" || cfg.InitialLanguage != "de" ||
		cfg.SMTP.Host != "smtp.example.org" || cfg.SMTP.Username != "mailer" || cfg.SMTP.Password != "s3cret-smtp" || !cfg.SelfUpdateConfigured() {
		t.Errorf("answers were not applied: %+v", cfg)
	}
	if strings.Contains(out, "Checking what can be checked") {
		t.Error("--skip-preflight still ran the preflight")
	}
	if !strings.Contains(out, "is not an http(s) address") {
		t.Errorf("an invalid address got no explanation:\n%s", out)
	}
}

func TestInteractiveSetupWithoutInputExplainsWhy(t *testing.T) {
	_, err := runSetup(t, "", "--dir", t.TempDir(), "--compose-template", repoCompose)
	if err == nil || !strings.Contains(err.Error(), "--non-interactive") {
		t.Fatalf("err = %v", err)
	}
}

func TestSetupRefusesToReplaceAnInstallation(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("JWT_SECRET=old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := runSetup(t, "", "-y", "--dir", dir, "--public-url", "https://chat.example.com", "--compose-template", repoCompose, "--skip-preflight")
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("err = %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, ".env")); string(b) != "JWT_SECRET=old\n" {
		t.Fatal("the existing .env was touched")
	}
}

func TestSetupUsageErrors(t *testing.T) {
	for _, args := range [][]string{{"--bogus"}, {"extra"}, {"-h"}} {
		if _, err := runSetup(t, "", args...); err == nil || !strings.Contains(err.Error(), "usage: mnema-talk setup") {
			t.Errorf("%v: err = %v", args, err)
		}
	}
}

func TestSetupWritesToTheCurrentDirectoryByDefault(t *testing.T) {
	if fi, err := os.Stat("/out"); err == nil && fi.IsDir() {
		t.Skip("/out exists on this machine and is the default")
	}
	template, err := filepath.Abs(repoCompose)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	t.Chdir(dir)
	if _, err := runSetup(t, "", "-y", "--public-url", "https://chat.example.com", "--compose-template", template, "--skip-preflight"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".env")); err != nil {
		t.Fatal(err)
	}
}
