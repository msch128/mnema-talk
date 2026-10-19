package linkpreview

import (
	"context"
	"net"
	"net/netip"
	"net/url"

	"github.com/msch128/mnema-talk/internal/ssrf"
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

// nonPublic stays here: webhooks refuse 240.0.0.0/4 inside 224.0.0.0/3, not as its own prefix.
var nonPublic = []netip.Prefix{
	netip.MustParsePrefix("240.0.0.0/4"),
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
	if ssrf.Refused(addr) {
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
