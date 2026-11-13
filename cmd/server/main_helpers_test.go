package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/opwarn"
	"github.com/msch128/mnema-talk/internal/setupcheck"
	"github.com/msch128/mnema-talk/internal/sfu"
	"github.com/msch128/mnema-talk/internal/turnrelay"
)

// captureLog routes the default logger into a buffer for the test and clears
// the warning codes it may raise, before and after.
func captureLog(t *testing.T, codes ...string) *syncBuffer {
	t.Helper()
	buf := &syncBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, nil)))
	reset := func() {
		for _, c := range codes {
			opwarn.Default.Clear(c)
		}
	}
	reset()
	t.Cleanup(func() { slog.SetDefault(prev); reset() })
	return buf
}

func warningValue(code string) (int, bool) {
	for _, w := range opwarn.Default.List() {
		if w.Code == code {
			return w.Value, true
		}
	}
	return 0, false
}

func TestWarnBroadTrustedProxies(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  config.Config
		want bool
	}{
		{"broad range on a public listener", config.Config{BindAddr: "0.0.0.0", TrustedProxies: []string{"10.0.0.0/8"}}, true},
		{"broad range on a loopback listener", config.Config{BindAddr: "127.0.0.1", TrustedProxies: []string{"10.0.0.0/8"}}, false},
		{"a single proxy address", config.Config{BindAddr: "0.0.0.0", TrustedProxies: []string{"172.18.0.2/32"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureLog(t, opwarn.TrustedProxyBroad)
			warnBroadTrustedProxies(&tc.cfg)
			if _, raised := warningValue(opwarn.TrustedProxyBroad); raised != tc.want {
				t.Fatalf("warning raised = %v, want %v", raised, tc.want)
			}
			if got := strings.Contains(logs.String(), "TRUSTED_PROXY_CIDRS"); got != tc.want {
				t.Fatalf("logged = %v, want %v: %s", got, tc.want, logs.String())
			}
		})
	}
}

func TestWarnInsecurePublicURL(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  config.Config
		want bool
	}{
		{"production over http", config.Config{AppEnv: "production", PublicURL: "http://chat.example.com"}, true},
		{"production over https", config.Config{AppEnv: "production", PublicURL: "https://chat.example.com"}, false},
		{"development over http", config.Config{AppEnv: "development", PublicURL: "http://localhost:8080"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureLog(t, opwarn.InsecurePublicURL)
			warnInsecurePublicURL(&tc.cfg)
			if _, raised := warningValue(opwarn.InsecurePublicURL); raised != tc.want {
				t.Fatalf("warning raised = %v, want %v", raised, tc.want)
			}
			if got := strings.Contains(logs.String(), "PUBLIC_URL is not https://"); got != tc.want {
				t.Fatalf("logged = %v, want %v: %s", got, tc.want, logs.String())
			}
		})
	}
}

func TestIsServerStart(t *testing.T) {
	for args, want := range map[string]bool{
		"":            true,
		"-version":    true,
		"serve":       true,
		"setup":       false,
		"doctor":      false,
		"unlock":      false,
		"reset-2fa":   false,
		"rotate-keys": false,
	} {
		argv := []string{"mnema-talk"}
		if args != "" {
			argv = append(argv, args)
		}
		if got := isServerStart(argv); got != want {
			t.Errorf("%q: %v, want %v", args, got, want)
		}
	}
}

func TestReportConfigErrorPausesOnlyForAServerStart(t *testing.T) {
	prev := configErrorDelay
	configErrorDelay = 0
	t.Cleanup(func() { configErrorDelay = prev })
	ce := &config.ConfigError{}
	for _, server := range []bool{true, false} {
		var out bytes.Buffer
		if code := reportConfigError(&out, ce, server); code != config.ExitCode {
			t.Fatalf("server=%v exit code %d", server, code)
		}
		if got := strings.Contains(out.String(), "docker compose up -d"); got != server {
			t.Fatalf("server=%v: restart hint present = %v: %q", server, got, out.String())
		}
	}
}

func TestTurnConflictHintNamesOnlyAPortConflict(t *testing.T) {
	if got := turnConflictHint(turnrelay.Status{UDPError: "listen udp: bind: permission denied"}, 3478); got != "" {
		t.Fatalf("hint for an unrelated error: %q", got)
	}
	for _, st := range []turnrelay.Status{
		{UDPError: "listen udp4 0.0.0.0:3478: bind: address already in use"},
		{TCPErr: "listen tcp4 0.0.0.0:3478: bind: address already in use"},
	} {
		got := turnConflictHint(st, 3478)
		if !strings.HasPrefix(got, ": ") || !strings.Contains(got, "3478") || !strings.Contains(got, "WEBRTC_BUILTIN_TURN_PORT") {
			t.Fatalf("hint = %q", got)
		}
	}
}

