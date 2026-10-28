// Package ssrf is the guard for outbound requests to addresses a member
// chose (outgoing webhooks). It resolves names itself, refuses every address
// that is not a public unicast one, and then connects to exactly the address
// it vetted, so a DNS answer that changes between the check and the connection
// (rebinding) does not matter. Redirects are not followed, no proxy from the
// environment is used and responses are never kept.
package ssrf

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// Refusal reasons. Callers show only these to members, never the raw network
// error, so a delivery log cannot be used to probe a network.
var (
	ErrBlocked  = errors.New("address is not allowed")
	ErrResolve  = errors.New("name could not be resolved")
	ErrConnect  = errors.New("could not connect")
	ErrBadURL   = errors.New("not a valid http or https URL")
	ErrRedirect = errors.New("redirects are not followed")
)

// maxURLLen bounds a target URL.
const maxURLLen = 2048

// never are addresses no allowlist can open and link preview refuses too (see
// PublicUnicast): the metadata endpoints, IPv4-compatible, IPv4-translated
// and discard-only IPv6, multicast and class E.
var never = []netip.Prefix{
	netip.MustParsePrefix("169.254.169.254/32"), // cloud metadata
	netip.MustParsePrefix("fd00:ec2::254/128"),  // AWS IPv6 metadata
	netip.MustParsePrefix("100.100.100.200/32"), // Alibaba metadata
	netip.MustParsePrefix("192.0.0.192/32"),     // Oracle metadata
	netip.MustParsePrefix("::/96"),              // IPv4-compatible
	netip.MustParsePrefix("::ffff:0:0:0/96"),    // IPv4-translated (SIIT)
	netip.MustParsePrefix("224.0.0.0/3"),        // multicast and class E
	netip.MustParsePrefix("ff00::/8"),           // multicast
	netip.MustParsePrefix("100::/64"),           // discard-only
}

// shared is the intersection: special-purpose prefixes both webhooks and
// link preview refuse. Carrier-grade NAT and site-local are also in private,
// so the webhook allowlist can still open those two; link preview cannot.
var shared = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),       // "this network"
	netip.MustParsePrefix("100.64.0.0/10"),   // carrier-grade NAT; allowlist may open it
	netip.MustParsePrefix("192.0.0.0/24"),    // IETF protocol assignments
	netip.MustParsePrefix("192.0.2.0/24"),    // documentation
	netip.MustParsePrefix("192.88.99.0/24"),  // 6to4 relay anycast
	netip.MustParsePrefix("198.18.0.0/15"),   // benchmarking
	netip.MustParsePrefix("198.51.100.0/24"), // documentation
	netip.MustParsePrefix("203.0.113.0/24"),  // documentation
	netip.MustParsePrefix("64:ff9b::/96"),    // NAT64: carries an IPv4 address
	netip.MustParsePrefix("64:ff9b:1::/48"),  // local-use NAT64
	netip.MustParsePrefix("2001::/32"),       // Teredo
	netip.MustParsePrefix("2001:db8::/32"),   // documentation
	netip.MustParsePrefix("2002::/16"),       // 6to4: carries an IPv4 address
	netip.MustParsePrefix("fec0::/10"),       // site-local; allowlist may open it
}

// private are the ranges for the local network: refused unless the operator
// allows them (WEBHOOK_ALLOW_PRIVATE_TARGETS).
var private = []netip.Prefix{
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("169.254.0.0/16"), // link-local
	netip.MustParsePrefix("100.64.0.0/10"),  // carrier-grade NAT; link preview always refuses this
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("fc00::/7"),  // unique local
	netip.MustParsePrefix("fe80::/10"), // link-local
	netip.MustParsePrefix("fec0::/10"), // site-local; link preview always refuses this
}

// Refused reports whether addr is not a public unicast address.
// Link preview blocks every true result. Policy.Check does too, except
// where its allowlist opens a loopback, private, link-local, carrier-grade
// NAT, unique-local or site-local address. Metadata in never stays closed.
func Refused(addr netip.Addr) bool {
	if !addr.IsValid() {
		return true
	}
	addr = addr.WithZone("").Unmap()
	return addr.IsUnspecified() || addr.IsLoopback() || addr.IsMulticast() ||
		addr.IsLinkLocalUnicast() || addr.IsPrivate() || contains(shared, addr)
}

// PublicUnicast reports whether addr is a globally routable unicast address:
// not Refused and not in the never list. Link preview connects only to such
// addresses, so it and the webhook guard share one list.
func PublicUnicast(addr netip.Addr) bool {
	addr = addr.WithZone("").Unmap()
	return !Refused(addr) && !contains(never, addr)
}

// Policy decides which addresses may be contacted.
type Policy struct {
	// Allow lists local-network ranges that may be contacted. Empty (the
	// default) allows public addresses only. It cannot open the metadata
	// addresses or the reserved ranges.
	Allow []netip.Prefix
}

