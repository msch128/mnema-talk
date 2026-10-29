package main

import (
	"bytes"
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"

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

func TestDoctorReportsCGNAT(t *testing.T) {
	p := healthyProbes()
	p.Detect = func(context.Context, []string, time.Duration) (netip.Addr, string, error) {
		return netip.MustParseAddr("100.72.1.9"), "stun.test:3478", nil
	}
	var out bytes.Buffer
	if err := runDoctor(context.Background(), productionTestConfig(t), p, &out); !errors.Is(err, errDoctorRed) {
		t.Fatalf("CGNAT is red: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "100.64.0.0/10") || !strings.Contains(out.String(), "Can I host at home?") {
		t.Errorf("CGNAT message incomplete:\n%s", out.String())
	}
}

func TestDoctorRejectsArguments(t *testing.T) {
	if err := doctorCommand(context.Background(), []string{"extra"}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "usage: mnema-talk doctor") {
		t.Fatalf("err = %v", err)
	}
}
