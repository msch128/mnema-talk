package setupcheck

import (
	"fmt"

	"github.com/msch128/mnema-talk/internal/opwarn"
)

func turnCheck(in Input, has map[string]bool) Check {
	cfg := in.Cfg
	p := Params{"port": fmt.Sprint(cfg.WebRTCBuiltinTURNPort)}
	external := len(cfg.WebRTCTURNURLs) > 0
	builtin := cfg.BuiltinTURNEnabled()
	switch {
	case builtin && has[opwarn.TURNUnavailable] && external:
		return newCheck(Turn, "turn.builtin_failed_external", OK, p)
	case builtin && has[opwarn.TURNUnavailable]:
		return newCheck(Turn, "turn.builtin_failed", Warn, p)
	case builtin && !in.TURNRunning && !external:
		return newCheck(Turn, "turn.voice_unavailable", Warn, p)
	case builtin && !in.TURNRunning:
		return newCheck(Turn, "turn.builtin_off_external", OK, p)
	case builtin && in.TURNProbe == "unreachable":
		return newCheck(Turn, "turn.unreachable", Info, p)
	case builtin:
		return newCheck(Turn, "turn.builtin_ok", OK, p)
	case external:
		return newCheck(Turn, "turn.external_ok", OK, p)
	}
	return newCheck(Turn, "turn.none", Warn, p)
}
