package config

import "testing"

func turnConfig(t *testing.T, kv map[string]string) *Config {
	t.Helper()
	env := base()
	for k, v := range kv {
		env[k] = v
	}
	cfg, err := FromEnv(lookup(env))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

const external = "turn:turn.example.test:3478?transport=udp"

func TestBuiltinTURNSwitch(t *testing.T) {
	secret := "replace_with_turn_secret_value"
	cases := []struct {
		name        string
		env         map[string]string
		enabled     bool
		withExtern  bool
		wantDefault bool
	}{
		{"default without external TURN runs", nil, true, false, true},
		{"auto without external TURN runs", map[string]string{"WEBRTC_BUILTIN_TURN": "auto"}, true, false, false},
		{"default with external TURN stays off", map[string]string{"WEBRTC_TURN_URLS": external, "WEBRTC_TURN_SECRET": secret}, false, false, true},
		{"auto with external TURN stays off", map[string]string{"WEBRTC_BUILTIN_TURN": "auto", "WEBRTC_TURN_URLS": external, "WEBRTC_TURN_SECRET": secret}, false, false, false},
		{"on without external TURN runs", map[string]string{"WEBRTC_BUILTIN_TURN": "on"}, true, false, false},
		{"on with external TURN offers both", map[string]string{"WEBRTC_BUILTIN_TURN": "on", "WEBRTC_TURN_URLS": external, "WEBRTC_TURN_SECRET": secret}, true, true, false},
		{"off never runs", map[string]string{"WEBRTC_BUILTIN_TURN": "off"}, false, false, false},
		{"off with external TURN", map[string]string{"WEBRTC_BUILTIN_TURN": "off", "WEBRTC_TURN_URLS": external, "WEBRTC_TURN_SECRET": secret}, false, false, false},
		{"value is case-insensitive", map[string]string{"WEBRTC_BUILTIN_TURN": "OFF"}, false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := turnConfig(t, tc.env)
			if tc.wantDefault && cfg.WebRTCBuiltinTURN != BuiltinTURNAuto {
				t.Errorf("mode = %q, want the default auto", cfg.WebRTCBuiltinTURN)
			}
			if got := cfg.BuiltinTURNEnabled(); got != tc.enabled {
				t.Errorf("BuiltinTURNEnabled = %v, want %v", got, tc.enabled)
			}
			if got := cfg.BuiltinTURNWithExternal(); got != tc.withExtern {
				t.Errorf("BuiltinTURNWithExternal = %v, want %v", got, tc.withExtern)
			}
		})
	}
}

func TestBuiltinTURNSettingsAreValidated(t *testing.T) {
	for _, bad := range []map[string]string{
		{"WEBRTC_BUILTIN_TURN": "yes"},
		{"WEBRTC_BUILTIN_TURN": "true"},
		{"WEBRTC_BUILTIN_TURN": "on", "WEBRTC_BUILTIN_TURN_PORT": "0"},
		{"WEBRTC_BUILTIN_TURN": "on", "WEBRTC_BUILTIN_TURN_PORT": "50010"}, // inside the media range
		{"WEBRTC_BUILTIN_TURN": "auto", "WEBRTC_BUILTIN_TURN_HOST": "https://turn.example.com/"},
		{"WEBRTC_BUILTIN_TURN": "on", "WEBRTC_BUILTIN_TURN_HOST": "https://turn.example.com"},
		{"WEBRTC_BUILTIN_TURN": "on", "WEBRTC_BUILTIN_TURN_HOST": "turn.example.com:3478"},
		{"WEBRTC_BUILTIN_TURN": "on", "WEBRTC_BUILTIN_TURN_PORT": "abc"},
	} {
		env := base()
		for k, v := range bad {
			env[k] = v
		}
		if _, err := FromEnv(lookup(env)); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
	// An installation that never asked for the relay keeps starting, whatever
	// its port range looks like: the relay itself declines when it cannot run.
	if _, err := FromEnv(lookup(map[string]string{"DATABASE_URL": "postgres://x", "WEBRTC_UDP_PORT_MIN": "3470", "WEBRTC_UDP_PORT_MAX": "3500"})); err != nil {
		t.Errorf("auto with 3478 inside the media range must not fail the start: %v", err)
	}
	// A malformed port only matters while the relay is explicitly switched on.
	for _, env := range []map[string]string{
		{"WEBRTC_BUILTIN_TURN_PORT": "abc"},
		{"WEBRTC_BUILTIN_TURN_PORT": "70000"},
		{"WEBRTC_BUILTIN_TURN": "off", "WEBRTC_BUILTIN_TURN_PORT": "abc"},
		{"WEBRTC_BUILTIN_TURN": "off", "WEBRTC_BUILTIN_TURN_HOST": "https://turn.example.com"},
	} {
		if cfg := turnConfig(t, env); cfg.WebRTCBuiltinTURN != "off" && cfg.WebRTCBuiltinTURNPort != 3479 {
			t.Errorf("%v: port = %d, want the default after a warning", env, cfg.WebRTCBuiltinTURNPort)
		}
	}
	cfg := turnConfig(t, nil)
	if cfg.WebRTCBuiltinTURN != "auto" || cfg.WebRTCBuiltinTURNPort != 3479 {
		t.Errorf("defaults = %q %d, want auto 3479", cfg.WebRTCBuiltinTURN, cfg.WebRTCBuiltinTURNPort)
	}
}

func TestTURNPublicHost(t *testing.T) {
	for _, tc := range []struct {
		name, url string
		announce  []string
		want      string
	}{
		{"public url host", "https://talk.example.com", nil, "talk.example.com"},
		{"port and path are dropped", "https://talk.example.com:8443/app", []string{"198.51.100.4"}, "talk.example.com"},
		{"localhost falls back to the announced address", "http://localhost:8080", []string{"198.51.100.4", "192.168.1.5"}, "198.51.100.4"},
		{"loopback address falls back too", "http://127.0.0.1:8080", []string{"198.51.100.4"}, "198.51.100.4"},
		{"localhost without announce stays", "http://localhost:8080", nil, "localhost"},
		{"ip literal", "http://192.168.1.5:8080", nil, "192.168.1.5"},
		{"explicit host wins", "https://talk.example.com", []string{"198.51.100.4"}, "turn.example.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &Config{PublicURL: tc.url, WebRTCAnnounce: tc.announce}
			if tc.name == "explicit host wins" {
				c.WebRTCBuiltinTURNHost = "turn.example.com"
			}
			if got := c.TURNPublicHost(); got != tc.want {
				t.Errorf("TURNPublicHost = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReachabilityWarningKnowsTheRelay(t *testing.T) {
	c := &Config{AppEnv: "production", WebRTCBuiltinTURN: BuiltinTURNAuto}
	if c.WebRTCReachabilityWarning() != "" {
		t.Error("warned although the built-in relay covers members outside the host")
	}
	if c.WebRTCReachabilityWarningWith(false) == "" {
		t.Error("no warning when the relay could not start")
	}
	c.WebRTCBuiltinTURN = BuiltinTURNOff
	if c.WebRTCReachabilityWarning() == "" {
		t.Error("no warning with the relay off and nothing announced")
	}
}
