package config

import (
	"errors"
	"strings"
	"testing"
)

func envOf(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func TestSeveralMissingSettingsAreReportedInOneMessage(t *testing.T) {
	_, err := FromEnv(envOf(map[string]string{"APP_ENV": "production", "PUBLIC_URL": "chat.example.com", "WEBRTC_UDP_PORT_MIN": "abc"}))
	var ce *ConfigError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %T %v", err, err)
	}
	msg := err.Error()
	if strings.Count(msg, "Mnema Talk cannot start") != 1 {
		t.Errorf("not one block:\n%s", msg)
	}
	for _, want := range []string{"DATABASE_URL", "POSTGRES_PASSWORD", "JWT_SECRET", "openssl rand -hex 32", "S3_ACCESS_KEY", "S3_SECRET_KEY", "PUBLIC_URL", "https://chat.example.com", "WEBRTC_UDP_PORT_MIN must be"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message lacks %q:\n%s", want, msg)
		}
	}
	if len(ce.Problems) < 6 {
		t.Errorf("only %d problems: %+v", len(ce.Problems), ce.Problems)
	}
	for _, l := range strings.Split(msg, "\n")[1:] {
		if !strings.HasPrefix(l, "  - ") {
			t.Errorf("line is not a bullet: %q", l)
		}
	}
}

func TestEveryMalformedTypedValueIsListed(t *testing.T) {
	env := base()
	env["SESSION_EXPIRY_HOURS"], env["EMOJI_MAX"] = "x", "y"
	_, err := FromEnv(lookup(env))
	if err == nil || !strings.Contains(err.Error(), "SESSION_EXPIRY_HOURS") || !strings.Contains(err.Error(), "EMOJI_MAX") {
		t.Fatalf("both malformed values must appear: %v", err)
	}
}

func TestPlaceholdersInProductionAreListedTogether(t *testing.T) {
	env := base()
	env["APP_ENV"] = "production"
	env["JWT_SECRET"] = "change-me"
	env["S3_ACCESS_KEY"], env["S3_SECRET_KEY"] = "mnema_admin", "replace_with_secret"
	_, err := FromEnv(lookup(env))
	if err == nil {
		t.Fatal("accepted")
	}
	for _, want := range []string{"JWT_SECRET", "S3_SECRET_KEY still uses the public example value"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("lacks %q: %v", want, err)
		}
	}
}

func TestValidSettingsGiveNoProblems(t *testing.T) {
	if ps := RequiredProblems(lookup(base())); len(ps) != 0 {
		t.Fatalf("%+v", ps)
	}
}
