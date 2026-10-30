package setupcheck

import (
	"fmt"
	"net/netip"
	"time"
)

// What the server saw while it ran, as opposed to what it found out at the
// start: the first member from outside who completed ICE, and whether
// browsers get through the reverse proxy with a WebSocket. Everything is in
// memory and starts empty with the process.
const (
	// wsFailWindow and wsFailLimit decide when failed upgrades are reported:
	// wsFailLimit misses within wsFailWindow with no success after them.
	wsFailWindow = 15 * time.Minute
	wsFailLimit  = 3
	// wsFailKept bounds the remembered misses.
	wsFailKept = 16
)

// cgnat is the shared address space of carrier-grade NAT (RFC 6598).
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// IsOutsideAddress reports whether a member reached the server from beyond
// its own network: a global unicast address that is neither private, loopback,
// link-local nor carrier-grade-NAT shared space.
func IsOutsideAddress(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsValid() || !a.IsGlobalUnicast() {
		return false
	}
	return !a.IsPrivate() && !a.IsLoopback() && !a.IsLinkLocalUnicast() && !cgnat.Contains(a)
}

// RecordICEConnected notes that a media connection finished ICE with a peer
// at remote. Only a peer outside the server's network counts; it confirms
// that the voice port is reachable from outside.
func (r *Runtime) RecordICEConnected(remote netip.Addr, now time.Time) {
	if r == nil {
		return
	}
	if !IsOutsideAddress(remote) {
		return
	}
	r.mu.Lock()
	r.outsideAt = now
	r.mu.Unlock()
}

// RecordWebSocket notes the outcome of one browser's attempt to open /api/ws:
// upgraded (ok) or answered without the upgrade headers, which is what a
// reverse proxy without WebSocket support produces.
func (r *Runtime) RecordWebSocket(ok bool, now time.Time) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if ok {
		r.wsOKAt, r.wsFails = now, nil
		return
	}
	r.wsFails = append(r.wsFails, now)
	if len(r.wsFails) > wsFailKept {
		r.wsFails = r.wsFails[len(r.wsFails)-wsFailKept:]
	}
}

// recentWSFails counts the misses within the window ending at now. A success
// clears them, so the count is of misses since the last success.
func recentWSFails(fails []time.Time, now time.Time) (int, time.Time) {
	n, last := 0, time.Time{}
	for _, f := range fails {
		if now.Sub(f) <= wsFailWindow {
			n++
			last = f
		}
	}
	return n, last
}

func voiceReachCheck(rt runtimeSnapshot, p Params) Check {
	if rt.outsideAt.IsZero() {
		return newCheck(VoiceUDP, "voice_udp.unconfirmed", Info, p)
	}
	p["at"], p["time"] = rt.outsideAt.UTC().Format(time.RFC3339), stamp(rt.outsideAt)
	return newCheck(VoiceUDP, "voice_udp.confirmed", OK, p)
}

func websocketCheck(rt runtimeSnapshot, now time.Time) Check {
	if n, last := recentWSFails(rt.wsFails, now); n >= wsFailLimit {
		return newCheck(WebSocket, "websocket.failing", Warn, Params{"count": fmt.Sprint(n), "time": stamp(last)})
	}
	if rt.wsOKAt.IsZero() {
		return newCheck(WebSocket, "websocket.unchecked", Info, nil)
	}
	return newCheck(WebSocket, "websocket.ok", OK, Params{"at": rt.wsOKAt.UTC().Format(time.RFC3339), "time": stamp(rt.wsOKAt)})
}

func stamp(t time.Time) string { return t.UTC().Format("2006-01-02 15:04 UTC") }
