package lt

import (
	"strings"
	"testing"
	"time"
)

const sampleText = `# HELP process_cpu_seconds_total x
# TYPE process_cpu_seconds_total counter
process_cpu_seconds_total 12.5

process_resident_memory_bytes 104857600
go_goroutines 321
go_memstats_alloc_bytes 52428800
unrelated_metric 99
mnema_ratelimit_hits_total{limiter="ws_ip"} 3
mnema_ratelimit_hits_total{limiter="api_user"} 4
mnema_http_request_duration_seconds_bucket{route="/x",le="0.1"} 7
mnema_sfu_egress_bytes_total 1000
`

func TestParseMetricsKeepsOurSeriesAndSumsLabels(t *testing.T) {
	got := ParseMetrics(strings.NewReader(sampleText))
	for name, want := range map[string]float64{
		"process_cpu_seconds_total":     12.5,
		"process_resident_memory_bytes": 104857600,
		"go_goroutines":                 321,
		"mnema_ratelimit_hits_total":    7,
		"mnema_sfu_egress_bytes_total":  1000,
		"go_memstats_alloc_bytes":       52428800,
	} {
		if got[name] != want {
			t.Errorf("%s = %v, want %v", name, got[name], want)
		}
	}
	if _, ok := got["unrelated_metric"]; ok {
		t.Error("an unrelated metric was kept")
	}
	if _, ok := got["mnema_http_request_duration_seconds_bucket"]; ok {
		t.Error("a histogram bucket was kept")
	}
}

func smp(at time.Time, phase string, cpu, rss, gor float64) Sample {
	return Sample{At: at, Phase: phase, Values: map[string]float64{
		"process_cpu_seconds_total": cpu, "process_resident_memory_bytes": rss * mb,
		"go_goroutines": gor, "mnema_sfu_egress_bytes_total": cpu * 1000,
	}}
}

func TestSummarizeServerReadsCPUAsCoresOverTheLoadedPhases(t *testing.T) {
	t0 := time.Unix(1000, 0)
	samples := []Sample{
		smp(t0, "pre", 10, 100, 50),
		smp(t0.Add(10*time.Second), "ramp", 12, 150, 300),
		smp(t0.Add(20*time.Second), "hold", 14, 200, 400), // 0.2 cores
		smp(t0.Add(30*time.Second), "hold", 24, 250, 410), // 1 core
		smp(t0.Add(40*time.Second), "hold", 34, 260, 405), // 1 core
		smp(t0.Add(50*time.Second), "post", 34.1, 120, 55),
	}
	s := SummarizeServer(samples, 0)
	if !s.Measured || s.Samples != 6 {
		t.Fatalf("summary = %+v", s)
	}
	// ramp..hold: (34-12)/30 s; hold alone: (34-14)/20 s.
	if s.CPUCoresAvg < 0.73 || s.CPUCoresAvg > 0.74 {
		t.Errorf("avg = %v", s.CPUCoresAvg)
	}
	if s.CPUCoresHold < 0.99 || s.CPUCoresHold > 1.01 {
		t.Errorf("hold = %v", s.CPUCoresHold)
	}
	if s.CPUCoresPeak < 1 {
		t.Errorf("peak = %v", s.CPUCoresPeak)
	}
	if s.RSSPeakMB != 260 || s.GoroutinesPre != 50 || s.GoroutinesPost != 55 || s.GoroutinesPeak != 410 {
		t.Errorf("summary = %+v", s)
	}
	if d := s.RunDeltas["mnema_sfu_egress_bytes_total"]; d != 24100 {
		t.Errorf("run delta = %v", d)
	}
	if d := s.Deltas["mnema_sfu_egress_bytes_total"]; d != 22000 {
		t.Errorf("delta = %v", d)
	}
	if _, ok := s.Deltas["process_cpu_seconds_total"]; ok {
		t.Error("CPU seconds listed as a counter delta")
	}
}

func TestSummarizeServerWithoutSamplesIsNotMeasured(t *testing.T) {
	if s := SummarizeServer(nil, 3); s.Measured || s.ScrapeErrors != 3 {
		t.Fatalf("summary = %+v", s)
	}
	one := SummarizeServer([]Sample{smp(time.Now(), "pre", 1, 1, 1)}, 0)
	if !one.Measured || one.CPUCoresAvg != 0 {
		t.Fatalf("summary = %+v", one)
	}
}

func TestChecksCollectViolations(t *testing.T) {
	var c Checks
	c.Max("x", 5, 5)
	c.Max("y", 5, 0) // off
	c.Zero("z", 0, 0)
	if !c.OK() {
		t.Fatalf("violations: %v", c.Violations)
	}
	c.Max("x", 5.1, 5)
	c.Zero("z", 1, 0)
	c.Failf("custom %d", 3)
	if c.OK() || len(c.Violations) != 3 {
		t.Fatalf("violations: %v", c.Violations)
	}
}

func TestOriginDefaultsToBaseAndIsOverridable(t *testing.T) {
	base, _ := ParseBase("http://127.0.0.1:8080")
	t.Setenv("ORIGIN", "")
	if got := Origin(base); got != "http://127.0.0.1:8080" {
		t.Fatalf("origin = %q", got)
	}
	t.Setenv("ORIGIN", "https://talk.example.org")
	if got := Origin(base); got != "https://talk.example.org" {
		t.Fatalf("origin = %q", got)
	}
}
