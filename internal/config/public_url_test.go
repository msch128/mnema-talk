package config

import (
	"strings"
	"testing"
)

// PUBLIC_URL is the one address an operator must set; everything below
// follows from it unless the specific variable is set explicitly.
func TestPublicURLDerivesTheRest(t *testing.T) {
	env := prodEnv()
	env["PUBLIC_URL"] = "https://Chat.Example.com:443/"
	cfg, err := FromEnv(lookup(env))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(cfg.AllowedOrigins, ",") != "https://chat.example.com" {
		t.Errorf("allowed origins = %v, want the public origin", cfg.AllowedOrigins)
	}
	if !cfg.SecureCookies() {
		t.Error("an https PUBLIC_URL must turn on Secure cookies")
	}
	if cfg.PublicHost() != "chat.example.com" {
		t.Errorf("PublicHost = %q", cfg.PublicHost())
	}
	if !cfg.WebRTCAutoAnnounce || len(cfg.WebRTCDetectSTUN) < 2 {
		t.Errorf("announce detection should default on with public STUN servers: %v %v", cfg.WebRTCAutoAnnounce, cfg.WebRTCDetectSTUN)
	}
	if !cfg.ClockCheck {
		t.Error("the clock check should default to on")
	}
}

func TestExplicitSettingsBeatPublicURL(t *testing.T) {
	env := prodEnv()
	env["PUBLIC_URL"] = "https://chat.example.com"
	env["CORS_ALLOWED_ORIGINS"] = "https://other.example.org"
	env["WEBRTC_NAT_1TO1_IP"] = "203.0.113.5"
	env["WEBRTC_AUTO_ANNOUNCE"] = "false"
	env["WEBRTC_DETECT_STUN_URLS"] = "stun:stun.internal:3478"
	env["CLOCK_CHECK_ENABLED"] = "false"
	cfg, err := FromEnv(lookup(env))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(cfg.AllowedOrigins, ",") != "https://other.example.org" {
		t.Errorf("explicit CORS_ALLOWED_ORIGINS must win, got %v", cfg.AllowedOrigins)
	}
	if strings.Join(cfg.WebRTCAnnounce, ",") != "203.0.113.5" || cfg.WebRTCAutoAnnounce || cfg.ClockCheck {
		t.Errorf("explicit voice and clock settings changed: %v %v %v", cfg.WebRTCAnnounce, cfg.WebRTCAutoAnnounce, cfg.ClockCheck)
	}
	if strings.Join(cfg.WebRTCDetectSTUN, ",") != "stun:stun.internal:3478" {
		t.Errorf("WEBRTC_DETECT_STUN_URLS = %v", cfg.WebRTCDetectSTUN)
	}
}

func TestPublicIPv4AndLocalURL(t *testing.T) {
	for url, want := range map[string]string{
		"http://192.168.1.20:8080": "192.168.1.20",
		"https://203.0.113.4":      "203.0.113.4",
		"http://localhost:8080":    "",
		"http://127.0.0.1:8080":    "",
		"https://chat.example.com": "",
		"http://[2001:db8::1]:80":  "",
	} {
		env := base()
		env["PUBLIC_URL"] = url
		cfg, err := FromEnv(lookup(env))
		if err != nil {
			t.Fatalf("%s: %v", url, err)
		}
		got, ok := cfg.PublicIPv4()
		if (want == "") == ok || (ok && got.String() != want) {
			t.Errorf("%s: PublicIPv4 = %v %v, want %q", url, got, ok, want)
		}
	}
	for url, want := range map[string]bool{
		"http://localhost:8080": true, "http://127.0.0.1": true,
		"http://[::1]:8080": true, "https://chat.example.com": false, "http://192.168.1.2": false,
	} {
		env := base()
		env["PUBLIC_URL"] = url
		cfg, _ := FromEnv(lookup(env))
		if cfg.PublicURLIsLocal() != want {
			t.Errorf("%s: PublicURLIsLocal = %v, want %v", url, cfg.PublicURLIsLocal(), want)
		}
	}
}

func TestDetectsPublicAddressOnlyWhenNothingIsPinned(t *testing.T) {
	for name, tc := range map[string]struct {
		env  map[string]string
		want bool
	}{
		"production default": {nil, true},
		"pinned address":     {map[string]string{"WEBRTC_NAT_1TO1_IP": "203.0.113.5"}, false},
		"switched off":       {map[string]string{"WEBRTC_AUTO_ANNOUNCE": "false"}, false},
	} {
		env := prodEnv()
		for k, v := range tc.env {
			env[k] = v
		}
		cfg, err := FromEnv(lookup(env))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.DetectsPublicAddress() != tc.want {
			t.Errorf("%s: DetectsPublicAddress = %v", name, !tc.want)
		}
	}
	cfg, _ := FromEnv(lookup(base()))
	if cfg.DetectsPublicAddress() {
		t.Error("development never probes")
	}
}
