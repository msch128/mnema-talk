package setupcheck

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/opwarn"
)

func prodConfig(t *testing.T, extra map[string]string) *config.Config {
	t.Helper()
	env := map[string]string{
		"APP_ENV":       "production",
		"DATABASE_URL":  "postgres://mnema:s3cret-db-pass@postgres:5432/mnema_talk",
		"JWT_SECRET":    strings.Repeat("k", 32),
		"S3_ACCESS_KEY": "mnema_admin",
		"S3_SECRET_KEY": "a-real-secret-key",
		"PUBLIC_URL":    "https://chat.example.com",
	}
	for k, v := range extra {
		env[k] = v
	}
	cfg, err := config.FromEnv(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func healthy(cfg *config.Config) Input {
	rt := &Runtime{}
	rt.SetAnnounce(SourceDetected, []string{"203.0.113.7"}, "")
	return Input{Cfg: cfg, Runtime: rt, DBReachable: true, StorageConfigured: true, TURNRunning: true}
}

func byID(checks []Check, id string) Check {
	for _, c := range checks {
		if c.ID == id {
			return c
		}
	}
	return Check{}
}

func TestHealthyInstallHasOnlyOptionalAmbers(t *testing.T) {
	cfg := prodConfig(t, map[string]string{"SMTP_HOST": "smtp.example.org", "SMTP_FROM": "noreply@example.org", "WEBRTC_TURN_URLS": "turn:chat.example.com:3478", "WEBRTC_TURN_SECRET": "turn-secret-value-123456"})
	in := healthy(cfg)
	in.RequestOrigin = "https://chat.example.com/"
	checks := Run(in)
	if w := Worst(checks); w != OK {
		t.Fatalf("worst = %s: %+v", w, checks)
	}
	for _, c := range checks {
		if c.Fix != "" && c.Level == OK {
			t.Errorf("ok check %s carries a fix: %q", c.ID, c.Fix)
		}
	}
}

func TestEveryProblemNamesTheVariableToSet(t *testing.T) {
	cfg := prodConfig(t, map[string]string{"PUBLIC_URL": "http://localhost:8080", "WEBRTC_BUILTIN_TURN": "off"})
	in := Input{Cfg: cfg, Runtime: &Runtime{}, StorageConfigured: true, StorageErr: errors.New("dial seaweedfs:8333")}
	checks := Run(in)
	want := map[string]struct {
		level Level
		fix   string
	}{
		PublicURL:    {Warn, "PUBLIC_URL="}, // a trial on this computer is legitimate: amber, not red
		VoiceAddress: {Fail, "WEBRTC_NAT_1TO1_IP="},
		Turn:         {Warn, "WEBRTC_BUILTIN_TURN=off"},
		Mail:         {Warn, "SMTP_HOST"},
		Storage:      {Fail, "S3_ACCESS_KEY"},
		Database:     {Fail, "POSTGRES_PASSWORD"},
	}
	for id, w := range want {
		c := byID(checks, id)
		if c.Level != w.level || !strings.Contains(c.Fix, w.fix) {
			t.Errorf("%s = %+v, want level %s and fix containing %q", id, c, w.level, w.fix)
		}
	}
	if strings.Contains(byID(checks, Storage).Detail, "seaweedfs:8333") {
		t.Error("the storage check leaks the internal address")
	}
	if Worst(checks) != Fail {
		t.Error("worst must be fail")
	}
}

func TestVoiceAddressWithTurnOnlyIsAmber(t *testing.T) {
	cfg := prodConfig(t, map[string]string{"WEBRTC_TURN_URLS": "turn:chat.example.com:3478", "WEBRTC_TURN_SECRET": "x-secret-value-123456"})
	in := healthy(cfg)
	in.Runtime = &Runtime{}
	in.Runtime.SetAnnounce(SourceNone, nil, "stun.example: no answer")
	c := byID(Run(in), VoiceAddress)
	if c.Level != Warn || !strings.Contains(c.Detail, "no answer") {
		t.Fatalf("voice address = %+v", c)
	}
}

func TestVoiceAddressIsNotJudgedInDevelopment(t *testing.T) {
	cfg, err := config.FromEnv(func(k string) (string, bool) { return "postgres://x", k == "DATABASE_URL" })
	if err != nil {
		t.Fatal(err)
	}
	if c := byID(Run(Input{Cfg: cfg}), VoiceAddress); c.Level != OK {
		t.Fatalf("development voice address = %+v", c)
	}
}

func TestOperatorWarningsFeedTheChecks(t *testing.T) {
	cfg := prodConfig(t, nil)
	in := healthy(cfg)
	in.Warnings = []opwarn.Warning{{Code: opwarn.VoiceUnavailable}, {Code: opwarn.ProxyNotTrusted}}
	checks := Run(in)
	if c := byID(checks, VoiceUDP); c.Level != Fail || !strings.Contains(c.Fix, "50000-50050") {
		t.Errorf("voice udp = %+v", c)
	}
	if c := byID(checks, ReverseProxy); c.Level != Warn || !strings.Contains(c.Fix, "TRUSTED_PROXY_CIDRS") {
		t.Errorf("reverse proxy = %+v", c)
	}
	in.Warnings = []opwarn.Warning{{Code: opwarn.WebRTCUDPMuxDisabled}, {Code: opwarn.TrustedProxyBroad}}
	checks = Run(in)
	if c := byID(checks, VoiceUDP); c.Level != Warn || !strings.Contains(c.Fix, "WEBRTC_UDP_MUX_PORT") {
		t.Errorf("mux disabled = %+v", c)
	}
	if c := byID(checks, ReverseProxy); c.Level != Warn {
		t.Errorf("broad proxies = %+v", c)
	}
}

func TestPublicURLChecks(t *testing.T) {
	plain := prodConfig(t, map[string]string{"PUBLIC_URL": "http://192.168.1.20:8080"})
	if c := byID(Run(healthy(plain)), PublicURL); c.Level != Warn || !strings.Contains(c.Fix, "https://") {
		t.Errorf("plain http = %+v", c)
	}
	cfg := prodConfig(t, nil)
	in := healthy(cfg)
	in.RequestOrigin = "http://192.168.1.20:8080"
	c := byID(Run(in), PublicURL)
	if c.Level != Warn || !strings.Contains(c.Fix, "PUBLIC_URL=http://192.168.1.20:8080") || !strings.Contains(c.Fix, "X-Forwarded-Proto") {
		t.Errorf("origin mismatch = %+v", c)
	}
	for a, b := range map[string]string{"https://CHAT.example.com:443/": "https://chat.example.com", "http://x.org:80": "http://x.org"} {
		if !sameOrigin(a, b) {
			t.Errorf("sameOrigin(%q, %q) = false", a, b)
		}
	}
	if sameOrigin("http://chat.example.com", "https://chat.example.com") {
		t.Error("scheme differences must not match")
	}
}

func TestClockThresholds(t *testing.T) {
	cfg := prodConfig(t, nil)
	for skew, want := range map[time.Duration]Level{
		time.Second:             OK,
		-2 * time.Second:        OK,
		9 * time.Second:         Warn,
		-45 * time.Second:       Fail,
		5 * time.Minute:         Fail,
		4999 * time.Millisecond: OK,
	} {
		in := healthy(cfg)
		in.Runtime.SetClockSkew(skew)
		c := byID(Run(in), Clock)
		if c.Level != want {
			t.Errorf("skew %s: %+v, want %s", skew, c, want)
		}
		if want != OK && !strings.Contains(c.Fix, "NTP") {
			t.Errorf("skew %s: fix %q does not mention NTP", skew, c.Fix)
		}
	}
	if c := byID(Run(healthy(cfg)), Clock); c.ID != "" {
		t.Errorf("an unmeasured clock must not be listed: %+v", c)
	}
}

func TestLogListsOnlyProblemsWithTheirFix(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	Log(logger, []Check{
		{ID: "a", Level: OK, Detail: "fine"},
		{ID: "b", Level: Warn, Detail: "meh", Fix: "set B=1"},
		{ID: "c", Level: Fail, Detail: "broken", Fix: "set C=2"},
	})
	out := buf.String()
	if strings.Contains(out, "fine") || !strings.Contains(out, "set B=1") || !strings.Contains(out, "set C=2") || !strings.Contains(out, `"worst":"fail"`) {
		t.Fatalf("log = %s", out)
	}
}

func detector(ip string, err error) Detector {
	return func(context.Context, []string, time.Duration) (netip.Addr, string, error) {
		if err != nil {
			return netip.Addr{}, "", err
		}
		return netip.MustParseAddr(ip), "stun.test:3478", nil
	}
}

func TestResolveAnnouncement(t *testing.T) {
	fail := detector("", errors.New("no answer"))
	for name, tc := range map[string]struct {
		env     map[string]string
		detect  Detector
		entries string
		source  string
		errPart string
	}{
		"explicit variable wins and skips detection": {map[string]string{"WEBRTC_NAT_1TO1_IP": "nas.example.org"}, detector("203.0.113.7", nil), "nas.example.org", SourceEnv, ""},
		"public host name is detected":               {nil, detector("203.0.113.7", nil), "203.0.113.7", SourceDetected, ""},
		"detection can be switched off":              {map[string]string{"WEBRTC_AUTO_ANNOUNCE": "false"}, detector("203.0.113.7", nil), "", SourceNone, ""},
		"failed detection is reported":               {nil, fail, "", SourceNone, "no answer"},
		"public IP in PUBLIC_URL needs no detection": {map[string]string{"PUBLIC_URL": "https://198.51.100.4"}, fail, "198.51.100.4", SourcePublicURL, ""},
		"LAN IP in PUBLIC_URL gets the public one":   {map[string]string{"PUBLIC_URL": "http://192.168.1.20:8080"}, detector("203.0.113.7", nil), "192.168.1.20,203.0.113.7", SourceDetected, ""},
		"LAN IP alone when detection fails":          {map[string]string{"PUBLIC_URL": "http://192.168.1.20:8080"}, fail, "192.168.1.20", SourcePublicURL, "no answer"},
		"development never probes":                   {map[string]string{"APP_ENV": "development", "PUBLIC_URL": "http://localhost:8080", "BIND_ADDR": "127.0.0.1"}, fail, "", SourceNone, ""},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := prodConfig(t, tc.env)
			got := ResolveAnnouncement(context.Background(), cfg, tc.detect, Host{})
			if strings.Join(got.Entries, ",") != tc.entries || got.Source != tc.source || !strings.Contains(got.DetectErr, tc.errPart) || (tc.errPart == "" && got.DetectErr != "") {
				t.Fatalf("got %+v, want entries %q source %s err %q", got, tc.entries, tc.source, tc.errPart)
			}
		})
	}
}

