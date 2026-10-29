package turnrelay

import (
	"net"
	"slices"
)

// Target describes where the SFU's media sockets are. The relay forwards to
// these and nowhere else.
type Target struct {
	// LocalIPs are the addresses the SFU's sockets are bound to on this
	// machine, IPv4 only.
	LocalIPs []net.IP
	// Announced are the addresses browsers are told the SFU is at
	// (WEBRTC_NAT_1TO1_IP); empty when it announces its own addresses.
	Announced []net.IP
	// PortMin and PortMax bound the UDP ports of the SFU's media sockets
	// (both the shared port when the SFU uses one).
	PortMin, PortMax uint16
}

// valid reports whether the target names something the relay may forward to.
// Without known ports it names nothing: forwarding to "any local port" would
// reach every other UDP service on the host.
func (t Target) valid() bool {
	return len(t.LocalIPs) > 0 && t.PortMin > 0 && t.PortMax >= t.PortMin
}

// relayIP is the address relayed sockets bind to and advertise: one the SFU
// listens on, preferably not loopback (a loopback relay address would be
// dropped by browsers' and Pion's candidate filters unless the SFU itself is
// loopback-only).
func (t Target) relayIP() net.IP {
	for _, ip := range t.LocalIPs {
		if !ip.IsLoopback() {
			return ip
		}
	}
	if len(t.LocalIPs) > 0 {
		return t.LocalIPs[0]
	}
	return nil
}

func containsIP(list []net.IP, ip net.IP) bool {
	return slices.ContainsFunc(list, func(x net.IP) bool { return x.Equal(ip) })
}

// isLocal reports whether ip is one of the SFU's own socket addresses.
func (t Target) isLocal(ip net.IP) bool { return containsIP(t.LocalIPs, ip) }

// isSFUAddress reports whether ip is an address a browser knows the SFU by:
// announced, or a local socket address.
func (t Target) isSFUAddress(ip net.IP) bool {
	return containsIP(t.Announced, ip) || t.isLocal(ip)
}

func (t Target) portOK(port int) bool {
	return port >= int(t.PortMin) && port <= int(t.PortMax) && port > 0
}

// concrete maps a virtual SFU address a client sends to onto a real local
// socket address, or reports false when the destination is not the SFU. The
// announced addresses (public IP, LAN IP) are not necessarily reachable from
// inside the host, so they map to the relay's own bind address.
func (t Target) concrete(dst *net.UDPAddr, bind net.IP) (*net.UDPAddr, bool) {
	if dst == nil || !t.portOK(dst.Port) {
		return nil, false
	}
	switch {
	case t.isLocal(dst.IP):
		return &net.UDPAddr{IP: dst.IP, Port: dst.Port}, true
	case containsIP(t.Announced, dst.IP):
		return &net.UDPAddr{IP: bind, Port: dst.Port}, true
	}
	return nil, false
}
