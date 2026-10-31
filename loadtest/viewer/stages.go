package main

import (
	"fmt"
)

// stageSplit follows the packets of the window through the three hops, to tell
// where they go missing: from the generator into the SFU, through the SFU's
// fan-out, and from the SFU back into the generator. All numbers cover the same
// window (see window.go). Remaining bias: packets in flight at either end of the
// window (a fraction of a second of traffic), retransmissions (counted by the
// SFU and the receivers like any packet, so a surplus is not shown as loss), and
// packets the SFU drops on purpose (its ingress rate limit, a viewer paused at a
// layer switch), which show as missing at the hop where they are dropped.
type stageSplit struct {
	WindowS           float64 `json:"window_s"`
	Audience          int     `json:"audience"`
	GeneratorSent     int64   `json:"generator_sent"`
	SFUIngress        int64   `json:"sfu_ingress"`
	IngressMissing    int64   `json:"ingress_missing"`
	IngressMissingPct float64 `json:"ingress_missing_pct"`
	// ExpectedEgress is what the SFU should write: the packets that reached it
	// times the connected members that watch each track.
	ExpectedEgress     int64   `json:"expected_egress"`
	SFUEgress          int64   `json:"sfu_egress"`
	EgressMissing      int64   `json:"egress_missing"`
	EgressMissingPct   float64 `json:"egress_missing_pct"`
	GeneratorReceived  int64   `json:"generator_received"`
	ReceiveMissing     int64   `json:"receive_missing"`
	ReceiveMissingPct  float64 `json:"receive_missing_pct"`
	ServerCountersUsed bool    `json:"server_counters_used"`
}

// splitStages computes the split; nil when no publisher sent anything.
func splitStages(in stageInput, c viewerConfig) *stageSplit {
	st := &stageSplit{WindowS: in.window.Seconds()}
	for _, ok := range in.audience {
		if ok {
			st.Audience++
		}
	}
	watchers := c.cameras.watchers(c.memberIDs(), c.cameraIDs(), in.audience)
	var expected float64
	for id, sent := range in.sent {
		if !in.audience[id] {
			continue // a publisher that dropped out sent an unknown share
		}
		st.GeneratorSent += sent
		switch in.roles[id] {
		case "camera":
			expected += float64(sent) * float64(watchers[id])
		case "screen", "audio":
			expected += float64(sent) * float64(st.Audience-1)
		}
	}
	if st.GeneratorSent == 0 {
		return nil
	}
	for id, n := range in.recv {
		if in.audience[id] {
			st.GeneratorReceived += n
		}
	}
	ing, eg := in.server["mnema_sfu_ingress_packets_total"], in.server["mnema_sfu_egress_packets_total"]
	if !in.hasServer || ing == 0 {
		st.ExpectedEgress = int64(expected)
		st.ReceiveMissing, st.ReceiveMissingPct = missing(st.ExpectedEgress, st.GeneratorReceived)
		return st
	}
	st.ServerCountersUsed = true
	st.SFUIngress, st.SFUEgress = int64(ing), int64(eg)
	st.IngressMissing, st.IngressMissingPct = missing(st.GeneratorSent, st.SFUIngress)
	st.ExpectedEgress = int64(expected * min(1, ing/float64(st.GeneratorSent)))
	st.EgressMissing, st.EgressMissingPct = missing(st.ExpectedEgress, st.SFUEgress)
	st.ReceiveMissing, st.ReceiveMissingPct = missing(st.SFUEgress, st.GeneratorReceived)
	return st
}

// missing returns how much of want did not arrive as got, and the percentage.
// A surplus (retransmissions, packets of other users) is not negative loss.
func missing(want, got int64) (int64, float64) {
	if want <= 0 || got >= want {
		return 0, 0
	}
	return want - got, float64(want-got) / float64(want) * 100
}

// lines renders the split for the console.
func (s *stageSplit) lines() []string {
	out := []string{fmt.Sprintf("packets      %.0f s window, %d members connected throughout; generator sent %d",
		s.WindowS, s.Audience, s.GeneratorSent)}
	if !s.ServerCountersUsed {
		return append(out, fmt.Sprintf("             no server counters (METRICS_TOKEN): expected to arrive %d, received %d (%.2f%% missing)",
			s.ExpectedEgress, s.GeneratorReceived, s.ReceiveMissingPct))
	}
	return append(out,
		fmt.Sprintf("  into SFU   ingress %d, missing %d (%.2f%%)", s.SFUIngress, s.IngressMissing, s.IngressMissingPct),
		fmt.Sprintf("  fan-out    expected egress %d (ingress x watchers), egress %d, missing %d (%.2f%%)", s.ExpectedEgress, s.SFUEgress, s.EgressMissing, s.EgressMissingPct),
		fmt.Sprintf("  out of SFU received %d of egress %d, missing %d (%.2f%%)", s.GeneratorReceived, s.SFUEgress, s.ReceiveMissing, s.ReceiveMissingPct))
}
