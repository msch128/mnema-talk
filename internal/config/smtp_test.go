package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func smtpEnv(extra map[string]string) map[string]string {
	env := base()
	env["SMTP_HOST"] = "mail.example.org"
	env["SMTP_FROM"] = "Mnema Talk <noreply@example.org>"
	for k, v := range extra {
		env[k] = v
	}
	return env
}

func TestSMTPIsOffByDefault(t *testing.T) {
	cfg, err := FromEnv(lookup(base()))
	if err != nil || cfg.SMTP.Enabled() {
		t.Fatalf("mail must be off without SMTP_HOST: %v %+v", err, cfg.SMTP)
	}
}

func TestSMTPDefaultsFollowTheTransportMode(t *testing.T) {
	cfg, err := FromEnv(lookup(smtpEnv(nil)))
	if err != nil || cfg.SMTP.TLS != SMTPStartTLS || cfg.SMTP.Port != 587 || !cfg.SMTP.Enabled() {
		t.Fatalf("starttls on 587 expected: %v %+v", err, cfg.SMTP)
	}
	cfg, err = FromEnv(lookup(smtpEnv(map[string]string{"SMTP_TLS": "TLS"})))
	if err != nil || cfg.SMTP.TLS != SMTPImplicit || cfg.SMTP.Port != 465 {
		t.Fatalf("implicit TLS on 465 expected: %v %+v", err, cfg.SMTP)
	}
}

func TestSMTPRefusesUnsafeOrIncompleteSetups(t *testing.T) {
	cases := map[string]map[string]string{
		"credentials over plain mail": {"SMTP_TLS": "none", "SMTP_USERNAME": "u", "SMTP_PASSWORD": "p"},
		"unknown mode":                {"SMTP_TLS": "ssl"},
		"missing sender":              {"SMTP_FROM": " "},
		"sender that is no address":   {"SMTP_FROM": "not an address"},
		"user without password":       {"SMTP_USERNAME": "u"},
		"password without user":       {"SMTP_PASSWORD": "p"},
		"public example password":     {"SMTP_USERNAME": "u", "SMTP_PASSWORD": "replace_with_smtp_password"},
		"host with a path":            {"SMTP_HOST": "mail.example.org/x"},
		"port out of range":           {"SMTP_PORT": "70000"},
	}
	for name, extra := range cases {
		if _, err := FromEnv(lookup(smtpEnv(extra))); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestSMTPPlainMailIsRefusedInProduction(t *testing.T) {
	env := smtpEnv(map[string]string{
		"SMTP_TLS": "none", "APP_ENV": "production", "JWT_SECRET": strings.Repeat("k", 40),
		"S3_ACCESS_KEY": "access-key-value", "S3_SECRET_KEY": "secret-key-value",
	})
	_, err := FromEnv(lookup(env))
	if err == nil || !strings.Contains(err.Error(), "SMTP_TLS=none") {
		t.Fatalf("plain mail must be refused in production: %v", err)
	}
}

func TestSMTPPlainMailIsAllowedInDevelopment(t *testing.T) {
	cfg, err := FromEnv(lookup(smtpEnv(map[string]string{"SMTP_TLS": "none", "SMTP_PORT": "1025"})))
	if err != nil || cfg.SMTP.TLS != SMTPPlain {
		t.Fatalf("plain mail for a local catcher: %v", err)
	}
}

func TestSMTPPasswordFromFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "smtp-password")
	if err := os.WriteFile(file, []byte("  from-file\nsecond line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := FromEnv(lookup(smtpEnv(map[string]string{"SMTP_USERNAME": "u", "SMTP_PASSWORD_FILE": file})))
	if err != nil || cfg.SMTP.Password != "from-file" {
		t.Fatalf("password from file: %v %q", err, cfg.SMTP.Password)
	}
	both := smtpEnv(map[string]string{"SMTP_USERNAME": "u", "SMTP_PASSWORD": "x", "SMTP_PASSWORD_FILE": file})
	if _, err := FromEnv(lookup(both)); err == nil {
		t.Error("the variable and the file together are ambiguous")
	}
	missing := smtpEnv(map[string]string{"SMTP_USERNAME": "u", "SMTP_PASSWORD_FILE": filepath.Join(dir, "nope")})
	_, err = FromEnv(lookup(missing))
	if err == nil || strings.Contains(err.Error(), dir) {
		t.Errorf("an unreadable file is an error that does not echo its path: %v", err)
	}
}
