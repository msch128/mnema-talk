// Package setupcheck turns the configuration and what the server found out
// about its surroundings into a short list of setup checks: green (ok),
// amber (warn) and red (fail), each red or amber one naming the concrete
// variable to set. The admin status page shows the list and the server logs
// the same list shortly after the start. Operator warnings raised elsewhere
// (internal/opwarn) feed the checks, so both views agree.
package setupcheck

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/opwarn"
)

// Level is the state of one check.
type Level string

const (
	OK   Level = "ok"
	Warn Level = "warn"
	Fail Level = "fail"
)

// Stable check ids; the web app labels each one. Never reuse or rename one.
const (
	PublicURL    = "public_url"
	VoiceAddress = "voice_address"
	VoiceUDP     = "voice_udp"
	Turn         = "turn"
	Mail         = "mail"
	Storage      = "storage"
	Database     = "database"
	Clock        = "clock"
	ReverseProxy = "reverse_proxy"
)

// Check is one line of the setup list.
type Check struct {
	// ID is the stable identifier of the check.
	ID string `json:"id" example:"public_url"`
	// Level is ok (green), warn (amber) or fail (red).
	Level Level `json:"level" enums:"ok,warn,fail"`
	// Code names the outcome ("public_url.local"): clients translate by it and
	// fill in Params. Absent when the server has no code for the outcome.
	Code string `json:"code,omitempty" binding:"optional"`
	// Params are the values the message names (ports, variable values), by name.
	Params map[string]string `json:"params,omitempty" binding:"optional"`
	// Detail says what was found, in one sentence, in English; the fallback
	// for clients that do not know the Code.
	Detail string `json:"detail"`
	// Fix names the variable to set and the value to use, in English; empty when ok.
	Fix string `json:"fix,omitempty" binding:"optional"`
}

// Where the announced voice address came from.
const (
	SourceNone      = "none"
	SourceEnv       = "env"
	SourcePublicURL = "public_url"
	SourceDetected  = "stun"
)

// Runtime holds what the server learned while starting: it is written once or
// twice at startup and read by every check run.
type Runtime struct {
	mu             sync.Mutex
	announceSource string
	announceIPs    []string
	detectErr      string
	skew           time.Duration
	clockMeasured  bool
}

// SetAnnounce records the announced voice addresses and where they came from.
// detectErr is why the automatic detection found nothing ("" if not tried).
func (r *Runtime) SetAnnounce(source string, ips []string, detectErr string) {
	r.mu.Lock()
	r.announceSource, r.announceIPs, r.detectErr = source, append([]string(nil), ips...), detectErr
	r.mu.Unlock()
}

// SetClockSkew records how far the server clock is ahead of real time
// (negative: behind).
func (r *Runtime) SetClockSkew(skew time.Duration) {
	r.mu.Lock()
	r.skew, r.clockMeasured = skew, true
	r.mu.Unlock()
}

type runtimeSnapshot struct {
	source        string
	ips           []string
	detectErr     string
	skew          time.Duration
	clockMeasured bool
}

