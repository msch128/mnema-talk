package sfu

import (
	"net"
)

// Endpoints says where the SFU's media sockets are, for the built-in TURN
// relay that forwards to them and to nothing else.
type Endpoints struct {
	// Local are the IPv4 addresses the sockets are bound to on this machine.
	Local []net.IP
	// Announced are the addresses browsers are told instead
	// (WEBRTC_NAT_1TO1_IP); empty when the sockets are announced as they are.
	Announced []net.IP
	// PortMin and PortMax bound the UDP ports of the media sockets: both the
	// shared port with a UDP mux. Both are zero when the SFU uses unfixed
	// ports, which nothing may be relayed to.
	PortMin, PortMax uint16
}

// Endpoints returns the current media endpoints. The announced addresses
// follow SetAnnouncedIPs.
func (s *SFU) Endpoints() Endpoints {
	_, announced := s.apiAndAnnounced()
	ep := Endpoints{Announced: ipv4s(announced)}
	if s.udpMux != nil {
		for _, a := range s.udpMux.GetListenAddresses() {
			if u, ok := a.(*net.UDPAddr); ok {
				if ip := u.IP.To4(); ip != nil && !containsIP(ep.Local, ip) {
					ep.Local = append(ep.Local, ip)
				}
				ep.PortMin, ep.PortMax = uint16(u.Port), uint16(u.Port)
			}
		}
		return ep
	}
	if s.portMin == 0 || s.portMax < s.portMin {
		return ep
	}
	ep.PortMin, ep.PortMax = s.portMin, s.portMax
	ep.Local = interfaceIPv4s(onlyLoopbackAnnouncements(announced), announcesLoopback(announced))
	return ep
}

func ipv4s(list []string) []net.IP {
	var out []net.IP
	for _, s := range list {
		if ip := net.ParseIP(s).To4(); ip != nil {
			out = append(out, ip)
		}
	}
	return out
}

func containsIP(list []net.IP, ip net.IP) bool {
	for _, x := range list {
		if x.Equal(ip) {
			return true
		}
	}
	return false
}

// interfaceIPv4s lists the machine's IPv4 addresses the way ICE gathers host
// candidates: loopback only when the SFU announces it, and then alone when it
// announces nothing else.
func interfaceIPv4s(loopbackOnly, withLoopback bool) []net.IP {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var out []net.IP
	for _, a := range addrs {
		n, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip := n.IP.To4()
		if ip == nil || ip.IsUnspecified() || ip.IsLinkLocalUnicast() {
			continue
		}
		if ip.IsLoopback() && !loopbackOnly && !withLoopback {
			continue
		}
		if !ip.IsLoopback() && loopbackOnly {
			continue
		}
		out = append(out, ip)
	}
	return out
}