func TestMeasureClockSkewAgainstAFakeNTPServer(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	ahead := 42 * time.Second // the fake server runs this far ahead of this host
	go func() {
		buf := make([]byte, 64)
		for {
			_, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			now := time.Now().Add(ahead)
			resp := make([]byte, 48)
			resp[0], resp[1] = 0x1C, 2 // server mode, stratum 2
			binary.BigEndian.PutUint32(resp[40:], uint32(now.Unix()+ntpEpochOffset))
			binary.BigEndian.PutUint32(resp[44:], uint32(uint64(now.Nanosecond())<<32/1_000_000_000))
			_, _ = pc.WriteTo(resp, from)
		}
	}()
	skew, err := MeasureClockSkew(context.Background(), []string{pc.LocalAddr().String()}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if d := skew + ahead; d > time.Second || d < -time.Second {
		t.Fatalf("skew = %s, want about %s", skew, -ahead)
	}
	if _, err := MeasureClockSkew(context.Background(), nil, time.Second); err == nil {
		t.Error("want an error without servers")
	}
}

func TestTurnCheckStates(t *testing.T) {
	ext := map[string]string{"WEBRTC_TURN_URLS": "turn:chat.example.com:3478", "WEBRTC_TURN_SECRET": "turn-secret-value-123456"}
	unavailable := []opwarn.Warning{{Code: opwarn.TURNUnavailable}}
	cases := []struct {
		name   string
		env    map[string]string
		warns  []opwarn.Warning
		probe  string
		level  Level
		detail string
		fix    string
	}{
		{"built-in by default", nil, nil, "", OK, "built-in TURN relay serves members", ""},
		{"probe reachable", nil, nil, "reachable", OK, "port 3479", ""},
		{"probe unreachable", nil, nil, "unreachable", Info, "could not verify port 3479", "Test my connection"},
		{"custom port", map[string]string{"WEBRTC_BUILTIN_TURN_PORT": "3500"}, nil, "", OK, "port 3500", ""},
		{"port taken", nil, unavailable, "", Warn, "could not listen on port 3479", "WEBRTC_BUILTIN_TURN_PORT"},
		{"external replaces built-in in auto", ext, nil, "unreachable", OK, "A TURN relay is configured", ""},
		{"forced on beside external, port taken", mergeEnv(ext, map[string]string{"WEBRTC_BUILTIN_TURN": "on"}), unavailable, "", OK, "could not start", ""},
		{"switched off", map[string]string{"WEBRTC_BUILTIN_TURN": "off"}, nil, "", Warn, "No TURN relay", "WEBRTC_BUILTIN_TURN=off"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := healthy(prodConfig(t, tc.env))
			in.Warnings, in.TURNProbe, in.TURNRunning = tc.warns, tc.probe, true
			c := byID(Run(in), Turn)
			if c.Level != tc.level || !strings.Contains(c.Detail, tc.detail) || !strings.Contains(c.Fix, tc.fix) || (tc.fix == "" && c.Fix != "") {
				t.Errorf("turn = %+v, want level %s, detail containing %q, fix containing %q", c, tc.level, tc.detail, tc.fix)
			}
		})
	}
}

