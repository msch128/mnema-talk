// Package opwarn collects operator warnings: configuration or deployment
// problems the server detects at startup or at runtime. They are logged where
// they arise and also registered here under a stable code, so the admin System
// tab can show them. The registry is deduplicated by code and bounded, and
// holds no secrets: only the code and a few non-sensitive numeric facts.
package opwarn

import (
	"sort"
	"sync"
	"time"
)

// Stable warning codes. The web app maps each to an explanation and a doc
// link; never reuse or rename a code.
const (
	TrustedProxyBroad    = "trusted_proxy_broad"
	ProxyNotTrusted      = "proxy_not_trusted"
	WebRTCReachability   = "webrtc_reachability"
	WebRTCUDPMuxDisabled = "webrtc_udp_mux_disabled"
	VoiceUnavailable     = "voice_unavailable"
)

// MaxWarnings bounds the registry; further new codes are dropped.
const MaxWarnings = 32

// Warning is one active operator warning.
type Warning struct {
	// Code is the stable identifier.
	Code string `json:"code"`
	// Since is when the warning was first raised in this process.
	Since time.Time `json:"since" format:"date-time"`
	// Count is how often it was raised since then (for runtime warnings).
	Count int `json:"count"`
	// Value is an optional non-sensitive number (e.g. the UDP port count).
	Value int `json:"value,omitempty" binding:"optional"`
}

// Registry is a concurrency-safe, bounded set of warnings keyed by code.
type Registry struct {
	mu  sync.Mutex
	now func() time.Time
	m   map[string]*Warning
}

// New returns an empty registry.
func New() *Registry {
	return &Registry{now: time.Now, m: make(map[string]*Warning)}
}

// Default is the process-wide registry.
var Default = New()

// Raise registers (or repeats) the warning with the given code. Empty codes
// and new codes beyond MaxWarnings are ignored.
func (r *Registry) Raise(code string, value int) {
	if r == nil || code == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if w, ok := r.m[code]; ok {
		w.Count++
		w.Value = value
		return
	}
	if len(r.m) >= MaxWarnings {
		return
	}
	r.m[code] = &Warning{Code: code, Since: r.now().UTC(), Count: 1, Value: value}
}

// Clear removes a warning whose cause is gone.
func (r *Registry) Clear(code string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	delete(r.m, code)
	r.mu.Unlock()
}

// List returns the active warnings ordered by code. It never returns nil.
func (r *Registry) List() []Warning {
	out := []Warning{}
	if r == nil {
		return out
	}
	r.mu.Lock()
	for _, w := range r.m {
		out = append(out, *w)
	}
	r.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

// Raise registers a warning on the Default registry.
func Raise(code string, value int) { Default.Raise(code, value) }