// ParseAllow parses a comma-separated list of addresses and CIDR ranges.
func ParseAllow(list string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, item := range strings.Split(list, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if p, err := netip.ParsePrefix(item); err == nil {
			out = append(out, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(item)
		if err != nil {
			return nil, fmt.Errorf("%q is neither an address nor a CIDR range", item)
		}
		out = append(out, netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()))
	}
	return out, nil
}

func contains(list []netip.Prefix, a netip.Addr) bool {
	for _, p := range list {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// notAllowable reports an address the allowlist cannot open: it is not a
// real unicast address, or it is one of the metadata and reserved ranges.
func notAllowable(addr netip.Addr) bool {
	if !addr.IsValid() {
		return true
	}
	if addr.IsUnspecified() || addr.IsMulticast() {
		return true
	}
	return contains(never, addr)
}

// onLocalNet reports an address the allowlist may open.
func onLocalNet(addr netip.Addr) bool {
	if addr.IsLoopback() || addr.IsPrivate() {
		return true
	}
	if addr.IsLinkLocalUnicast() || addr.IsInterfaceLocalMulticast() {
		return true
	}
	return contains(private, addr)
}

// Check reports whether addr may be contacted. IPv4-mapped IPv6 addresses
// count as the IPv4 address they carry. The allowlist can open a local range
// even when Refused reports it; it cannot open never.
func (p *Policy) Check(addr netip.Addr) error {
	addr = addr.WithZone("").Unmap()
	if notAllowable(addr) {
		return ErrBlocked
	}
	if onLocalNet(addr) {
		if p != nil && contains(p.Allow, addr) {
			return nil
		}
		return ErrBlocked
	}
	if Refused(addr) {
		return ErrBlocked
	}
	return nil
}

// RejectedURLText reports a URL string that is empty, too long, or contains
// a space, an ASCII control or DEL. Other packages that fetch user-supplied
// URLs share it with ValidateURL.
func RejectedURLText(raw string) bool { return rejectedURLText(raw) }

// AcceptableURL reports an absolute http(s) URL with a host and no credentials.
func AcceptableURL(u *url.URL) bool { return acceptableURL(u) }

// rejectedURLText reports a URL string that is empty, too long, or contains
// a space, an ASCII control or DEL.
func rejectedURLText(raw string) bool {
	if raw == "" || len(raw) > maxURLLen {
		return true
	}
	for _, r := range raw {
		if r <= 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// acceptableURL reports an absolute http(s) URL with a host and no credentials.
func acceptableURL(u *url.URL) bool {
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	if u.Hostname() == "" || u.User != nil {
		return false
	}
	return u.Opaque == ""
}

// ValidateURL parses a target URL: absolute http or https with a host, no
// credentials, a literal IP only if its address is allowed.
func (p *Policy) ValidateURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if rejectedURLText(raw) {
		return nil, ErrBadURL
	}
	u, err := url.Parse(raw)
	if err != nil || !acceptableURL(u) {
		return nil, ErrBadURL
	}
	if addr, err := netip.ParseAddr(u.Hostname()); err == nil {
		if err := p.Check(addr); err != nil {
			return nil, err
		}
	}
	return u, nil
}

// Resolver looks names up (net.DefaultResolver in production).
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// Dialer connects to vetted addresses only.
type Dialer struct {
	Policy   *Policy
	Resolver Resolver
	// Timeout bounds one connection attempt.
	Timeout time.Duration
	// dial connects to a literal address; tests replace it.
	dial func(ctx context.Context, network, address string) (net.Conn, error)
}

// NewDialer returns a Dialer using the system resolver.
func NewDialer(policy *Policy) *Dialer {
	return &Dialer{Policy: policy, Resolver: net.DefaultResolver, Timeout: 5 * time.Second}
}

// DialContext resolves the host itself. Every address it resolves to must be
// allowed (one blocked answer refuses the host, so a record set mixing public
// and local addresses cannot be used), then it connects to the vetted
// addresses by number, never by name again.
func (d *Dialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, ErrBadURL
	}
	addrs, err := d.hostAddrs(ctx, host)
	if err != nil {
		return nil, err
	}
	for _, addr := range addrs {
		if err := d.Policy.Check(addr); err != nil {
			return nil, err
		}
	}
	return d.dialAddrs(ctx, network, port, addrs)
}

// hostAddrs returns a literal address as written, or every address of a name.
func (d *Dialer) hostAddrs(ctx context.Context, host string) ([]netip.Addr, error) {
	if addr, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{addr}, nil
	}
	addrs, err := d.Resolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addrs) == 0 {
		return nil, ErrResolve
	}
	return addrs, nil
}

// dialAddrs dials vetted addresses by number and returns the first connection.
func (d *Dialer) dialAddrs(ctx context.Context, network, port string, addrs []netip.Addr) (net.Conn, error) {
	dial := d.dial
	if dial == nil {
		nd := &net.Dialer{Timeout: d.Timeout}
		dial = nd.DialContext
	}
	for _, addr := range addrs {
		conn, err := dial(ctx, network, net.JoinHostPort(addr.Unmap().String(), port))
		if err == nil {
			return conn, nil
		}
		if ctx.Err() != nil {
			break
		}
	}
	return nil, ErrConnect
}

// NewClient returns an HTTP client that connects through d, ignores proxy
// environment variables, never follows redirects and gives up after timeout.
func NewClient(d *Dialer, timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:                  nil,
			DialContext:            d.DialContext,
			TLSHandshakeTimeout:    10 * time.Second,
			ResponseHeaderTimeout:  timeout,
			DisableKeepAlives:      true,
			MaxResponseHeaderBytes: 16 << 10,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}