func mergeEnv(a, b map[string]string) map[string]string {
	m := map[string]string{}
	for _, x := range []map[string]string{a, b} {
		for k, v := range x {
			m[k] = v
		}
	}
	return m
}

func TestTurnCheckWarnsWhenTheBuiltInRelayDidNotStart(t *testing.T) {
	in := healthy(prodConfig(t, nil))
	in.TURNRunning = false
	c := byID(Run(in), Turn)
	if c.Level != Warn || !strings.Contains(c.Detail, "not running because voice is unavailable") {
		t.Errorf("turn = %+v, want an amber check that says voice is unavailable", c)
	}
	in = healthy(prodConfig(t, map[string]string{"WEBRTC_TURN_URLS": "turn:chat.example.com:3478", "WEBRTC_TURN_SECRET": "turn-secret-value-123456", "WEBRTC_BUILTIN_TURN": "on"}))
	in.TURNRunning = false
	if c := byID(Run(in), Turn); c.Level != OK {
		t.Errorf("an external relay keeps the check green, got %+v", c)
	}
}

func TestEveryCheckOutcomeHasAUniqueCodeAndEnglishText(t *testing.T) {
	codes := Codes()
	seen := map[string]bool{}
	for _, c := range codes {
		if seen[c] {
			t.Errorf("code %q listed twice", c)
		}
		seen[c] = true
		m := messages[c]
		if m.detail == "" {
			t.Errorf("code %q has no English detail", c)
		}
		if !strings.Contains(c, ".") {
			t.Errorf("code %q is not <check>.<outcome>", c)
		}
	}
}

