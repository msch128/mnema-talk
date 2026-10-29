package setupcheck

import (
	"fmt"

	"github.com/msch128/mnema-talk/internal/opwarn"
)

func turnCheck(in Input, has map[string]bool) Check {
	cfg := in.Cfg
	c := Check{ID: Turn}
	port := cfg.WebRTCBuiltinTURNPort
	external := len(cfg.WebRTCTURNURLs) > 0
	switch {
	case cfg.BuiltinTURNEnabled() && has[opwarn.TURNUnavailable]:
		if external {
			c.Level = OK
			c.Detail = "A TURN relay is configured for members behind strict firewalls; the built-in relay could not start."
			return c
		}
		c.Level = Warn
		c.Detail = fmt.Sprintf("The built-in TURN relay could not listen on port %d: members on networks that block UDP cannot join voice.", port)
		c.Fix = fmt.Sprintf("Free port %d (UDP and TCP) or pick another with WEBRTC_BUILTIN_TURN_PORT, then run docker compose up -d.", port)
	case cfg.BuiltinTURNEnabled() && !in.TURNRunning && !external:
		c.Level = Warn
		c.Detail = "The built-in TURN relay is not running because voice is unavailable: members on networks that block UDP cannot join voice."
		c.Fix = "Fix the voice engine first (see the Voice port check), then run docker compose up -d."
	case cfg.BuiltinTURNEnabled() && !in.TURNRunning:
		c.Level = OK
		c.Detail = "A TURN relay is configured for members behind strict firewalls; the built-in relay is not running."
	case cfg.BuiltinTURNEnabled() && in.TURNProbe == "unreachable":
		c.Level = Warn
		c.Detail = fmt.Sprintf("The built-in TURN relay runs, but port %d did not answer from this server; a router without NAT loopback causes this although the forward works.", port)
		c.Fix = fmt.Sprintf("Forward port %d (UDP and TCP) to this server and test from a phone on mobile data; set WEBRTC_BUILTIN_TURN_HOST if members must reach it under another name.", port)
	case cfg.BuiltinTURNEnabled():
		c.Level = OK
		c.Detail = fmt.Sprintf("The built-in TURN relay serves members behind strict firewalls on port %d; forward it (UDP and TCP) next to the voice ports.", port)
	case external:
		c.Level = OK
		c.Detail = "A TURN relay is configured for members behind strict firewalls."
	default:
		c.Level = Warn
		c.Detail = "No TURN relay: members on networks that block UDP cannot join voice."
		c.Fix = fmt.Sprintf("Optional: remove WEBRTC_BUILTIN_TURN=off to use the built-in relay (forward port %d, UDP and TCP), or set WEBRTC_TURN_URLS and WEBRTC_TURN_SECRET for an external one.", port)
	}
	return c
}
