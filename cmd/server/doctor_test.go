package main

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/setupcheck"
)

// healthyProbes is a server that is up and fully reachable.
func healthyProbes() doctorProbes {
	return doctorProbes{
		DBPing:  func(context.Context) error { return nil },
		Storage: func(context.Context) error { return nil },
		Detect: func(context.Context, []string, time.Duration) (netip.Addr, string, error) {
			return netip.MustParseAddr("203.0.113.7"), "stun.test:3478", nil
		},
		Detect6: func(context.Context, []string, time.Duration) (netip.Addr, string, error) {
			return netip.Addr{}, "", errors.New("no IPv6")
		},
		Host: func() setupcheck.Host {
			return setupcheck.Host{InContainer: true, Local: netip.MustParseAddr("172.18.0.5")}
		},
		Skew:    func(context.Context) (time.Duration, error) { return 0, nil },
		UDPHeld: func(uint16) bool { return true },
		TCPHeld: func(uint16) bool { return true },
	}
}

func TestDoctorPrintsTheChecksAndSucceedsWithoutRed(t *testing.T) {
	var out bytes.Buffer
	err := runDoctor(context.Background(), productionTestConfig(t), healthyProbes(), &out)
	if err != nil {
		t.Fatalf("no check is red, got %v\n%s", err, out.String())
	}
	for _, want := range []string{"[ ok ] Voice is announced on 203.0.113.7", "[ ok ] The database is reachable.", "fix: Optional: set SMTP_HOST", "Status tab"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("report lacks %q:\n%s", want, out.String())
		}
	}
}

