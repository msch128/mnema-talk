package server

import (
	"math"
	"testing"

	"github.com/msch128/mnema-talk/internal/config"
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
	if st.HeapBytes == 0 || st.CPUUsedCores != 0 || st.EgressPPS != 0 || !st.Collecting {
		t.Fatalf("%+v", st)
	}
}

func TestCapacityEstimateWorksWithoutTrafficAndSaysWhenItIsBusy(t *testing.T) {
	idle := capacityEstimate(CapacityStatus{CPUCores: 4}, 0)
	if idle.Status != "ok" || idle.Basis != "table" || idle.MoreMembers < 100 || idle.PlanCores != 2 || idle.PerMemberMillicores <= 0 {
		t.Fatalf("idle: %+v", idle)
	}
	busy := capacityEstimate(CapacityStatus{CPUCores: 2, CPUUsedCores: 1.5}, 0)
	if busy.Status != "busy" || busy.MoreMembers != 0 {
		t.Fatalf("busy: %+v", busy)
	}
	tight := capacityEstimate(CapacityStatus{CPUCores: 4, RSSBytes: 950 << 20, MemoryLimitBytes: 1 << 30}, 0)
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

func TestCapacityEstimateListsTheScenariosWithTheirLimits(t *testing.T) {
	e := capacityEstimate(CapacityStatus{CPUCores: 4, RSSBytes: 150 << 20, MemoryLimitBytes: 1 << 30}, 0)
	ids := []string{"chat", "broadcast", "conversation", "screen", "video"}
	if len(e.Scenarios) != len(ids) || e.UploadLimitMbps != 0 {
		t.Fatalf("%+v", e)
	}
	for i, s := range e.Scenarios {
		if s.ID != ids[i] || s.Members <= 0 || s.Limit == "" || s.CPUCores <= 0 || s.MemoryBytes == 0 {
			t.Fatalf("row %d: %+v", i, s)
		}
	}
	if s := e.Scenarios[3]; s.Basis != "measured" || s.MeasuredUpTo != 100 || !s.Extrapolated || s.UploadMbps < 100 {
		t.Fatalf("screen: %+v", s)
	}
	if busy := capacityEstimate(CapacityStatus{CPUCores: 2, CPUUsedCores: 1.5}, 0); busy.Scenarios == nil || len(busy.Scenarios) != 0 {
		t.Fatalf("busy lists no scenarios but an empty list: %+v", busy.Scenarios)
	}
}

func TestCapacitySnapshotLimitsByTheConfiguredUpload(t *testing.T) {
	h := &systemHandler{cfg: &config.Config{VoiceUploadLimitMbit: 40}}
	e := h.capacitySnapshot().Estimate
	if e.UploadLimitMbps != 40 || len(e.Scenarios) == 0 {
		t.Fatalf("%+v", e)
	}
	for _, s := range e.Scenarios[1:] {
		if s.Limit != "upload" || s.UploadMbps > 40 {
			t.Fatalf("%s should stop at the 40 Mbit/s upload: %+v", s.ID, s)
		}
	}
}
