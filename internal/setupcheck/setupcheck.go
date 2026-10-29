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
	// Detail says what was found, in one sentence.
	Detail string `json:"detail"`
	// Fix names the variable to set and the value to use; empty when ok.
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
	c := Check{ID: PublicURL}
	switch {
	case cfg.IsProduction() && cfg.PublicURLIsLocal():
		c.Level = Fail
		c.Detail = fmt.Sprintf("PUBLIC_URL is %s, which only this machine can open; invite links, e-mails and browser origin checks use it.", cfg.PublicURL)
		c.Fix = "Set PUBLIC_URL=https://chat.example.com (the address members type into their browser) in .env and run docker compose up -d."
	case in.RequestOrigin != "" && !sameOrigin(in.RequestOrigin, cfg.PublicURL):
		c.Level = Warn
		c.Detail = fmt.Sprintf("You opened this page at %s but PUBLIC_URL is %s.", in.RequestOrigin, cfg.PublicURL)
		c.Fix = fmt.Sprintf("If %s is the address members use, set PUBLIC_URL=%s; if you came through a reverse proxy, let it pass the Host and X-Forwarded-Proto headers.", in.RequestOrigin, in.RequestOrigin)
	case cfg.InsecurePublicURLWarning() != "" || (has[opwarn.InsecurePublicURL]):
		c.Level = Warn
		c.Detail = "PUBLIC_URL is plain http://, so session cookies lose the Secure flag and the __Host- prefix."
		c.Fix = "Put a reverse proxy with TLS in front and set PUBLIC_URL=https://chat.example.com (skip this on a trusted LAN)."
	default:
		c.Level = OK
		c.Detail = fmt.Sprintf("PUBLIC_URL is %s.", cfg.PublicURL)
	}
	return c
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
	c := Check{ID: VoiceAddress}
	if !cfg.IsProduction() {
		c.Level = OK
		c.Detail = "Development mode: voice uses this machine's own addresses."
		return c
	}
	if rt.source != SourceNone && len(rt.ips) > 0 {
		c.Level = OK
		c.Detail = fmt.Sprintf("Voice is announced on %s (%s).", strings.Join(rt.ips, ", "), sourceLabel(rt.source))
		return c
	}
	c.Detail = "No public voice address is known, so browsers are told the container's own address."
	if rt.detectErr != "" {
		c.Detail += " Automatic detection failed: " + rt.detectErr + "."
	}
	c.Fix = "Set WEBRTC_NAT_1TO1_IP=<your public IPv4 address or dynamic-DNS name> in .env (add the LAN address after a comma for members on the same network), then docker compose up -d."
	if len(cfg.WebRTCTURNURLs) > 0 {
		c.Level = Warn
		c.Detail += " Only the TURN relay can carry voice."
	} else {
		c.Level = Fail
	}
	return c
}

func sourceLabel(source string) string {
	switch source {
	case SourceEnv:
		return "from WEBRTC_NAT_1TO1_IP"
	case SourcePublicURL:
		return "taken from PUBLIC_URL"
	case SourceDetected:
		return "detected with a STUN request"
	}
	return source
}

func voiceUDPCheck(cfg *config.Config, has map[string]bool) Check {
	c := Check{ID: VoiceUDP}
	switch {
	case has[opwarn.VoiceUnavailable]:
		c.Level = Fail
		c.Detail = "The voice engine did not start, so Talks have no audio or video."
		c.Fix = fmt.Sprintf("Free UDP ports %d-%d on the host (or change WEBRTC_UDP_PORT_MIN and WEBRTC_UDP_PORT_MAX) and restart; the log names the cause.", cfg.WebRTCUDPPortMin, cfg.WebRTCUDPPortMax)
	case has[opwarn.WebRTCUDPMuxDisabled]:
		c.Level = Warn
		c.Detail = "WEBRTC_UDP_MUX_PORT=0 gives every media peer its own port, which caps how many people can be in calls."
		c.Fix = "Remove WEBRTC_UDP_MUX_PORT (or set it to " + fmt.Sprint(cfg.WebRTCUDPPortMin) + ") so all peers share one UDP port."
	default:
		c.Level = OK
		c.Detail = fmt.Sprintf("Voice listens on UDP %d. Forward that port from the router to this host; it cannot be tested from inside.", mediaPort(cfg))
	}
	return c
}

