package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/metrics"
)

func TestMetricsExposeProcessBuildAndInstruments(t *testing.T) {
	metrics.ObserveHTTP("/api/test-metrics", http.StatusInternalServerError, 20*time.Millisecond)
	rec := httptest.NewRecorder()
	metricsHandler(&opsSource{version: "9.9.9", hasStore: true, storage: func() error { return nil }}).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/metrics", nil))
	body := rec.Body.String()
	for _, want := range []string{
		"mnema_up 1\n",
		`mnema_build_info{version="9.9.9"`,
		"process_start_time_seconds ",
		"process_cpu_seconds_total ",
		"go_gc_cycles_total ",
		"go_gc_pause_seconds_total ",
		"go_memstats_heap_inuse_bytes ",
		"mnema_ws_online_users 0\n",
		`mnema_sfu_tracks{kind="video"} 0`,
		"mnema_storage_up 1\n",
		`mnema_http_requests_total{route="/api/test-metrics",class="5xx"}`,
		`mnema_http_request_duration_seconds_count{route="/api/test-metrics"}`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in\n%s", want, body)
		}
	}
	if strings.Contains(body, "mnema_db_connections") {
		t.Error("pool metrics without a pool")
	}

	rec = httptest.NewRecorder()
	metricsHandler(&opsSource{hasStore: true, storage: func() error { return http.ErrServerClosed }}).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/metrics", nil))
	if !strings.Contains(rec.Body.String(), "mnema_storage_up 0\n") || !strings.Contains(rec.Body.String(), `version="dev"`) {
		t.Fatal("unreachable storage or default version not reported")
	}
}

func TestOpsSamplerDifferencesCountersIntoRates(t *testing.T) {
	// Earlier tests of the package may have used up the series of the family
	// (maxSeries); a new route would then count under "other", not "5xx".
	metrics.HTTPRequests.Reset()
	o := newOpsSampler(nil)
	o.sample() // baseline only
	if len(o.ring.Snapshot()) != 0 {
		t.Fatal("baseline sample was stored")
	}
	for range 5 {
		metrics.ObserveHTTP("/api/test-sampler", http.StatusBadGateway, time.Millisecond)
	}
	metrics.SFUICE.With("failed").Inc()
	metrics.SFUEgressBytes.Add(1 << 20)
	time.Sleep(20 * time.Millisecond)
	o.sample()
	got := o.ring.Snapshot()
	if len(got) != 1 {
		t.Fatalf("samples = %d, want 1", len(got))
	}
	s := got[0]
	if s.Errors5xx < 5 || s.ICEFailures < 1 || s.Requests < 5 || s.EgressBps <= 0 || s.Goroutines == 0 || s.T == 0 {
		t.Fatalf("sample = %+v", s)
	}
	if s.CPUMillicores < 0 {
		t.Fatalf("negative cpu %v", s.CPUMillicores)
	}
}

func TestOpsSamplerRunsUntilCanceled(t *testing.T) {
	o := newOpsSampler(nil)
	o.every = 5 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { o.run(ctx); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for len(o.ring.Snapshot()) < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("sampler did not stop")
	}
	if len(o.ring.Snapshot()) < 2 {
		t.Fatal("sampler produced no samples")
	}
}

func TestOpsRingStaysBounded(t *testing.T) {
	o := newOpsSampler(nil)
	for i := range opsHistory + 25 {
		o.ring.Push(OpsSample{T: int64(i)})
	}
	got := o.ring.Snapshot()
	if len(got) != opsHistory || got[0].T != 25 || got[len(got)-1].T != opsHistory+24 {
		t.Fatalf("ring len %d first %d", len(got), got[0].T)
	}
}

func TestOpsStatusWithoutSamplerIsEmptyHistory(t *testing.T) {
	h := &systemHandler{}
	rec := httptest.NewRecorder()
	if err := h.opsStatus(rec, httptest.NewRequest(http.MethodGet, "/api/admin/system/ops", nil)); err != nil {
		t.Fatal(err)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("ops answer is cacheable")
	}
	var st OpsStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.IntervalSeconds != 10 || st.History == nil || len(st.History) != 0 || st.Process.Goroutines == 0 {
		t.Fatalf("status = %+v", st)
	}
}
