package main

import "fmt"

// stageSplit follows the packets of a run through the three hops, to tell
// where they go missing: from the generator into the SFU, through the SFU's
// fan-out, and from the SFU back into the generator. The server's counters are
// those of the whole run; the generator's are the packets of its publishers
// and of all received tracks.
type stageSplit struct {
	GeneratorSent     int64   `json:"generator_sent"`
	SFUIngress        int64   `json:"sfu_ingress"`
	IngressMissing    int64   `json:"ingress_missing"`
	IngressMissingPct float64 `json:"ingress_missing_pct"`
	// ExpectedEgress is what the SFU should write: the packets of every
	// published track, as far as they reached it, times the members that
	// watch the track.
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
func splitStages(res *result, c viewerConfig) *stageSplit {
	st := &stageSplit{}
	watchers := c.cameras.watchers(c.memberIDs(), c.cameraIDs())
	n := int64(len(res.PerMember))
	var expected float64
	for _, m := range res.PerMember {
		st.GeneratorSent += m.SentPackets
		switch m.Role {
		case "camera":
			expected += float64(m.SentPackets) * float64(watchers[m.ID])
		case "screen", "audio":
			expected += float64(m.SentPackets) * float64(n-1)
		}
	}
	if st.GeneratorSent == 0 {
		return nil
	}
	for _, r := range res.Media {
		st.GeneratorReceived += r.Packets
	}
	ing, eg := res.Server.RunDeltas["mnema_sfu_ingress_packets_total"], res.Server.RunDeltas["mnema_sfu_egress_packets_total"]
	if !res.Server.Measured || ing == 0 {
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
	out := []string{fmt.Sprintf("packets      generator sent %d", s.GeneratorSent)}
	if !s.ServerCountersUsed {
		return append(out, fmt.Sprintf("             no server counters (METRICS_TOKEN): expected to arrive %d, received %d (%.2f%% missing)",
			s.ExpectedEgress, s.GeneratorReceived, s.ReceiveMissingPct))
	}
	return append(out,
		fmt.Sprintf("  into SFU   ingress %d, missing %d (%.2f%%)", s.SFUIngress, s.IngressMissing, s.IngressMissingPct),
		fmt.Sprintf("  fan-out    expected egress %d (ingress x watchers), egress %d, missing %d (%.2f%%)", s.ExpectedEgress, s.SFUEgress, s.EgressMissing, s.EgressMissingPct),
		fmt.Sprintf("  out of SFU received %d of egress %d, missing %d (%.2f%%)", s.GeneratorReceived, s.SFUEgress, s.ReceiveMissing, s.ReceiveMissingPct))
}