func TestTurnTargetCarriesTheSFUEndpoints(t *testing.T) {
	local, announced := net.ParseIP("10.0.0.2"), net.ParseIP("203.0.113.7")
	got := turnTarget(sfu.Endpoints{Local: []net.IP{local}, Announced: []net.IP{announced}, PortMin: 50000, PortMax: 50010})
	if len(got.LocalIPs) != 1 || !got.LocalIPs[0].Equal(local) || len(got.Announced) != 1 || !got.Announced[0].Equal(announced) || got.PortMin != 50000 || got.PortMax != 50010 {
		t.Fatalf("target = %+v", got)
	}
}

func TestStartUpdateCheckerHonoursTheSwitch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if got := startUpdateChecker(ctx, &config.Config{UpdateCheck: false}); got != nil {
		t.Fatal("a checker started although UPDATE_CHECK_ENABLED is off")
	}
	if got := startUpdateChecker(ctx, &config.Config{UpdateCheck: true}); got == nil {
		t.Fatal("no checker although the update check is on")
	}
}

func voiceTestConfig(t *testing.T, mux uint16, uploadMbit int) *config.Config {
	t.Helper()
	port := freeUDP(t)
	return &config.Config{
		AppEnv: "development", WebRTCAnnounce: []string{"127.0.0.1"},
		WebRTCUDPPortMin: port, WebRTCUDPPortMax: port, WebRTCUDPMuxPort: mux,
		VoiceUploadLimitMbit: uploadMbit,
	}
}

func TestStartVoiceAnnouncesAndAppliesTheUploadLimit(t *testing.T) {
	logs := captureLog(t, opwarn.WebRTCUDPMuxDisabled)
	cfg := voiceTestConfig(t, 0, 25)
	cfg.WebRTCUDPMuxPort = cfg.WebRTCUDPPortMin
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	voice, closeVoice, err := startVoice(ctx, cfg, &setupcheck.Runtime{})
	if err != nil || voice == nil {
		t.Fatalf("voice = %v, err = %v", voice, err)
	}
	closeVoice()
	for _, want := range []string{"voice upload limit on", "limit_mbit=25", "webrtc announce"} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("log lacks %q: %s", want, logs.String())
		}
	}
	if _, raised := warningValue(opwarn.WebRTCUDPMuxDisabled); raised {
		t.Error("the shared-port warning was raised although a mux port is set")
	}
}

func TestStartVoiceWarnsWhenEveryPeerTakesItsOwnPort(t *testing.T) {
	captureLog(t, opwarn.WebRTCUDPMuxDisabled)
	cfg := voiceTestConfig(t, 0, 0)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_, closeVoice, err := startVoice(ctx, cfg, &setupcheck.Runtime{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closeVoice)
	if ports, raised := warningValue(opwarn.WebRTCUDPMuxDisabled); !raised || ports != 1 {
		t.Fatalf("warning raised = %v, ports = %d, want a one-port range", raised, ports)
	}
}

func TestStartVoiceDisablesVoiceWhenThePortIsTaken(t *testing.T) {
	logs := captureLog(t, opwarn.VoiceUnavailable)
	cfg := voiceTestConfig(t, 0, 0)
	cfg.WebRTCUDPMuxPort = cfg.WebRTCUDPPortMin
	holder, err := net.ListenPacket("udp", fmt.Sprintf(":%d", cfg.WebRTCUDPMuxPort))
	if err != nil {
		t.Skipf("cannot hold the port: %v", err)
	}
	t.Cleanup(func() { _ = holder.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	voice, closeVoice, err := startVoice(ctx, cfg, &setupcheck.Runtime{})
	if err != nil || voice != nil {
		t.Fatalf("voice = %v, err = %v: want voice off without an error", voice != nil, err)
	}
	closeVoice() // must be callable even though nothing runs
	if _, raised := warningValue(opwarn.VoiceUnavailable); !raised || !strings.Contains(logs.String(), "voice disabled") {
		t.Fatalf("no operator warning for the disabled voice: %s", logs.String())
	}
}
