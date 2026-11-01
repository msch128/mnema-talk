// Package metrics holds the process-wide operational counters (HTTP, realtime,
// SFU, storage, rate limits) and renders them in the Prometheus text format
// without any client library. Every labelled family has a hard cap on its
// number of series, so a flood of unusual values cannot grow memory: past the
// cap new label values are folded into "other". Labels never carry user or
// channel ids.
package metrics

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// maxSeries bounds the series of one labelled family.
const maxSeries = 256

// maxHTTPSeries bounds the HTTP families. Their route label is the bounded set
// of router patterns (several hundred) times up to five status classes, so the
// generic cap would fold late routes, including their 4xx/5xx, into "other".
const maxHTTPSeries = 2048

// otherLabel replaces label values beyond maxSeries.
const otherLabel = "other"

// Counter is a monotonically increasing count.
type Counter struct{ v atomic.Uint64 }

// Inc adds one.
func (c *Counter) Inc() { c.v.Add(1) }

// Add adds n.
func (c *Counter) Add(n uint64) { c.v.Add(n) }

// Load returns the current value.
func (c *Counter) Load() uint64 { return c.v.Load() }

// Gauge is a value that moves both ways.
type Gauge struct{ v atomic.Int64 }

// Add changes the gauge by d.
func (g *Gauge) Add(d int64) { g.v.Add(d) }

// Load returns the current value.
func (g *Gauge) Load() int64 { return g.v.Load() }

// Vec is a counter family with fixed label names.
type Vec struct {
	name, help string
	labels     []string
	max        int
	mu         sync.RWMutex
	series     map[string]*Counter
}

func newVec(name, help string, labels ...string) *Vec {
	return &Vec{name: name, help: help, labels: labels, max: maxSeries, series: map[string]*Counter{}}
}

func newHTTPVec(name, help string, labels ...string) *Vec {
	v := newVec(name, help, labels...)
	v.max = maxHTTPSeries
	return v
}

