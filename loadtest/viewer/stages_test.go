package main

import (
	"strings"
	"testing"
)

func stageFixture() (stageInput, viewerConfig) {
	ids := []string{"s", "c1", "c2", "v1", "v2"}
	c := viewerConfig{from: 0, to: 5, roles: roleCounts{1, 2, 0}, cameras: allCameras}
	for _, id := range ids {
		c.tokens = append(c.tokens, token{ID: id})
	}
	in := stageInput{
		window:   100e9,
		sent:     map[string]int64{"s": 1000, "c1": 500, "c2": 500, "v1": 0, "v2": 0},
		recv:     map[string]int64{"s": 1000, "c1": 2000, "c2": 2000, "v1": 3000, "v2": 3000},
		audience: map[string]bool{"s": true, "c1": true, "c2": true, "v1": true, "v2": true},
		roles:    map[string]string{"s": "screen", "c1": "camera", "c2": "camera", "v1": "viewer", "v2": "viewer"},
		server: map[string]float64{
			"mnema_sfu_ingress_packets_total": 2000, "mnema_sfu_egress_packets_total": 8000,
		},
		hasServer: true,
	}
	return in, c
}

func TestStagesBalanceWhenNothingIsLost(t *testing.T) {
	in, c := stageFixture()
	// screen: 1000 x 4 others = 4000; each camera: 500 x 4 others = 2000 each;
	// The receivers read the 8000 packets that were written.
	in.recv = map[string]int64{"s": 1000, "c1": 1000, "c2": 1000, "v1": 2500, "v2": 2500}
	st := splitStages(in, c)
	if st == nil || !st.ServerCountersUsed || st.GeneratorSent != 2000 || st.ExpectedEgress != 8000 || st.Audience != 5 {
		t.Fatalf("stages = %+v", st)
	}
	if st.IngressMissing != 0 || st.EgressMissing != 0 || st.ReceiveMissing != 0 {
		t.Fatalf("stages = %+v", st)
	}
}

func TestStagesNameTheHopThatLosesPackets(t *testing.T) {
	in, c := stageFixture()
	in.server["mnema_sfu_ingress_packets_total"] = 1800 // 10% lost on the way in
	in.server["mnema_sfu_egress_packets_total"] = 6480  // 10% short of the expected 7200
	in.recv = map[string]int64{"s": 500, "c1": 500, "c2": 500, "v1": 2000, "v2": 2000}
	st := splitStages(in, c)
	if st.IngressMissing != 200 || st.IngressMissingPct != 10 {
		t.Errorf("ingress: %+v", st)
	}
	if st.ExpectedEgress != 7200 || st.EgressMissing != 720 {
		t.Errorf("egress: %+v", st)
	}
	if st.GeneratorReceived != 5500 || st.ReceiveMissing != 980 {
		t.Errorf("receive: %+v", st)
	}
	if !strings.Contains(strings.Join(st.lines(), "\n"), "fan-out") {
		t.Error("no fan-out line")
	}
}

func TestMembersThatLeftAreNotAudienceNorCounted(t *testing.T) {
	in, c := stageFixture()
	in.audience["v2"] = false // flapped during the window
	in.recv["v2"] = 999
	st := splitStages(in, c)
	// Three others instead of four watch the screen: 1000 x 3, cameras 500 x 3 each.
	if st.Audience != 4 || st.ExpectedEgress != int64(float64(3000+1500+1500)) {
		t.Fatalf("stages = %+v", st)
	}
	if st.GeneratorReceived != 1000+2000+2000+3000 {
		t.Fatalf("received = %d", st.GeneratorReceived)
	}
	in.audience["s"] = false // the sharer dropped out: its packets are not counted
	if st := splitStages(in, c); st.GeneratorSent != 1000 {
		t.Fatalf("sent = %d", st.GeneratorSent)
	}
}

func TestStagesWithoutServerCountersCompareOnlyWhatArrived(t *testing.T) {
	in, c := stageFixture()
	in.hasServer = false
	st := splitStages(in, c)
	if st.ServerCountersUsed || st.ExpectedEgress != 8000 || st.ReceiveMissing != 0 {
		t.Fatalf("stages = %+v", st)
	}
	in.sent = map[string]int64{}
	if splitStages(in, c) != nil {
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
