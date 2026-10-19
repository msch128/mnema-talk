package linkpreview

import (
	"context"
	"net"
	"net/netip"
	"net/url"
)

// checkResolvedAddress applies the address policy after DNS resolution.
// The native dialer calls it for every connection attempt, including redirects.
func (f *Fetcher) checkResolvedAddress(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return ErrBlocked
	}
	if f.allowPrivate {
		return nil
	}
	ip := net.ParseIP(host)
	if !publicIP(ip) || f.isDenied(ip) {
		return ErrBlocked
	}
	return nil
}

// nonPublic are special-purpose ranges that Go's IsGlobalUnicast accepts but
// that must never be fetched: shared, documentation, benchmarking and
// reserved IPv4 space, the deprecated 6to4 relay anycast and IPv6
// site-local ranges, and IPv6 prefixes that embed an IPv4 address (NAT64,
// 6to4, Teredo), which could point at a private host.
var nonPublic = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("fec0::/10"),
}

// publicIP reports whether ip is a globally routable unicast address.
func publicIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return false
	}
	for _, p := range nonPublic {
		if p.Contains(addr) {
			return false
		}
	}
	return true
}

// Deny refuses the addresses of these hosts or IPs; each call replaces the
// list. Host names are resolved now (call Deny again to refresh them). When a
// lookup fails, the addresses that host resolved to last time stay denied,
// so a DNS hiccup never opens the server's own public address. Used for the
// server's own public address.
func (f *Fetcher) Deny(ctx context.Context, hostsOrIPs ...string) {
	f.deniedMu.RLock()
	previous := f.deniedHosts
	f.deniedMu.RUnlock()

	hosts := map[string][]netip.Addr{}
	set := map[netip.Addr]bool{}
	for _, h := range hostsOrIPs {
		if h == "" {
			continue
		}
		if a, err := netip.ParseAddr(h); err == nil {
			set[a.Unmap()] = true
			continue
		}
		addrs, err := f.lookup(ctx, h)
		if err != nil || len(addrs) == 0 {
			addrs = previous[h]
		}
		hosts[h] = addrs
		for _, a := range addrs {
			set[a.Unmap()] = true
		}
	}
	f.deniedMu.Lock()
	f.denied, f.deniedHosts = set, hosts
	f.deniedMu.Unlock()
}

func (f *Fetcher) isDenied(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	f.deniedMu.RLock()
	defer f.deniedMu.RUnlock()
	return f.denied[addr.Unmap()]
}

// checkURL allows only plain http(s) URLs on the default ports.
func (f *Fetcher) checkURL(u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return ErrBlocked
	}
	if u.User != nil || u.Hostname() == "" {
		return ErrBlocked
	}
	if p := u.Port(); p != "" && p != "80" && p != "443" && !f.allowPrivate {
		return ErrBlocked
	}
	return nil
}