func (r *Runtime) snapshot() runtimeSnapshot {
	if r == nil {
		return runtimeSnapshot{source: SourceNone}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	s := runtimeSnapshot{source: r.announceSource, ips: r.announceIPs, detectErr: r.detectErr, skew: r.skew, clockMeasured: r.clockMeasured}
	if s.source == "" {
		s.source = SourceNone
	}
	return s
}

// Input is everything Run looks at.
type Input struct {
	Cfg     *config.Config
	Runtime *Runtime
	// Warnings are the active operator warnings (opwarn.Default.List()).
	Warnings []opwarn.Warning
	// DBReachable is whether the database answered a ping.
	DBReachable bool
	// StorageConfigured and StorageErr describe object storage; StorageErr is
	// nil when it is ready.
	StorageConfigured bool
	StorageErr        error
	// RequestOrigin is the scheme and host the admin opened the page at
	// ("https://chat.example.com"); empty when there is no request (startup).
	RequestOrigin string
	// TURNRunning is whether the built-in TURN relay was started; false when
	// it is off or when it could not start (voice unavailable).
	TURNRunning bool
	// TURNProbe is the last self-test of the built-in TURN relay: "reachable",
	// "unreachable" or "" / "unknown" (not run, or no relay).
	TURNProbe string
}

// Run evaluates every check, in the order they are shown.
func Run(in Input) []Check {
	rt := in.Runtime.snapshot()
	has := warningSet(in.Warnings)
	checks := []Check{
		publicURLCheck(in, has),
		voiceAddressCheck(in.Cfg, rt),
		voiceUDPCheck(in.Cfg, has),
		turnCheck(in, has),
		mailCheck(in.Cfg),
		storageCheck(in),
		databaseCheck(in.DBReachable),
	}
	if c, ok := clockCheck(rt); ok {
		checks = append(checks, c)
	}
	return append(checks, proxyCheck(has))
}

// Worst returns the most severe level in the list (ok for an empty list).
func Worst(checks []Check) Level {
	worst := OK
	for _, c := range checks {
		if c.Level == Fail {
			return Fail
		}
		if c.Level == Warn {
			worst = Warn
		}
	}
	return worst
}

func warningSet(ws []opwarn.Warning) map[string]bool {
	m := make(map[string]bool, len(ws))
	for _, w := range ws {
		m[w.Code] = true
	}
	return m
}

func publicURLCheck(in Input, has map[string]bool) Check {
	cfg := in.Cfg
	p := Params{"public_url": cfg.PublicURL, "origin": in.RequestOrigin}
	switch {
	case cfg.IsProduction() && cfg.PublicURLIsLocal():
		return newCheck(PublicURL, "public_url.local", Fail, p)
	case in.RequestOrigin != "" && !sameOrigin(in.RequestOrigin, cfg.PublicURL):
		return newCheck(PublicURL, "public_url.origin_mismatch", Warn, p)
	case cfg.InsecurePublicURLWarning() != "" || has[opwarn.InsecurePublicURL]:
		return newCheck(PublicURL, "public_url.insecure", Warn, p)
	}
	return newCheck(PublicURL, "public_url.ok", OK, p)
}

// sameOrigin compares two origins ignoring case, a trailing slash and the
// default port.
func sameOrigin(a, b string) bool {
	norm := func(s string) string {
		s = strings.ToLower(strings.TrimRight(strings.TrimSpace(s), "/"))
		s = strings.TrimSuffix(s, ":443")
		if strings.HasPrefix(s, "http://") {
			s = strings.TrimSuffix(s, ":80")
		}
		return s
	}
	return norm(a) == norm(b)
}

func voiceAddressCheck(cfg *config.Config, rt runtimeSnapshot) Check {
	if !cfg.IsProduction() {
		return newCheck(VoiceAddress, "voice_address.development", OK, nil)
	}
	if rt.source != SourceNone && len(rt.ips) > 0 {
		ips := strings.Join(rt.ips, ", ")
		if code, ok := announcedCodes[rt.source]; ok {
			return newCheck(VoiceAddress, code, OK, Params{"ips": ips})
		}
		return Check{ID: VoiceAddress, Level: OK, Detail: fmt.Sprintf("Voice is announced on %s (%s).", ips, rt.source)}
	}
	code, level := "voice_address.missing", Fail
	switch {
	case rt.detectErr != "" && len(cfg.WebRTCTURNURLs) > 0:
		code, level = "voice_address.missing_detect_failed_turn_only", Warn
	case rt.detectErr != "":
		code = "voice_address.missing_detect_failed"
	case len(cfg.WebRTCTURNURLs) > 0:
		code, level = "voice_address.missing_turn_only", Warn
	}
	return newCheck(VoiceAddress, code, level, Params{"error": rt.detectErr})
}

// announcedCodes maps where the voice address came from to its check code.
var announcedCodes = map[string]string{
	SourceEnv:       "voice_address.announced_env",
	SourcePublicURL: "voice_address.announced_public_url",
	SourceDetected:  "voice_address.announced_stun",
}

func voiceUDPCheck(cfg *config.Config, has map[string]bool) Check {
	p := Params{
		"port_min": fmt.Sprint(cfg.WebRTCUDPPortMin),
		"port_max": fmt.Sprint(cfg.WebRTCUDPPortMax),
		"port":     fmt.Sprint(mediaPort(cfg)),
	}
	switch {
	case has[opwarn.VoiceUnavailable]:
		return newCheck(VoiceUDP, "voice_udp.unavailable", Fail, p)
	case has[opwarn.WebRTCUDPMuxDisabled]:
		return newCheck(VoiceUDP, "voice_udp.mux_disabled", Warn, p)
	}
	return newCheck(VoiceUDP, "voice_udp.ok", OK, p)
}

func mediaPort(cfg *config.Config) uint16 {
	if cfg.WebRTCUDPMuxPort != 0 {
		return cfg.WebRTCUDPMuxPort
	}
	return cfg.WebRTCUDPPortMin
}

func mailCheck(cfg *config.Config) Check {
	if cfg.SMTP.Enabled() {
		return newCheck(Mail, "mail.ok", OK, Params{"host": cfg.SMTP.Host})
	}
	return newCheck(Mail, "mail.none", Warn, nil)
}

func storageCheck(in Input) Check {
	switch {
	case !in.StorageConfigured:
		return newCheck(Storage, "storage.not_configured", Fail, nil)
	case in.StorageErr != nil:
		return newCheck(Storage, "storage.unreachable", Fail, nil)
	}
	return newCheck(Storage, "storage.ok", OK, nil)
}

func databaseCheck(reachable bool) Check {
	if reachable {
		return newCheck(Database, "database.ok", OK, nil)
	}
	return newCheck(Database, "database.unreachable", Fail, nil)
}

// Thresholds: sign-in codes of two-factor authentication are valid for 30
// seconds, so a few seconds of skew are harmless and half a minute is not.
const (
	clockWarnSkew = 5 * time.Second
	clockFailSkew = 30 * time.Second
)

// clockCheck reports false when the skew was never measured (no NTP server
// reachable, or the check is switched off): an unknown is not shown as a
// problem.
func clockCheck(rt runtimeSnapshot) (Check, bool) {
	if !rt.clockMeasured {
		return Check{}, false
	}
	abs := rt.skew
	if abs < 0 {
		abs = -abs
	}
	if abs < clockWarnSkew {
		return newCheck(Clock, "clock.ok", OK, Params{"window": clockWarnSkew.String()}), true
	}
	level := Warn
	if abs >= clockFailSkew {
		level = Fail
	}
	return newCheck(Clock, "clock.skewed", level, Params{"skew": abs.Round(time.Second).String()}), true
}

func proxyCheck(has map[string]bool) Check {
	switch {
	case has[opwarn.ProxyNotTrusted]:
		return newCheck(ReverseProxy, "reverse_proxy.not_trusted", Warn, nil)
	case has[opwarn.TrustedProxyBroad]:
		return newCheck(ReverseProxy, "reverse_proxy.broad", Warn, nil)
	}
	return newCheck(ReverseProxy, "reverse_proxy.ok", OK, nil)
}
