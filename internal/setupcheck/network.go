package setupcheck

import (
	"context"
	"log/slog"
	"net"
	"net/netip"
	"os"

	"github.com/msch128/mnema-talk/internal/config"
)

// cgnatRange is the shared address space (RFC 6598) providers use for
// carrier-grade NAT: a WAN address in it is not reachable from the internet.
var cgnatRange = netip.MustParsePrefix("100.64.0.0/10")

// IsCGNAT reports whether a is in 100.64.0.0/10.
func IsCGNAT(a netip.Addr) bool { return a.IsValid() && cgnatRange.Contains(a.Unmap()) }

// Host is what the server can see of the machine it runs on.
type Host struct {
	// Local is the IPv4 address this host uses towards the internet (the
	// default route's interface); invalid when unknown. In a Docker bridge
	// network this is the container's address, not the machine's.
	Local netip.Addr
	// InContainer is whether the process runs in a container, where Local is
	// not the LAN address of the machine.
	InContainer bool
}

// SystemHost reads the host's default-route address and whether this is a
// container. Dialing a UDP socket sends nothing; it only picks the route.
func SystemHost() Host {
	h := Host{InContainer: inContainer()}
	if c, err := net.Dial("udp4", "192.0.2.1:9"); err == nil {
		if ua, ok := c.LocalAddr().(*net.UDPAddr); ok {
			if a, ok := netip.AddrFromSlice(ua.IP); ok {
				h.Local = a.Unmap()
			}
		}
		_ = c.Close()
	}
	return h
}

func inContainer() bool {
	for _, f := range []string{"/.dockerenv", "/run/.containerenv"} {
		if _, err := os.Stat(f); err == nil {
			return true
		}
	}
	return false
}

// LANAnnounce returns the host's LAN addresses to announce next to the public
// one, so members on the same network reach voice without NAT loopback.
// WEBRTC_LAN_IP wins. Without it only a process outside a container knows its
// LAN address: in a Docker bridge network the address it sees belongs to the
// container and no browser can reach it, so nothing is guessed there.
func LANAnnounce(cfg *config.Config, h Host) []string {
	if len(cfg.WebRTCLANIPs) > 0 {
		return append([]string(nil), cfg.WebRTCLANIPs...)
	}
	if !h.InContainer && h.Local.IsValid() && h.Local.Is4() && h.Local.IsPrivate() {
		return []string{h.Local.String()}
	}
	return nil
}

// NetworkFacts are the observations ClassifyNetwork judges.
type NetworkFacts struct {
	// STUN is the IPv4 address STUN reported; invalid when the request failed
	// or was not made.
	STUN netip.Addr
	// STUNFailed is whether the IPv4 STUN request was made and failed.
	STUNFailed bool
	// IPv6OK is whether a global IPv6 address was found with STUN.
	IPv6OK bool
	// Local is the host's default-route IPv4 address.
	Local netip.Addr
}

// Outcome codes of the network check.
const (
	codeCGNAT           = "network.cgnat"
	codeNoIPv4          = "network.no_ipv4"
	codeAddressMismatch = "network.address_mismatch"
)

// ClassifyNetwork says whether the connection cannot take port forwarding, as
// the code of the network check and its parameters; "" means nothing to report.
//   - the WAN address is in 100.64.0.0/10 (carrier-grade NAT);
//   - IPv4 detection fails while IPv6 works (DS-Lite and similar);
//   - the host has a public address of its own that differs from the one STUN
//     sees, so its traffic leaves through another gateway.
func ClassifyNetwork(f NetworkFacts) (string, Params) {
	switch {
	case IsCGNAT(f.STUN):
		return codeCGNAT, Params{"address": f.STUN.String()}
	case IsCGNAT(f.Local):
		return codeCGNAT, Params{"address": f.Local.String()}
	case f.STUNFailed && f.IPv6OK:
		return codeNoIPv4, nil
	case f.STUN.IsValid() && f.Local.IsValid() && f.Local.IsGlobalUnicast() && !f.Local.IsPrivate() && f.Local != f.STUN:
		return codeAddressMismatch, Params{"local": f.Local.String(), "public": f.STUN.String()}
	}
	return "", nil
}

// JudgeNetwork gathers the facts after the announcement was resolved, records
// the verdict for the Status page and logs it. The IPv6 STUN request is made
// only when the IPv4 one failed. detect6 may be nil (no request). Development
// mode and a PUBLIC_URL on this machine have nothing to judge.
func JudgeNetwork(ctx context.Context, cfg *config.Config, ann Announcement, h Host, detect6 Detector, rt *Runtime) {
	if !cfg.IsProduction() || cfg.PublicURLIsLocal() {
		return
	}
	f := NetworkFacts{STUN: ann.DetectedIP, STUNFailed: ann.DetectErr != "", Local: h.Local}
	if f.STUNFailed && detect6 != nil {
		_, _, err := detect6(ctx, cfg.WebRTCDetectSTUN, detectTimeout)
		f.IPv6OK = err == nil
	}
	code, p := ClassifyNetwork(f)
	if code == "" {
		return
	}
	rt.SetNetwork(code, p)
	c := networkCheck(code, p)
	slog.Warn("setup check: "+c.Detail, "check", Network, "code", code, "fix", c.Fix)
}

func networkCheck(code string, p Params) Check {
	level := Warn
	if code == codeCGNAT {
		level = Fail
	}
	return newCheck(Network, code, level, p)
}

// NetworkCheck renders a network.* outcome as a check (level, English detail
// and fix), for callers that report it outside the Status page.
func NetworkCheck(code string, p Params) Check { return networkCheck(code, p) }
