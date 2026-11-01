package metrics

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestVecCapsSeriesAndFoldsIntoOther(t *testing.T) {
	v := newVec("x_total", "x", "a", "b")
	for i := 0; i < maxSeries+50; i++ {
		v.With(fmt.Sprint(i), "y").Inc()
	}
	v.With("0", "y").Inc() // an existing series still counts after the cap
	if len(v.series) != maxSeries+1 {
		t.Fatalf("series = %d, want %d", len(v.series), maxSeries+1)
	}
	if got := v.With("other", "other").Load(); got != 50 {
		t.Fatalf("other = %d, want 50", got)
	}
	if v.With("0", "y").Load() != 2 {
		t.Fatal("existing series lost")
	}
	if v.Total() != maxSeries+51 {
		t.Fatalf("total = %d", v.Total())
	}
	if v.Sum(1, "y") != maxSeries+1 {
		t.Fatalf("sum = %d", v.Sum(1, "y"))
	}
	if v.Sum(5, "y") != 0 {
		t.Fatal("out of range label index matched")
	}
}

func TestVecWritesSortedExposition(t *testing.T) {
	v := newVec("req_total", "Requests", "route", "class")
	v.With("/b", "2xx").Add(3)
	v.With("/a", "5xx").Inc()
	var b strings.Builder
	v.write(&b)
	want := "# HELP req_total Requests\n# TYPE req_total counter\n" +
		"req_total{route=\"/a\",class=\"5xx\"} 1\nreq_total{route=\"/b\",class=\"2xx\"} 3\n\n"
	if b.String() != want {
		t.Fatalf("got\n%q\nwant\n%q", b.String(), want)
	}
}

func TestHistogramBucketsAreCumulative(t *testing.T) {
	h := newHistVec("lat_seconds", "Latency", "route")
	h.Observe("/x", 3*time.Millisecond)
	h.Observe("/x", 300*time.Millisecond)
	h.Observe("/x", time.Minute)
	h.Observe("/x", 0)
	var b strings.Builder
	h.write(&b)
	out := b.String()
	for _, want := range []string{
		`lat_seconds_bucket{route="/x",le="0.005"} 2`,
		`lat_seconds_bucket{route="/x",le="0.5"} 3`,
		`lat_seconds_bucket{route="/x",le="10"} 3`,
		`lat_seconds_bucket{route="/x",le="+Inf"} 4`,
		`lat_seconds_count{route="/x"} 4`,
		`lat_seconds_sum{route="/x"} 60.303`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}

func TestHistogramCapsSeries(t *testing.T) {
	h := newHistVec("lat_seconds", "Latency", "route")
	for i := 0; i < maxSeries+10; i++ {
		h.Observe(fmt.Sprint(i), time.Millisecond)
	}
	h.Observe("again", time.Millisecond)
	if len(h.series) != maxSeries+1 {
		t.Fatalf("series = %d", len(h.series))
	}
	if h.series[otherLabel].count.Load() != 11 {
		t.Fatalf("other count = %d", h.series[otherLabel].count.Load())
	}
}

func TestObserveHTTPClasses(t *testing.T) {
	before := HTTPRequests.Total()
	for _, st := range []int{101, 200, 302, 404, 503} {
		ObserveHTTP("/test-classes", st, time.Millisecond)
	}
	if HTTPRequests.Total()-before != 5 {
		t.Fatal("requests not counted")
	}
	for _, class := range []string{"1xx", "2xx", "3xx", "4xx", "5xx"} {
		if HTTPRequests.With("/test-classes", class).Load() != 1 {
			t.Errorf("class %s not counted once", class)
		}
	}
}

func TestS3CountsResults(t *testing.T) {
	S3("test-op", nil)
	S3("test-op", fmt.Errorf("boom"))
	if S3Operations.With("test-op", "ok").Load() != 1 || S3Operations.With("test-op", "error").Load() != 1 {
		t.Fatal("results not counted")
	}
}

func TestWriteInstrumentsListsEveryFamily(t *testing.T) {
	HTTPInflight.Add(1)
	defer HTTPInflight.Add(-1)
	var b strings.Builder
	WriteInstruments(&b)
	for _, name := range []string{
		"mnema_http_requests_total", "mnema_http_request_duration_seconds", "mnema_http_requests_in_flight",
		"mnema_ratelimit_hits_total", "mnema_ws_connections_total", "mnema_ws_events_sent_total",
		"mnema_ws_events_received_total", "mnema_ws_dropped_total", "mnema_mls_handshakes_total",
		"mnema_mls_handshake_bytes_total", "mnema_sfu_ingress_bytes_total", "mnema_sfu_egress_packets_total",
		"mnema_sfu_nack_requests_total", "mnema_sfu_nack_packets_total", "mnema_sfu_retransmitted_packets_total", "mnema_sfu_retransmit_dropped_total", "mnema_sfu_keyframe_requests_total",
		"mnema_sfu_layer_switches_total", "mnema_sfu_camera_layer_switches_total", "mnema_sfu_ice_transitions_total", "mnema_sfu_peer_joins_total",
		"mnema_s3_operations_total", "mnema_sfu_bandwidth_estimate_reports_total", "mnema_sfu_bandwidth_layer_moves_total",
	} {
		if !strings.Contains(b.String(), "# TYPE "+name+" ") {
			t.Errorf("missing family %s", name)
		}
	}
}

func TestFloatWritersAndNaN(t *testing.T) {
	var b strings.Builder
	WriteFloatGauge(&b, "g", "help", 1.5)
	WriteFloatCounter(&b, "c_total", "help", 2)
	WriteFloatGauge(&b, "nan", "help", nanValue())
	for _, want := range []string{"g 1.5\n", "c_total 2\n", "nan 0\n"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("missing %q in\n%s", want, b.String())
		}
	}
}

