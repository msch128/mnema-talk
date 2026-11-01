package capacity

import (
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestMemberPacketsIsTheShapeInTheBenchmark(t *testing.T) {
	// 3 speakers at 50 packets/s and a 1.5 Mbit/s screen at 6.5 kbit a packet.
	if got := MemberPackets(); math.Abs(got-381) > 1 {
		t.Fatalf("MemberPackets = %v, want about 381", got)
	}
}

func TestEstimateWithoutTrafficUsesTheTable(t *testing.T) {
	r := Estimate(Input{Cores: 4})
	if r.Basis != BasisTable || r.Scale != 1 || r.Status != StatusOK {
		t.Fatalf("%+v", r)
	}
	// 381 packets x 22 us + 0.57 ms = about 0.009 cores per member; 2 cores planned.
	if math.Abs(r.PerMemberCores-0.00895) > 0.0002 || r.PlanCores != 2 {
		t.Fatalf("%+v", r)
	}
	if r.MoreMembers < 200 || r.MoreMembers > 230 {
		t.Fatalf("MoreMembers = %d", r.MoreMembers)
	}
}

func TestEstimateScalesWithTheHostsObservedCost(t *testing.T) {
	// Model: 38,000 x 22 us + 400 x 72 us + 100 x 0.57 ms = 0.92 cores.
	base := Input{Cores: 4, EgressPPS: 38000, IngressPPS: 400, Peers: 100, UsedCores: 0.92 + IdleCores}
	fast := Estimate(base)
	slow := base
	slow.UsedCores = 1.84 + IdleCores // twice as expensive as the model says
	s := Estimate(slow)
	if fast.Basis != BasisObserved || math.Abs(fast.Scale-1) > 0.02 {
		t.Fatalf("fast: %+v", fast)
	}
	if math.Abs(s.Scale-2) > 0.05 {
		t.Fatalf("slow scale: %+v", s)
	}
	if s.MoreMembers >= fast.MoreMembers || s.PerMemberCores < 1.9*fast.PerMemberCores {
		t.Fatalf("a slower host must fit fewer members: fast %+v slow %+v", fast, s)
	}
}

func TestEstimateIgnoresTooLittleTrafficAndClampsNoise(t *testing.T) {
	few := Estimate(Input{Cores: 2, EgressPPS: 500, IngressPPS: 50, Peers: 3, UsedCores: 0.9})
	if few.Basis != BasisTable {
		t.Fatalf("500 packets/s is too little to trust: %+v", few)
	}
	wild := Estimate(Input{Cores: 8, EgressPPS: 6000, Peers: 5, UsedCores: 7})
	if wild.Scale != 4 {
		t.Fatalf("scale = %v, want clamped to 4", wild.Scale)
	}
}

func TestEstimateStatus(t *testing.T) {
	if r := Estimate(Input{Cores: 2, UsedCores: 1.2}); r.Status != StatusBusy || r.MoreMembers != 0 {
		t.Fatalf("past the plan: %+v", r)
	}
	if r := Estimate(Input{Cores: 4, RSSBytes: 900 << 20, MemoryLimit: 1 << 30}); r.Status != StatusMemory || r.MoreMembers != 0 {
		t.Fatalf("memory tight: %+v", r)
	}
	if r := Estimate(Input{Cores: 4, RSSBytes: 900 << 20}); r.Status != StatusOK {
		t.Fatalf("unknown limit must not block: %+v", r)
	}
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCores(t *testing.T) {
	host := float64(runtime.NumCPU())
	dir := t.TempDir()
	if got := Cores(dir); got != host {
		t.Errorf("no cpu.max: %v", got)
	}
	write(t, dir, "cpu.max", "max 100000\n")
	if got := Cores(dir); got != host {
		t.Errorf("unlimited: %v", got)
	}
	write(t, dir, "cpu.max", "50000 100000\n")
	if got := Cores(dir); got != 0.5 {
		t.Errorf("half a core: %v", got)
	}
	write(t, dir, "cpu.max", "100000000 100000\n")
	if got := Cores(dir); got != host {
		t.Errorf("quota above the host: %v", got)
	}
	for _, bad := range []string{"x y", "1", "0 100000", "100000 0"} {
		write(t, dir, "cpu.max", bad)
		if got := Cores(dir); got != host {
			t.Errorf("%q: %v", bad, got)
		}
	}
}

func TestMemoryLimit(t *testing.T) {
	dir := t.TempDir()
	if got := MemoryLimit(dir); got != 0 {
		t.Errorf("none: %d", got)
	}
	write(t, dir, "memory.max", "max\n")
	if got := MemoryLimit(dir); got != 0 {
		t.Errorf("max: %d", got)
	}
	write(t, dir, "memory.max", "1073741824\n")
	if got := MemoryLimit(dir); got != 1<<30 {
		t.Errorf("v2: %d", got)
	}
	v1 := t.TempDir()
	write(t, v1, "memory/memory.limit_in_bytes", "536870912")
	if got := MemoryLimit(v1); got != 512<<20 {
		t.Errorf("v1: %d", got)
	}
	write(t, v1, "memory/memory.limit_in_bytes", "9223372036854771712")
	if got := MemoryLimit(v1); got != 0 {
		t.Errorf("v1 unlimited: %d", got)
	}
}

// A small call on a quiet server: the CPU is mostly the rest of the server
// (about 0.1 core), not media, and must not make a member look 3x as costly.
func TestEstimateDoesNotBlameMediaForTheIdleBaseline(t *testing.T) {
	quiet := Input{Cores: 2, EgressPPS: 1200, IngressPPS: 150, Peers: 3, UsedCores: 0.12}
	r := Estimate(quiet)
	table := Estimate(Input{Cores: 2})
	if r.Basis != BasisTable || r.Scale != 1 {
		t.Fatalf("1200 packets/s is not enough traffic to measure a host: %+v", r)
	}
	if r.PerMemberCores != table.PerMemberCores {
		t.Fatalf("per member %v, want the table's %v", r.PerMemberCores, table.PerMemberCores)
	}
	// Enough traffic, and a host that costs what the model says on top of the baseline.
	m := Input{Cores: 4, EgressPPS: 6000, IngressPPS: 400, Peers: 20}
	m.UsedCores = modelled(m) + IdleCores
	if r := Estimate(m); r.Basis != BasisObserved || math.Abs(r.Scale-1) > 0.01 {
		t.Fatalf("baseline must be subtracted: %+v", r)
	}
	// Used CPU below the baseline cannot be a measurement of media.
	m.UsedCores = IdleCores / 2
	if r := Estimate(m); r.Basis != BasisTable {
		t.Fatalf("%+v", r)
	}
}

func TestCoresFromCgroupV1(t *testing.T) {
	host := float64(runtime.NumCPU())
	dir := t.TempDir()
	write(t, dir, "cpu/cpu.cfs_quota_us", "50000\n")
	write(t, dir, "cpu/cpu.cfs_period_us", "100000\n")
	if got := Cores(dir); got != 0.5 {
		t.Errorf("half a core: %v", got)
	}
	write(t, dir, "cpu/cpu.cfs_quota_us", "-1\n")
	if got := Cores(dir); got != host {
		t.Errorf("unlimited (-1): %v", got)
	}
	write(t, dir, "cpu/cpu.cfs_quota_us", "abc")
	if got := Cores(dir); got != host {
		t.Errorf("garbage: %v", got)
	}
}
