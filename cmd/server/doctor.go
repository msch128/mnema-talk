package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"
	"time"

	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/opwarn"
	"github.com/msch128/mnema-talk/internal/portconflict"
	"github.com/msch128/mnema-talk/internal/s3"
	"github.com/msch128/mnema-talk/internal/setupcheck"
	"github.com/msch128/mnema-talk/internal/stunprobe"
)

const doctorUsage = `usage: mnema-talk doctor

Prints the setup checks of the admin console's Status tab with a fix line under
every amber or red one, and exits with status 1 when one is red. Run it where
the server runs: docker compose exec app mnema-talk doctor`

// errDoctorRed is returned when a check is red; main exits with status 1
// without logging it, the report already said what is wrong.
var errDoctorRed = errors.New("a setup check is red")

// doctorProbes is what the command asks of the system; tests replace it.
type doctorProbes struct {
	DBPing  func(ctx context.Context) error
	Storage func(ctx context.Context) error
	Detect  setupcheck.Detector
	Detect6 setupcheck.Detector
	Host    func() setupcheck.Host
	Skew    func(ctx context.Context) (time.Duration, error)
	// UDPHeld and TCPHeld say whether some process already holds the port in
	// this container, which is how the running server shows itself.
	UDPHeld func(port uint16) bool
	TCPHeld func(port uint16) bool
}

func doctorCommand(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 {
		return errors.New(doctorUsage)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	return runDoctor(ctx, cfg, systemDoctorProbes(cfg), out)
}

// doctorProbeTimeout bounds every probe of the doctor, so an unreachable
// database, storage or STUN server cannot hang the command.
const doctorProbeTimeout = 5 * time.Second

func systemDoctorProbes(cfg *config.Config) doctorProbes {
	return doctorProbes{
		DBPing: func(ctx context.Context) error {
			ctx, cancel := context.WithTimeout(ctx, doctorProbeTimeout)
			defer cancel()
			pool, err := db.Connect(ctx, cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer pool.Close()
			return pool.Ping(ctx)
		},
		Storage: func(ctx context.Context) error {
			ctx, cancel := context.WithTimeout(ctx, doctorProbeTimeout)
			defer cancel()
			return s3.CheckBucket(ctx, cfg) // HEAD only: the doctor never creates the bucket
		},
		Detect: func(ctx context.Context, s []string, d time.Duration) (netip.Addr, string, error) {
			return stunprobe.Detect(ctx, s, min(d, doctorProbeTimeout))
		},
		Detect6: func(ctx context.Context, s []string, d time.Duration) (netip.Addr, string, error) {
			return stunprobe.Detect6(ctx, s, min(d, doctorProbeTimeout))
		},
		Host: setupcheck.SystemHost,
		Skew: func(ctx context.Context) (time.Duration, error) {
			ctx, cancel := context.WithTimeout(ctx, doctorProbeTimeout)
			defer cancel()
			return setupcheck.MeasureClockSkew(ctx, setupcheck.DefaultNTPServers, 3*time.Second)
		},
		UDPHeld: func(port uint16) bool {
			pc, err := net.ListenPacket("udp4", net.JoinHostPort("", strconv.Itoa(int(port))))
			if err == nil {
				_ = pc.Close()
				return false
			}
			return portconflict.InUse(err)
		},
		TCPHeld: func(port uint16) bool {
			c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port))), 2*time.Second)
			if err != nil {
				return false
			}
			_ = c.Close()
			return true
		},
	}
}

// runDoctor gathers what the Status tab shows, prints it and returns
// errDoctorRed when a check is red.
func runDoctor(ctx context.Context, cfg *config.Config, p doctorProbes, out io.Writer) error {
	in, notes := doctorInput(ctx, cfg, p)
	checks := withoutProxy(setupcheck.Run(in))
	fmt.Fprintf(out, "Mnema Talk setup checks (%s)\n\n", cfg.PublicURL)
	worst := setupcheck.Report(out, checks)
	for _, n := range notes {
		fmt.Fprintln(out, n)
	}
	if worst == setupcheck.Fail {
		return errDoctorRed
	}
	return nil
}

// withoutProxy drops the reverse proxy and WebSocket checks: they look at live
// requests and only the running server can judge them.
func withoutProxy(checks []setupcheck.Check) []setupcheck.Check {
	out := checks[:0:0]
	for _, c := range checks {
		if c.ID != setupcheck.ReverseProxy && c.ID != setupcheck.WebSocket {
			out = append(out, c)
		}
	}
	return out
}

// doctorInput measures what a second process can measure. notes says what it
// could not.
func doctorInput(ctx context.Context, cfg *config.Config, p doctorProbes) (setupcheck.Input, []string) {
	rt := &setupcheck.Runtime{}
	host := p.Host()
	ann := setupcheck.ResolveAnnouncement(ctx, cfg, p.Detect, host)
	rt.SetAnnounce(ann.Source, ann.Entries, ann.DetectErr)
	setupcheck.JudgeNetwork(ctx, cfg, ann, host, p.Detect6, rt)
	if cfg.ClockCheck {
		if skew, err := p.Skew(ctx); err == nil {
			rt.SetClockSkew(skew)
		}
	}
	in := setupcheck.Input{Cfg: cfg, Runtime: rt, DBReachable: p.DBPing(ctx) == nil, StorageConfigured: true, StorageErr: p.Storage(ctx)}
	notes := []string{"The reverse proxy check needs live requests: see the admin console's Status tab."}
	if !p.TCPHeld(portNumber(cfg.Port)) {
		return in, append(notes, "The server is not running in this container, so the voice and relay checks were skipped.")
	}
	in.Warnings = serverWarnings(cfg, p)
	in.TURNRunning = cfg.BuiltinTURNEnabled() && p.TCPHeld(cfg.WebRTCBuiltinTURNPort)
	return in, notes
}

// serverWarnings derives from the ports what the running server would have
// raised: voice did not start (nobody holds the media port), a shared port is
// off (then voice is not judged), the relay did not start.
func serverWarnings(cfg *config.Config, p doctorProbes) []opwarn.Warning {
	var ws []opwarn.Warning
	if cfg.WebRTCUDPMuxPort == 0 {
		// Every peer takes its own port from the range while a call runs, so an
		// idle server holds no fixed port to look for: voice is not judged.
		return append(ws, opwarn.Warning{Code: opwarn.WebRTCUDPMuxDisabled})
	}
	if !p.UDPHeld(cfg.WebRTCUDPMuxPort) {
		ws = append(ws, opwarn.Warning{Code: opwarn.VoiceUnavailable})
	}
	return ws
}

func portNumber(s string) uint16 {
	n, _ := strconv.Atoi(s)
	return uint16(n)
}
