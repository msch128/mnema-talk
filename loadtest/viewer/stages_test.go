package main

import (
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/loadtest/viewer/internal/lt"
)

func stageFixture() (*result, viewerConfig) {
	ids := []string{"s", "c1", "c2", "v1", "v2"}
	c := viewerConfig{from: 0, to: 5, roles: roleCounts{1, 2, 0}, cameras: allCameras}
	for _, id := range ids {
		c.tokens = append(c.tokens, token{ID: id})
	}
	res := &result{
		PerMember: []memberResult{
			{ID: "s", Role: "screen", SentPackets: 1000},
			{ID: "c1", Role: "camera", SentPackets: 500},
			{ID: "c2", Role: "camera", SentPackets: 500},
			{ID: "v1", Role: "viewer"}, {ID: "v2", Role: "viewer"},
		},
		Media: map[mediaClass]classResult{
			classScreen: {Packets: 4000}, classCamera: {Packets: 4000},
		},
		Server: lt.ServerSummary{Measured: true, RunDeltas: map[string]float64{
			"mnema_sfu_ingress_packets_total": 2000, "mnema_sfu_egress_packets_total": 8000,
		}},
	}
	return res, c
}

func TestStagesBalanceWhenNothingIsLost(t *testing.T) {
	res, c := stageFixture()
	// screen: 1000 x 4 others = 4000; each camera: 500 x 4 others = 2000 each.
	st := splitStages(res, c)
	if st == nil || !st.ServerCountersUsed || st.GeneratorSent != 2000 || st.ExpectedEgress != 8000 {
		t.Fatalf("stages = %+v", st)
	}
	if st.IngressMissing != 0 || st.EgressMissing != 0 || st.ReceiveMissing != 0 {
		t.Fatalf("stages = %+v", st)
	}
}

func TestStagesNameTheHopThatLosesPackets(t *testing.T) {
	res, c := stageFixture()
	res.Server.RunDeltas["mnema_sfu_ingress_packets_total"] = 1800 // 10% lost on the way in
	res.Server.RunDeltas["mnema_sfu_egress_packets_total"] = 6480  // all of the expected 7200 but 10%
	res.Media[classScreen] = classResult{Packets: 3000}
	res.Media[classCamera] = classResult{Packets: 3000}
	st := splitStages(res, c)
	if st.IngressMissing != 200 || st.IngressMissingPct != 10 {
		t.Errorf("ingress: %+v", st)
	}
	if st.ExpectedEgress != 7200 || st.EgressMissing != 720 {
		t.Errorf("egress: %+v", st)
	}
	if st.ReceiveMissing != 480 {
		t.Errorf("receive: %+v", st)
	}
	if !strings.Contains(strings.Join(st.lines(), "\n"), "fan-out") {
		t.Error("no fan-out line")
	}
}

func TestStagesWithoutServerCountersCompareOnlyWhatArrived(t *testing.T) {
	res, c := stageFixture()
	res.Server = lt.ServerSummary{}
	st := splitStages(res, c)
	if st.ServerCountersUsed || st.ExpectedEgress != 8000 || st.ReceiveMissing != 0 {
		t.Fatalf("stages = %+v", st)
	}
	res.PerMember = nil
	if splitStages(res, c) != nil {
		t.Fatal("a run without publishers has no split")
	}
}

func TestSurplusIsNotNegativeLoss(t *testing.T) {
	if n, p := missing(100, 150); n != 0 || p != 0 {
		t.Fatalf("%d %v", n, p)
	}
	if n, p := missing(0, 5); n != 0 || p != 0 {
		t.Fatalf("%d %v", n, p)
	}
}
