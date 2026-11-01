package server

import (
	"math"
	"testing"
)

func TestAverageSamplesTakesRatesOverTheWindowAndStateFromTheNewest(t *testing.T) {
	st := averageSamples([]OpsSample{
		{CPUMillicores: 400, EgressBps: 10e6, EgressPPS: 1000, IngressPPS: 100, RSSBytes: 1, HeapBytes: 1, Peers: 1},
		{CPUMillicores: 800, EgressBps: 30e6, EgressPPS: 3000, IngressPPS: 300, RSSBytes: 200 << 20, HeapBytes: 90 << 20, Peers: 12, ScreenShares: 1},
	})
	if math.Abs(st.CPUUsedCores-0.6) > 1e-9 || math.Abs(st.EgressMbps-20) > 1e-9 || st.EgressPPS != 2000 || st.IngressPPS != 200 {
		t.Fatalf("rates: %+v", st)
	}
	if st.RSSBytes != 200<<20 || st.HeapBytes != 90<<20 || st.Peers != 12 || st.ScreenShares != 1 {
		t.Fatalf("newest sample: %+v", st)
	}
}

func TestAverageSamplesWithoutSamplesReadsTheProcess(t *testing.T) {
	st := averageSamples(nil)
	if st.HeapBytes == 0 || st.CPUUsedCores != 0 || st.EgressPPS != 0 {
		t.Fatalf("%+v", st)
	}
}

func TestCapacityEstimateWorksWithoutTrafficAndSaysWhenItIsBusy(t *testing.T) {
	idle := capacityEstimate(CapacityStatus{CPUCores: 4})
	if idle.Status != "ok" || idle.Basis != "table" || idle.MoreMembers < 100 || idle.PlanCores != 2 || idle.PerMemberMillicores <= 0 {
		t.Fatalf("idle: %+v", idle)
	}
	busy := capacityEstimate(CapacityStatus{CPUCores: 2, CPUUsedCores: 1.5})
	if busy.Status != "busy" || busy.MoreMembers != 0 {
		t.Fatalf("busy: %+v", busy)
	}
	tight := capacityEstimate(CapacityStatus{CPUCores: 4, RSSBytes: 950 << 20, MemoryLimitBytes: 1 << 30})
	if tight.Status != "memory" || tight.MoreMembers != 0 {
		t.Fatalf("memory: %+v", tight)
	}
}

func TestCapacitySnapshotWithoutASamplerStillAnswers(t *testing.T) {
	h := &systemHandler{}
	st := h.capacitySnapshot()
	if st.CPUCores <= 0 || st.Estimate.Status == "" {
		t.Fatalf("%+v", st)
	}
}
