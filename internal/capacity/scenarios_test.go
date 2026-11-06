package capacity

import (
	"math"
	"testing"
)

func scenario(t *testing.T, id string) Scenario {
	t.Helper()
	for _, s := range Scenarios() {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("no scenario %q", id)
	return Scenario{}
}

func near(t *testing.T, what string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Fatalf("%s = %.3f, want %.3f (±%.3f)", what, got, want, tol)
	}
}

func TestMeasuredScenariosReproduceTheSizingTable(t *testing.T) {
	// doc/benchmark.md, sizing table: 3 speakers at 100 members 0.40 cores and
	// 15 Mbit/s; with a screen share 0.92 cores and 208 Mbit/s.
	b := scenario(t, ScenarioBroadcast).Cost(100)
	near(t, "broadcast cores", b.Cores, 0.40, 0.02)
	near(t, "broadcast upload", b.UploadMbps, 15, 1)
	s := scenario(t, ScenarioScreen).Cost(100)
	near(t, "screen cores", s.Cores, 0.92, 0.02)
	near(t, "screen upload", s.UploadMbps, 208, 3)
	// Chat runs: 0.67 cores at 1,000 sessions and 0.09 at 300.
	c := scenario(t, ScenarioChat)
	near(t, "chat 1000", c.Cost(1000).Cores, 0.67, 0.01)
	near(t, "chat 300", c.Cost(300).Cores, 0.09, 0.01)
	if c.Cost(1000).UploadMbps != 0 {
		t.Fatal("chat sends no media")
	}
}

func TestDerivedScenariosAreBuiltFromTheMeasuredTerms(t *testing.T) {
	// Everyone talking, 10 people: 500 packets in, 4,500 out.
	got := scenario(t, ScenarioConversation).Cost(10)
	near(t, "conversation cores", got.Cores, 4500*EgressCost+500*IngressCost+10*MemberCost, 1e-9)
	near(t, "conversation upload", got.UploadMbps, 1.3*10*9*0.04, 1e-9)
	// A video call of 30 sees at most 24 cameras each.
	v30, v26 := videoCall(30), videoCall(26)
	perViewer30 := (v30.UploadMbps/1.3 - 30*29*AudioMbps) / 30
	perViewer26 := (v26.UploadMbps/1.3 - 26*25*AudioMbps) / 26
	near(t, "cameras seen at 30", perViewer30/0.5, 24, 1e-9)
	near(t, "cameras seen at 26", perViewer26/0.5, 24, 1e-9)
	near(t, "video of one", videoCall(1).UploadMbps, 0, 1e-9)
}

func TestFitScenarioFindsTheLargestTalkAndNamesTheLimit(t *testing.T) {
	s := scenario(t, ScenarioScreen)
	b := Budget{Cores: 1.9, Scale: 1}
	f := FitScenario(s, b)
	if f.Limit != LimitCPU || f.Members < 200 || f.Members > 215 {
		t.Fatalf("%+v", f)
	}
	if s.Cost(float64(f.Members)).Cores > b.Cores || s.Cost(float64(f.Members+1)).Cores <= b.Cores {
		t.Fatalf("not the largest fit: %+v", f)
	}
	if !f.Extrapolated || f.Load.Cores <= 0 {
		t.Fatalf("beyond 100 measured members: %+v", f)
	}
	// 40 Mbit/s of upload holds only a small screen share audience.
	up := FitScenario(s, Budget{Cores: 1.9, Scale: 1, UploadMbps: 40})
	if up.Limit != LimitUpload || up.Members != 19 || up.Extrapolated {
		t.Fatalf("upload: %+v", up)
	}
	// Little memory left limits before the CPU: 100 MB / (1.3 x 2.32 MB).
	mem := FitScenario(s, Budget{Cores: 1.9, Scale: 1, MemoryBytes: 100e6})
	if mem.Limit != LimitMemory || mem.Members != 33 {
		t.Fatalf("memory: %+v", mem)
	}
}

func TestFitScenarioScalesOnlyMediaByTheHost(t *testing.T) {
	one, two := Budget{Cores: 1, Scale: 1}, Budget{Cores: 1, Scale: 2}
	chat := scenario(t, ScenarioChat)
	if FitScenario(chat, one).Members != FitScenario(chat, two).Members {
		t.Fatal("chat CPU is not packet forwarding")
	}
	screen := scenario(t, ScenarioScreen)
	fast, slow := FitScenario(screen, one), FitScenario(screen, two)
	if slow.Members >= fast.Members || math.Abs(slow.Load.Cores-1) > 0.02 {
		t.Fatalf("fast %+v slow %+v", fast, slow)
	}
}

func TestFitScenarioWithNothingLeft(t *testing.T) {
	f := FitScenario(scenario(t, ScenarioVideo), Budget{Scale: 1})
	if f.Members != 0 || f.Limit != LimitCPU || f.Load != (Load{}) || f.Extrapolated {
		t.Fatalf("%+v", f)
	}
}

func TestEstimateListsTheScenariosOnlyWhenThereIsRoom(t *testing.T) {
	// The test host: 4 cores, idle, 150 MB resident of 1 GB.
	r := Estimate(Input{Cores: 4, UsedCores: 0.1, RSSBytes: 150 << 20, MemoryLimit: 1 << 30})
	want := map[string][2]int{ScenarioChat: {1700, 1850}, ScenarioBroadcast: {400, 500}, ScenarioConversation: {35, 45}, ScenarioScreen: {200, 220}, ScenarioVideo: {15, 25}}
	if len(r.Scenarios) != len(want) {
		t.Fatalf("%d scenarios", len(r.Scenarios))
	}
	for _, s := range r.Scenarios {
		w := want[s.Scenario.ID]
		if s.Fit.Members < w[0] || s.Fit.Members > w[1] {
			t.Fatalf("%s: %d people, want %d to %d", s.Scenario.ID, s.Fit.Members, w[0], w[1])
		}
	}
	if r.Scenarios[1].Fit.Limit != LimitMemory || r.Scenarios[3].Fit.Limit != LimitCPU {
		t.Fatalf("broadcast is memory-bound, screen CPU-bound: %+v", r.Scenarios)
	}
	if busy := Estimate(Input{Cores: 1, UsedCores: 0.9}); len(busy.Scenarios) != 0 {
		t.Fatalf("busy: %+v", busy.Scenarios)
	}
}
