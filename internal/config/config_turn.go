package config

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// The values of WEBRTC_BUILTIN_TURN.
const (
	BuiltinTURNAuto = "auto"
	BuiltinTURNOn   = "on"
	BuiltinTURNOff  = "off"
)

// BuiltinTURNEnabled reports whether the settings ask for the TURN relay built
// into the server. "auto" is the default: it runs the relay only while no
// external TURN server is configured, so an installation that already points
// WEBRTC_TURN_URLS at coturn behaves exactly as before. "on" runs it in any
// case; with an external TURN configured as well, members are offered both.
func (c *Config) BuiltinTURNEnabled() bool {
	switch c.WebRTCBuiltinTURN {
	case BuiltinTURNOn:
		return true
	case BuiltinTURNOff:
		return false
	}
	return len(c.WebRTCTURNURLs) == 0
}

// BuiltinTURNWithExternal reports whether the built-in relay is forced on
// next to a configured external TURN server.
func (c *Config) BuiltinTURNWithExternal() bool {
	return c.WebRTCBuiltinTURN == BuiltinTURNOn && len(c.WebRTCTURNURLs) > 0
}

func (c *Config) validateBuiltinTURN() error {
	switch c.WebRTCBuiltinTURN {
	case BuiltinTURNAuto, BuiltinTURNOn, BuiltinTURNOff:
	default:
		return fmt.Errorf("WEBRTC_BUILTIN_TURN must be auto, on or off, got %q", c.WebRTCBuiltinTURN)
	}
	// The host ends up in the TURN URLs of every member, so it is checked
	// whenever the relay can run (auto without an external TURN, or on).
	if h := strings.TrimSpace(c.WebRTCBuiltinTURNHost); c.BuiltinTURNEnabled() && strings.ContainsAny(h, "/:@ ?#") && net.ParseIP(h) == nil {
		return fmt.Errorf("WEBRTC_BUILTIN_TURN_HOST must be a host name or IP address without scheme, port or path, got %q", h)
	}
	// Only an explicit "on" can fail the start over the port: under "auto" an
	// installation that never asked for the relay keeps starting whatever the
	// port says, and the relay itself declines (with a warning) when it
	// cannot run.
	if c.WebRTCBuiltinTURN != BuiltinTURNOn {
		return nil
	}
	if c.WebRTCBuiltinTURNPort == 0 {
		return fmt.Errorf("WEBRTC_BUILTIN_TURN_PORT must be a port number (1-65535), got 0")
	}
	if p := c.WebRTCBuiltinTURNPort; p >= c.WebRTCUDPPortMin && p <= c.WebRTCUDPPortMax {
		return fmt.Errorf("WEBRTC_BUILTIN_TURN_PORT %d must lie outside the WebRTC UDP port range %d-%d", p, c.WebRTCUDPPortMin, c.WebRTCUDPPortMax)
	}
	return nil
}

// TURNPublicHost is the host name or address browsers reach the built-in TURN
// relay at: WEBRTC_BUILTIN_TURN_HOST when set (for a PUBLIC_URL name that only
// forwards HTTP, such as a CDN or tunnel), otherwise the host of PUBLIC_URL
// (the relay listens on the same machine the reverse proxy in front of the app
// runs on). Behind a PUBLIC_URL that names
// no reachable host (localhost, an unset default) it falls back to the first
// WEBRTC_NAT_1TO1_IP entry. It is empty when neither names a host.
func (c *Config) TURNPublicHost() string {
	if h := strings.TrimSpace(c.WebRTCBuiltinTURNHost); h != "" {
		return h
	}
	if u, err := url.Parse(c.PublicURL); err == nil {
		if h := u.Hostname(); h != "" && !isLocalName(h) {
			return h
		}
	}
	for _, a := range c.WebRTCAnnounce {
		if a != "" {
			return a
		}
	}
	if u, err := url.Parse(c.PublicURL); err == nil {
		return u.Hostname() // localhost and the like: right for a single-machine test setup
	}
	return ""
}

func isLocalName(h string) bool {
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && (ip.IsLoopback() || ip.IsUnspecified())
}