func TestDoctorExitsWithRedWhenTheDatabaseIsDown(t *testing.T) {
	p := healthyProbes()
	p.DBPing = func(context.Context) error { return errors.New("connection refused") }
	var out bytes.Buffer
	err := runDoctor(context.Background(), productionTestConfig(t), p, &out)
	if !errors.Is(err, errDoctorRed) {
		t.Fatalf("err = %v, want errDoctorRed\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "[FAIL] The database does not answer.") || !strings.Contains(out.String(), "fix: Check that the postgres container") {
		t.Errorf("the red check and its fix must be printed:\n%s", out.String())
	}
}

func TestDoctorFlagsAVoiceEngineThatDidNotStart(t *testing.T) {
	p := healthyProbes()
	p.UDPHeld = func(uint16) bool { return false }
	var out bytes.Buffer
	if err := runDoctor(context.Background(), productionTestConfig(t), p, &out); !errors.Is(err, errDoctorRed) {
		t.Fatalf("a free media port means voice is not listening; err = %v\n%s", err, out.String())
	}
}

func TestDoctorSkipsVoiceChecksWhenNoServerRunsHere(t *testing.T) {
	p := healthyProbes()
	p.UDPHeld = func(uint16) bool { return false }
	p.TCPHeld = func(uint16) bool { return false }
	var out bytes.Buffer
	if err := runDoctor(context.Background(), productionTestConfig(t), p, &out); err != nil {
		t.Fatalf("without a server here nothing about voice is judged: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "not running in this container") {
		t.Errorf("the skipped checks must be named:\n%s", out.String())
	}
}

func TestDoctorReportsCGNATAsAttentionNotRed(t *testing.T) {
	p := healthyProbes()
	p.Detect = func(context.Context, []string, time.Duration) (netip.Addr, string, error) {
		return netip.Addr{}, "", errors.New("no answer")
	}
	p.Host = func() setupcheck.Host { return setupcheck.Host{Local: netip.MustParseAddr("100.64.3.2")} }
	var out bytes.Buffer
	// The failed STUN request makes the voice address red on its own; the
	// host address in 100.64.0.0/10 must be amber, not red.
	_ = runDoctor(context.Background(), productionTestConfig(t), p, &out)
	if !strings.Contains(out.String(), "[warn] This home connection has no public IPv4") {
		t.Errorf("the host-only CGNAT finding must be amber:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "100.64.0.0/10") || !strings.Contains(out.String(), "Can I host at home?") {
		t.Errorf("CGNAT message incomplete:\n%s", out.String())
	}
}

func TestDoctorDoesNotJudgeVoiceWithoutASharedPort(t *testing.T) {
	cfg := productionTestConfig(t)
	cfg.WebRTCUDPMuxPort = 0
	p := healthyProbes()
	p.UDPHeld = func(uint16) bool { t.Fatal("looked for a port nobody holds while idle"); return false }
	var out bytes.Buffer
	if err := runDoctor(context.Background(), cfg, p, &out); err != nil {
		t.Fatalf("a valid configuration must not be red: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "WEBRTC_UDP_MUX_PORT=0") {
		t.Errorf("the mux_disabled warning is missing:\n%s", out.String())
	}
}

func TestDoctorRejectsArguments(t *testing.T) {
	if err := doctorCommand(context.Background(), []string{"extra"}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "usage: mnema-talk doctor") {
		t.Fatalf("err = %v", err)
	}
}

func TestDoctorReportsAllMissingSettingsAsOneConfigError(t *testing.T) {
	for _, k := range []string{"DATABASE_URL", "JWT_SECRET", "S3_ACCESS_KEY", "S3_SECRET_KEY"} {
		t.Setenv(k, "")
	}
	t.Setenv("APP_ENV", "production")
	err := doctorCommand(context.Background(), nil, &bytes.Buffer{})
	var ce *config.ConfigError
	if !errors.As(err, &ce) || len(ce.Problems) < 4 {
		t.Fatalf("err = %v", err)
	}
}

// The real probes, against local fixtures: a held port is seen as held, a free
// one as free, storage answers a HEAD without creating anything, and an
// unreachable database or a cancelled context fails fast.
func TestSystemDoctorProbesAgainstLocalFixtures(t *testing.T) {
	var methods []string
	objects := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(objects.Close)
	cfg := productionTestConfig(t)
	cfg.S3Endpoint, cfg.S3Bucket, cfg.S3Region, cfg.S3ForcePathStyle = objects.URL, "doctor-test", "us-east-1", true
	cfg.DatabaseURL = "postgres://u:p@127.0.0.1:1/x?sslmode=disable&connect_timeout=1"
	p := systemDoctorProbes(cfg)
	ctx := context.Background()

	if err := p.Storage(ctx); err != nil {
		t.Fatalf("storage: %v", err)
	}
	if len(methods) != 1 || methods[0] != http.MethodHead {
		t.Fatalf("storage probe sent %v, want one HEAD and no bucket creation", methods)
	}
	if err := p.DBPing(ctx); err == nil {
		t.Error("an unreachable database answered")
	}

	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := uint16(l.Addr().(*net.TCPAddr).Port)
	if !p.TCPHeld(port) {
		t.Error("a listening port is not seen as held")
	}
	_ = l.Close()
	if p.TCPHeld(port) {
		t.Error("a closed port is seen as held")
	}

	pc, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		t.Fatal(err)
	}
	udp := uint16(pc.LocalAddr().(*net.UDPAddr).Port)
	if !p.UDPHeld(udp) {
		t.Error("a bound UDP port is not seen as held")
	}
	_ = pc.Close()
	if p.UDPHeld(udp) {
		t.Error("a free UDP port is seen as held")
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := p.Detect(cancelled, []string{"127.0.0.1:9"}, time.Second); err == nil {
		t.Error("IPv4 detection ignored the cancelled context")
	}
	if _, _, err := p.Detect6(cancelled, []string{"[::1]:9"}, time.Second); err == nil {
		t.Error("IPv6 detection ignored the cancelled context")
	}
	if _, err := p.Skew(cancelled); err == nil {
		t.Error("the clock probe ignored the cancelled context")
	}
	if p.Host == nil {
		t.Error("no host probe")
	}
}

func TestPortNumberRejectsWhatDoesNotFitAPort(t *testing.T) {
	for in, want := range map[string]uint16{"8080": 8080, "65535": 65535, "65536": 0, "99999999": 0, "-1": 0, "x": 0, "": 0} {
		if got := portNumber(in); got != want {
			t.Errorf("portNumber(%q) = %d, want %d", in, got, want)
		}
	}
}
