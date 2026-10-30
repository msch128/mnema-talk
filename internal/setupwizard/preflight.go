package setupwizard

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/msch128/mnema-talk/internal/setupcheck"
	"github.com/msch128/mnema-talk/internal/stunprobe"
)

// LevelNote marks a finding that is a reminder, not a result.
const LevelNote setupcheck.Level = "note"

// Finding is one preflight result.
type Finding struct {
	Level  setupcheck.Level
	Title  string
	Detail string
}

// Probe is what the preflight asks of the network; tests replace it.
type Probe struct {
	Resolve  func(ctx context.Context, host string) ([]netip.Addr, error)
	PublicIP func(ctx context.Context) (netip.Addr, error)
	Dial     func(ctx context.Context, addr string) error
	Skew     func(ctx context.Context) (time.Duration, error)

	// The rest is optional; nil leaves the matching check out.
	// PublicIP6 finds a global IPv6 address with STUN.
	PublicIP6 func(ctx context.Context) (netip.Addr, error)
	// Host describes the machine: its default-route address and whether the
	// wizard runs in a container (then it sees neither Docker nor the host's
	// ports).
	Host func() setupcheck.Host
	// PortFree says whether a TCP or UDP port can be bound on this machine.
	PortFree func(proto string, port int) bool
	// Compose reports whether `docker compose` works on this machine.
	Compose func(ctx context.Context) error
}

// DefaultProbe asks the real network with the default STUN and NTP servers.
func DefaultProbe() Probe { return NewProbe(stunprobe.DefaultServers, setupcheck.DefaultNTPServers) }

// NewProbe asks the real network, using the given STUN and NTP servers.
func NewProbe(stunServers, ntpServers []string) Probe {
	return Probe{
		Resolve: func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip4", host)
		},
		PublicIP: func(ctx context.Context) (netip.Addr, error) {
			ip, _, err := stunprobe.Detect(ctx, stunServers, stunprobe.DefaultTimeout)
			return ip, err
		},
		Dial: func(ctx context.Context, addr string) error {
			d := net.Dialer{Timeout: 3 * time.Second}
			c, err := d.DialContext(ctx, "tcp", addr)
			if err == nil {
				_ = c.Close()
			}
			return err
		},
		Skew: func(ctx context.Context) (time.Duration, error) {
			return setupcheck.MeasureClockSkew(ctx, ntpServers, 3*time.Second)
		},
		PublicIP6: func(ctx context.Context) (netip.Addr, error) {
			ip, _, err := stunprobe.Detect6(ctx, stunServers, stunprobe.DefaultTimeout)
			return ip, err
		},
		Host:     setupcheck.SystemHost,
		PortFree: portFree,
		Compose:  composeAvailable,
	}
}

// Preflight runs the checks that are possible from here. Whether the router
// forwards the voice ports cannot be tested from inside, so that is left to
// the checklist at the end and the connection check in the admin console.
func Preflight(ctx context.Context, o Options, p Probe) []Finding {
	u, err := url.Parse(o.PublicURL)
	if err != nil {
		return nil
	}
	out := append(machineFindings(ctx, o, p), clockFinding(ctx, p))
	if o.IsLocal() {
		return out
	}
	public, pubErr := p.PublicIP(ctx)
	out = append(out, publicIPFinding(public, pubErr))
	if f, ok := networkFinding(ctx, public, pubErr, p); ok {
		out = append(out, f)
	}
	if f, ok := dnsFinding(ctx, u.Hostname(), public, pubErr == nil, p); ok {
		out = append(out, f)
	}
	return append(out, reachFinding(ctx, u, p))
}

func publicIPFinding(ip netip.Addr, err error) Finding {
	if err != nil {
		return Finding{setupcheck.Warn, "Public address", "Could not find this network's public IPv4 address with STUN. Voice then needs WEBRTC_NAT_1TO1_IP=<public IPv4 or dynamic-DNS name> in .env."}
	}
	return Finding{setupcheck.OK, "Public address", fmt.Sprintf("This network's public IPv4 address is %s; the server finds it itself at startup.", ip)}
}

func dnsFinding(ctx context.Context, host string, public netip.Addr, havePublic bool, p Probe) (Finding, bool) {
	if _, err := netip.ParseAddr(host); err == nil || isLocalName(host) {
		return Finding{}, false
	}
	addrs, err := p.Resolve(ctx, host)
	switch {
	case err != nil || len(addrs) == 0:
		detail := fmt.Sprintf("%s does not resolve yet. Create a DNS A record for it", host)
		if havePublic {
			detail += " pointing to " + public.String()
		}
		return Finding{setupcheck.Warn, "DNS", detail + "."}, true
	case havePublic && !slices.Contains(addrs, public):
		return Finding{setupcheck.Warn, "DNS", fmt.Sprintf("%s resolves to %s but this network's public address is %s. Fine behind a CDN or when the server runs elsewhere; otherwise point the DNS record at %s. If the record holds your router's own address and it differs from %s, your provider shares one public address between many customers (CGNAT): see \"Can I host at home?\" in doc/operations.md.", host, joinAddrs(addrs), public, public, public)}, true
	}
	return Finding{setupcheck.OK, "DNS", fmt.Sprintf("%s resolves to %s.", host, joinAddrs(addrs))}, true
}

func isLocalName(host string) bool {
	return host == "localhost" || strings.HasSuffix(host, ".local") || !strings.Contains(host, ".")
}

func joinAddrs(a []netip.Addr) string {
	s := make([]string, len(a))
	for i, x := range a {
		s[i] = x.String()
	}
	return strings.Join(s, ", ")
}

func reachFinding(ctx context.Context, u *url.URL, p Probe) Finding {
	port := u.Port()
	if port == "" {
		port = map[string]string{"https": "443", "http": "80"}[u.Scheme]
	}
	addr := net.JoinHostPort(u.Hostname(), port)
	if err := p.Dial(ctx, addr); err != nil {
		return Finding{LevelNote, "Web address", fmt.Sprintf("Nothing answers on %s yet. That is expected before the first start and the reverse proxy; the checklist below says when to look again.", addr)}
	}
	return Finding{setupcheck.OK, "Web address", fmt.Sprintf("%s accepts connections.", addr)}
}

func clockFinding(ctx context.Context, p Probe) Finding {
	skew, err := p.Skew(ctx)
	if err != nil {
		return Finding{setupcheck.OK, "Clock", "No NTP server reachable; clock not compared."}
	}
	if skew < 0 {
		skew = -skew
	}
	if skew >= 5*time.Second {
		return Finding{setupcheck.Warn, "Clock", fmt.Sprintf("This host's clock is off by about %s; turn on time synchronization (NTP) or two-factor codes will fail.", skew.Round(time.Second))}
	}
	return Finding{setupcheck.OK, "Clock", "The clock is in sync."}
}