func nanValue() float64 {
	var z float64
	return z / z
}

func TestRingKeepsNewestOldestFirst(t *testing.T) {
	r := NewRing[int](3)
	if len(r.Snapshot()) != 0 {
		t.Fatal("new ring not empty")
	}
	r.Push(1)
	r.Push(2)
	if got := fmt.Sprint(r.Snapshot()); got != "[1 2]" {
		t.Fatalf("partial = %s", got)
	}
	for i := 3; i <= 5; i++ {
		r.Push(i)
	}
	if got := fmt.Sprint(r.Snapshot()); got != "[3 4 5]" {
		t.Fatalf("full = %s", got)
	}
	if got := len(NewRing[int](0).buf); got != 1 {
		t.Fatalf("capacity %d, want 1", got)
	}
}

func TestParseStatm(t *testing.T) {
	if got := parseStatm("1000 25 10 5 0 90 0\n", 4096); got != 25*4096 {
		t.Fatalf("rss = %d", got)
	}
	for _, bad := range []string{"", "1000", "1000 x"} {
		if parseStatm(bad, 4096) != 0 {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestProcessReportsCPU(t *testing.T) {
	end := time.Now().Add(20 * time.Millisecond)
	for time.Now().Before(end) {
	}
	if Process().CPUSeconds <= 0 {
		t.Fatal("no cpu time reported")
	}
	if ProcessStart().After(time.Now()) {
		t.Fatal("start in the future")
	}
}

func TestBandwidthRange(t *testing.T) {
	seen := map[string]bool{}
	for _, bps := range []int{0, 499_999, 500_000, 999_999, 1_000_000, 2_000_000, 4_000_000, 8_000_000, 16_000_000, 1 << 30} {
		seen[BandwidthRange(bps)] = true
	}
	if len(seen) != 7 {
		t.Fatalf("ranges %v", seen)
	}
}

func TestHTTPFamiliesKeepErrorClassesPastGenericCap(t *testing.T) {
	HTTPRequests.Reset()
	t.Cleanup(HTTPRequests.Reset)
	for i := 0; i < maxSeries+100; i++ {
		ObserveHTTP(fmt.Sprintf("GET /route/%d", i), 200, time.Millisecond)
	}
	ObserveHTTP("GET /late-route", 500, time.Millisecond)
	if got := HTTPRequests.Sum(1, "5xx"); got != 1 {
		t.Fatalf("5xx total = %d, want 1", got)
	}
}