// TestCodesMatchTheSharedList keeps api/status-codes.json, which the web
// app's translation test reads, equal to the codes the server can send.
// Run with UPDATE_STATUS_CODES=1 to rewrite it after adding a code.
func TestCodesMatchTheSharedList(t *testing.T) {
	const path = "../../api/status-codes.json"
	want, err := json.MarshalIndent(map[string][]string{"setup_checks": Codes(), "operator_warnings": opwarn.Codes()}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want = append(want, '\n')
	if os.Getenv("UPDATE_STATUS_CODES") != "" {
		if err := os.WriteFile(path, want, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("%s is out of date (err %v); run UPDATE_STATUS_CODES=1 go test ./internal/setupcheck and add the translations in web/src/i18n", path, err)
	}
}

func TestRunReturnsOnlyKnownCodesWithTheirParams(t *testing.T) {
	good := healthy(prodConfig(t, nil))
	bad := Input{Cfg: prodConfig(t, map[string]string{"PUBLIC_URL": "http://localhost:8080"}), RequestOrigin: "https://other.example.com", Warnings: []opwarn.Warning{{Code: opwarn.VoiceUnavailable}, {Code: opwarn.ProxyNotTrusted}}}
	for _, in := range []Input{good, bad} {
		for _, c := range Run(in) {
			if _, ok := messages[c.Code]; !ok {
				t.Errorf("check %s: unknown code %q", c.ID, c.Code)
			}
			if !strings.HasPrefix(c.Code, c.ID+".") {
				t.Errorf("check %s: code %q does not belong to it", c.ID, c.Code)
			}
			if strings.Contains(c.Detail+c.Fix, "{") {
				t.Errorf("check %s: unfilled placeholder in %q / %q", c.ID, c.Detail, c.Fix)
			}
		}
	}
}