func mediaPort(cfg *config.Config) uint16 {
	if cfg.WebRTCUDPMuxPort != 0 {
		return cfg.WebRTCUDPMuxPort
	}
	return cfg.WebRTCUDPPortMin
}

func mailCheck(cfg *config.Config) Check {
	if cfg.SMTP.Enabled() {
		return Check{ID: Mail, Level: OK, Detail: "Outgoing mail is configured (" + cfg.SMTP.Host + ")."}
	}
	return Check{
		ID:     Mail,
		Level:  Warn,
		Detail: "No outgoing mail: members get no reset link for a forgotten login and e-mail addresses cannot be confirmed.",
		Fix:    "Optional: set SMTP_HOST, SMTP_PORT, SMTP_USERNAME, SMTP_PASSWORD and SMTP_FROM in .env.",
	}
}

func storageCheck(in Input) Check {
	c := Check{ID: Storage}
	switch {
	case !in.StorageConfigured:
		c.Level = Fail
		c.Detail = "Object storage is not configured, so uploads and avatars do not work."
		c.Fix = "Set S3_ENDPOINT, S3_BUCKET, S3_ACCESS_KEY and S3_SECRET_KEY in .env."
	case in.StorageErr != nil:
		c.Level = Fail
		c.Detail = "Object storage does not answer yet; uploads fail until it does."
		c.Fix = "Check that the seaweedfs container is running (docker compose ps) and that S3_ACCESS_KEY and S3_SECRET_KEY in .env match the ones it was first started with."
	default:
		c.Level = OK
		c.Detail = "Object storage is ready."
	}
	return c
}

func databaseCheck(reachable bool) Check {
	if reachable {
		return Check{ID: Database, Level: OK, Detail: "The database is reachable."}
	}
	return Check{
		ID:     Database,
		Level:  Fail,
		Detail: "The database does not answer.",
		Fix:    "Check that the postgres container is healthy (docker compose ps) and that POSTGRES_PASSWORD in .env is the one it was created with.",
	}
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
	c := Check{ID: Clock}
	switch {
	case abs >= clockFailSkew:
		c.Level = Fail
	case abs >= clockWarnSkew:
		c.Level = Warn
	default:
		c.Level = OK
		c.Detail = fmt.Sprintf("The server clock is within %s of real time.", clockWarnSkew)
		return c, true
	}
	c.Detail = fmt.Sprintf("The server clock is off by about %s, which breaks two-factor codes and session expiry.", abs.Round(time.Second))
	c.Fix = "Turn on time synchronization (NTP) on the host; on a Synology: Control Panel > Regional Options > Time > Synchronize with an NTP server."
	return c, true
}

func proxyCheck(has map[string]bool) Check {
	c := Check{ID: ReverseProxy}
	switch {
	case has[opwarn.ProxyNotTrusted]:
		c.Level = Warn
		c.Detail = "Requests arrive with X-Forwarded-For from a proxy that is not trusted, so every member looks like one address to rate limits and the login lockout."
		c.Fix = "Set TRUSTED_PROXY_CIDRS=<address of your reverse proxy> in .env (its container IP, the Docker bridge gateway or its LAN address; never a whole private range)."
	case has[opwarn.TrustedProxyBroad]:
		c.Level = Warn
		c.Detail = "TRUSTED_PROXY_CIDRS covers a wide range, so any host in it can forge a client address."
		c.Fix = "List only the reverse proxy's own address in TRUSTED_PROXY_CIDRS."
	default:
		c.Level = OK
		c.Detail = "No proxy header problems seen."
	}
	return c
}
