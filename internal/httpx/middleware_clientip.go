package httpx

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync/atomic"
	"time"

	"github.com/msch128/mnema-talk/internal/opwarn"
)

func normalizeCIDR(v string) string {
	if strings.Contains(v, "/") {
		return v
	}
	if strings.Contains(v, ":") {
		return v + "/128"
	}
	return v + "/32"
}

// ParseCIDRs parses CIDRs or bare IPs into networks.
func ParseCIDRs(values []string) ([]*net.IPNet, error) {
	var out []*net.IPNet
	for _, v := range values {
		cidr := normalizeCIDR(v)
		_, n, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR %q: %w", v, err)
		}
		out = append(out, n)
	}
	return out, nil
}

func peerHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// remoteTrusted reports whether host may set forwarding headers: loopback
// always, otherwise only peers inside a configured trusted-proxy CIDR.
func remoteTrusted(host string, trusted []*net.IPNet) bool {
	addr, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	if addr.IsLoopback() {
		return true
	}
	ip := net.IP(addr.AsSlice())
	for _, n := range trusted {
		if n != nil && n.Contains(ip) {
			return true
		}
	}
	return false
}

// ClientIPMiddleware resolves the client IP once per request. X-Forwarded-For
// is only consulted when the direct peer is a trusted proxy, and then the
// right-most hop that is not itself a trusted proxy wins: entries left of it
// were supplied by the client and cannot be trusted for rate limiting.
func ClientIPMiddleware(trusted []*net.IPNet) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		var lastWarn atomic.Int64
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			client := peerHost(r)
			if !remoteTrusted(client, trusted) {
				warnUntrustedForwarded(r, client, &lastWarn)
			} else {
				// A proxy may append its hop as a separate header line instead
				// of extending the first one; all lines form one list (RFC 9110).
				hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
				for i := len(hops) - 1; i >= 0; i-- {
					addr, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
					if err != nil {
						break
					}
					client = addr.Unmap().String()
					if !remoteTrusted(client, trusted) {
						break
					}
				}
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientIPKey, client)))
		})
	}
}

// warnUntrustedForwarded logs (at most hourly) that a private-range peer sent
// X-Forwarded-For but is not a trusted proxy: the header is ignored, which
// is right for a stranger but means a legitimate reverse proxy is missing
// from TRUSTED_PROXY_CIDRS and all clients share its address.
func warnUntrustedForwarded(r *http.Request, peer string, last *atomic.Int64) {
	if r.Header.Get("X-Forwarded-For") == "" && r.Header.Get("X-Real-IP") == "" {
		return
	}
	addr, err := netip.ParseAddr(peer)
	if err != nil || !(addr.IsPrivate() || addr.IsLinkLocalUnicast()) {
		return
	}
	now := time.Now().Unix()
	prev := last.Load()
	if now-prev < 3600 || !last.CompareAndSwap(prev, now) {
		return
	}
	slog.Warn("requests arrive through a proxy but this peer is not in TRUSTED_PROXY_CIDRS (empty = loopback only): all clients share one rate-limit identity; add the reverse proxy's address to TRUSTED_PROXY_CIDRS",
		"peer", peer)
	opwarn.Raise(opwarn.ProxyNotTrusted, 0)
}

// ClientIP returns the IP resolved by ClientIPMiddleware (or the raw peer).
func ClientIP(r *http.Request) string {
	if ip, ok := r.Context().Value(clientIPKey).(string); ok {
		return ip
	}
	return peerHost(r)
}

// ClientIPKey is ClientIP reduced to the unit a single client controls, for
// rate-limit and lockout keys: IPv4 addresses as they are, IPv6 addresses
// masked to their /64 (one customer prefix hands out 2^64 addresses, so
// per-/128 keys would let one client rotate around every limit).
func ClientIPKey(r *http.Request) string {
	return IPKey(ClientIP(r))
}

// ClientAggregateKey is the coarser limiter key for budgets that must hold
// against an attacker who owns a whole site allocation: IPv4 addresses as they
// are, IPv6 addresses masked to their /48 (65536 /64 prefixes).
func ClientAggregateKey(r *http.Request) string {
	addr, err := netip.ParseAddr(strings.Trim(ClientIP(r), "[]"))
	if err != nil {
		return ClientIP(r)
	}
	addr = addr.Unmap()
	if addr.Is4() {
		return addr.String()
	}
	return netip.PrefixFrom(addr.WithZone(""), 48).Masked().String()
}

// IPKey normalizes ip for use as a limiter key (see ClientIPKey). Anything
// that does not parse as an IP is returned unchanged.
func IPKey(ip string) string {
	addr, err := netip.ParseAddr(strings.Trim(ip, "[]"))
	if err != nil {
		return ip
	}
	addr = addr.Unmap()
	if addr.Is4() {
		return addr.String()
	}
	return netip.PrefixFrom(addr.WithZone(""), 64).Masked().String()
}