// With returns the counter of one label combination.
func (v *Vec) With(values ...string) *Counter {
	key := strings.Join(values, "\x00")
	v.mu.RLock()
	c, ok := v.series[key]
	v.mu.RUnlock()
	if ok {
		return c
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if c, ok = v.series[key]; ok {
		return c
	}
	if len(v.series) >= v.max {
		other := make([]string, len(v.labels))
		for i := range other {
			other[i] = otherLabel
		}
		key = strings.Join(other, "\x00")
		if c, ok = v.series[key]; ok {
			return c
		}
	}
	c = &Counter{}
	v.series[key] = c
	return c
}

// Reset forgets every series. Tests that read the exposition use it so the
// series other tests created (bounded by maxSeries) cannot crowd theirs out.
func (v *Vec) Reset() {
	v.mu.Lock()
	defer v.mu.Unlock()
	clear(v.series)
}

// Total sums every series.
func (v *Vec) Total() uint64 {
	v.mu.RLock()
	defer v.mu.RUnlock()
	var n uint64
	for _, c := range v.series {
		n += c.Load()
	}
	return n
}

// Sum adds the series whose label at index i equals value.
func (v *Vec) Sum(i int, value string) uint64 {
	v.mu.RLock()
	defer v.mu.RUnlock()
	var n uint64
	for k, c := range v.series {
		if parts := strings.Split(k, "\x00"); i < len(parts) && parts[i] == value {
			n += c.Load()
		}
	}
	return n
}

func (v *Vec) write(w io.Writer) {
	v.mu.RLock()
	keys := make([]string, 0, len(v.series))
	for k := range v.series {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s counter\n", v.name, v.help, v.name)
	for _, k := range keys {
		fmt.Fprintf(w, "%s%s %d\n", v.name, labelString(v.labels, strings.Split(k, "\x00")), v.series[k].Load())
	}
	v.mu.RUnlock()
	fmt.Fprintln(w)
}

func labelString(names, values []string) string {
	var b strings.Builder
	b.WriteByte('{')
	for i, n := range names {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%s=%q", n, values[i])
	}
	b.WriteByte('}')
	return b.String()
}

// Buckets are the HTTP latency bounds in seconds.
var Buckets = []float64{0.005, 0.025, 0.1, 0.25, 0.5, 1, 2.5, 10}

// The process-wide instruments.
var (
	// HTTPRequests counts requests by route pattern and status class (2xx..5xx).
	HTTPRequests = newHTTPVec("mnema_http_requests_total", "HTTP requests by route pattern and status class", "route", "class")
	// HTTPDuration is the latency by route pattern.
	HTTPDuration = newHTTPHistVec("mnema_http_request_duration_seconds", "HTTP request latency by route pattern", "route")
	// HTTPInflight is the number of requests being served.
	HTTPInflight Gauge

	// RateLimitHits counts requests refused by a rate limiter, by limiter name.
	RateLimitHits = newVec("mnema_ratelimit_hits_total", "Requests refused by a rate limiter", "limiter")

	// WSConnections counts accepted realtime connections.
	WSConnections Counter
	// WSEventsSent counts events queued to clients.
	WSEventsSent Counter
	// WSEventsReceived counts events accepted from clients.
	WSEventsReceived Counter
	// WSDropped counts events dropped or connections cut, by reason:
	// slow_client, flood_frame, flood_event.
	WSDropped = newVec("mnema_ws_dropped_total", "Realtime events dropped or connections cut, by reason", "reason")

	// MLSHandshakes counts relayed or refused media_handshake messages, by
	// result: relayed, rejected, rate_limited.
	MLSHandshakes = newVec("mnema_mls_handshakes_total", "MLS handshake messages for voice rooms, by result", "result")
	// MLSHandshakeBytes is the size of relayed handshakes.
	MLSHandshakeBytes Counter

	// SFUIngressBytes/Packets are RTP received from publishers; the Egress
	// pair is RTP written to subscribers (one packet counts once per viewer).
	SFUIngressBytes, SFUIngressPackets Counter
	// SFUIngressDropped counts publisher packets dropped over the per-track
	// rate limit (reason "rate") and publishers closed for sustained
	// abuse (reason "closed").
	SFUIngressDropped                = newVec("mnema_sfu_ingress_dropped_total", "Publisher packets dropped over the ingress limit, by reason", "reason")
	SFUEgressBytes, SFUEgressPackets Counter
	// SFUNackRequests counts NACK feedback messages received from
	// subscribers, SFUNackPackets the packets they asked to resend.
	SFUNackRequests, SFUNackPackets Counter
	// SFURetransmitted counts the packets the SFU resent in answer.
	SFURetransmitted Counter
	// SFUKeyframeRequests counts PLI/FIR feedback received.
	SFUKeyframeRequests Counter
	// SFULayerSwitches counts simulcast layer changes for viewers.
	SFULayerSwitches Counter
	// SFUBandwidth counts the bandwidth estimates taken for automatic screen
	// viewers, by range (see BandwidthRange), so the distribution is visible
	// with a handful of series. SFUBandwidthMoves counts layer moves down that
	// the estimate caused.
	SFUBandwidth      = newVec("mnema_sfu_bandwidth_estimate_reports_total", "Bandwidth estimates of automatic screen viewers, by range", "range")
	SFUBandwidthMoves Counter
	// SFUICE counts ICE connection transitions, by state: connected, failed,
	// disconnected.
	SFUICE = newVec("mnema_sfu_ice_transitions_total", "SFU ICE connection transitions, by state", "state")
	// SFUPeerJoins counts media connections created.
	SFUPeerJoins Counter

	// WebhookDropped counts outgoing webhook events that were not delivered
	// because of a bound, by reason: buffer_full, channel_share, webhook_cap,
	// global_cap, expired, store_error.
	WebhookDropped = newVec("mnema_webhook_events_dropped_total", "Outgoing webhook events dropped or expired unsent, by reason", "reason")

	// PushDeliveries counts Web Push deliveries by outcome: ok, gone (the
	// browser's subscription ended), retry (temporary refusal, retried once),
	// rejected, error (no answer) and dropped (a full queue).
	PushDeliveries = newVec("mnema_push_deliveries_total", "Web Push deliveries, by outcome", "outcome")

	// TURNEvents counts the built-in TURN relay's events, by event:
	// allocation, auth_failed, forbidden, rate_limited, quota_user,
	// quota_total, peer_denied (a permission or channel for a peer that is
	// not the SFU), packet_blocked (a datagram to or from anywhere but the
	// SFU's media ports) and throttled (over the per-allocation byte rate).
	TURNEvents = newVec("mnema_turn_events_total", "Built-in TURN relay events and refused abuse attempts, by event", "event")
	// TURNRelayedBytes counts the bytes the built-in TURN relay forwarded, by
	// direction (to_sfu, from_sfu). TURNAllocations is the live allocations.
	TURNRelayedBytes = newVec("mnema_turn_relayed_bytes_total", "Bytes forwarded by the built-in TURN relay, by direction", "direction")
	TURNAllocations  Gauge

	// S3Operations counts object storage calls, by operation and result.
	S3Operations = newVec("mnema_s3_operations_total", "Object storage operations, by operation and result", "op", "result")
)

// ObserveHTTP records one finished request.
func ObserveHTTP(route string, status int, d time.Duration) {
	class := "5xx"
	switch {
	case status < 200:
		class = "1xx"
	case status < 300:
		class = "2xx"
	case status < 400:
		class = "3xx"
	case status < 500:
		class = "4xx"
	}
	HTTPRequests.With(route, class).Inc()
	HTTPDuration.Observe(route, d)
}

// S3 records one object storage call.
func S3(op string, err error) {
	result := "ok"
	if err != nil {
		result = "error"
	}
	S3Operations.With(op, result).Inc()
}

// WriteInstruments writes every instrument of this package.
func WriteInstruments(w io.Writer) {
	HTTPRequests.write(w)
	HTTPDuration.write(w)
	WriteGauge(w, "mnema_http_requests_in_flight", "HTTP requests being served", HTTPInflight.Load())
	RateLimitHits.write(w)
	writeCounter(w, "mnema_ws_connections_total", "Accepted realtime connections", WSConnections.Load())
	writeCounter(w, "mnema_ws_events_sent_total", "Realtime events queued to clients", WSEventsSent.Load())
	writeCounter(w, "mnema_ws_events_received_total", "Realtime events accepted from clients", WSEventsReceived.Load())
	WSDropped.write(w)
	MLSHandshakes.write(w)
	writeCounter(w, "mnema_mls_handshake_bytes_total", "Bytes of relayed MLS handshakes", MLSHandshakeBytes.Load())
	writeCounter(w, "mnema_sfu_ingress_bytes_total", "RTP bytes received from publishers", SFUIngressBytes.Load())
	writeCounter(w, "mnema_sfu_ingress_packets_total", "RTP packets received from publishers", SFUIngressPackets.Load())
	writeCounter(w, "mnema_sfu_egress_bytes_total", "RTP bytes written to subscribers", SFUEgressBytes.Load())
	writeCounter(w, "mnema_sfu_egress_packets_total", "RTP packets written to subscribers", SFUEgressPackets.Load())
	writeCounter(w, "mnema_sfu_nack_requests_total", "NACK feedback messages received from subscribers", SFUNackRequests.Load())
	writeCounter(w, "mnema_sfu_nack_packets_total", "Packets subscribers asked to be retransmitted", SFUNackPackets.Load())
	writeCounter(w, "mnema_sfu_retransmitted_packets_total", "Packets resent to subscribers after a NACK", SFURetransmitted.Load())
	writeCounter(w, "mnema_sfu_keyframe_requests_total", "PLI and FIR feedback messages received", SFUKeyframeRequests.Load())
	writeCounter(w, "mnema_sfu_layer_switches_total", "Simulcast layer switches for viewers", SFULayerSwitches.Load())
	writeCounter(w, "mnema_sfu_bandwidth_layer_moves_total", "Layer moves down caused by the bandwidth estimate", SFUBandwidthMoves.Load())
	SFUBandwidth.write(w)
	SFUICE.write(w)
	SFUIngressDropped.write(w)
	writeCounter(w, "mnema_sfu_peer_joins_total", "Media connections created", SFUPeerJoins.Load())
	TURNEvents.write(w)
	TURNRelayedBytes.write(w)
	WriteGauge(w, "mnema_turn_allocations", "Live allocations of the built-in TURN relay", TURNAllocations.Load())
	S3Operations.write(w)
	WebhookDropped.write(w)
	PushDeliveries.write(w)
}

func writeCounter(w io.Writer, name, help string, v uint64) {
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s counter\n%s %d\n\n", name, help, name, name, v)
}

// WriteGauge writes an integer gauge.
func WriteGauge(w io.Writer, name, help string, v int64) {
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n%s %d\n\n", name, help, name, name, v)
}

// WriteFloatGauge writes a gauge with a fractional value.
func WriteFloatGauge(w io.Writer, name, help string, v float64) {
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n%s %s\n\n", name, help, name, name, formatFloat(v))
}

// WriteFloatCounter writes a counter with a fractional value.
func WriteFloatCounter(w io.Writer, name, help string, v float64) {
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s counter\n%s %s\n\n", name, help, name, name, formatFloat(v))
}

// BandwidthRange names the bucket of a bandwidth estimate in bits per second;
// the set of names is fixed, so the label has few values.
func BandwidthRange(bps int) string {
	switch {
	case bps < 500_000:
		return "lt0.5m"
	case bps < 1_000_000:
		return "lt1m"
	case bps < 2_000_000:
		return "lt2m"
	case bps < 4_000_000:
		return "lt4m"
	case bps < 8_000_000:
		return "lt8m"
	case bps < 16_000_000:
		return "lt16m"
	}
	return "ge16m"
}
