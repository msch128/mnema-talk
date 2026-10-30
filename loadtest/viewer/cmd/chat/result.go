package main

import (
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/msch128/mnema-talk/loadtest/viewer/internal/lt"
)

// kindResult is what became of one kind of event.
type kindResult struct {
	Sent         int64      `json:"sent"`
	Expected     int64      `json:"expected_deliveries"`
	Delivered    int64      `json:"delivered"`
	Lost         int64      `json:"lost"`
	Duplicates   int64      `json:"duplicates"`
	LossPct      float64    `json:"loss_pct"`
	SendAttempts int64      `json:"send_attempts"`
	SendErrors   int64      `json:"send_errors"`
	RateLimited  int64      `json:"rate_limited"`
	Latency      lt.Summary `json:"latency"`
}

type sessionResult struct {
	Requested      int        `json:"requested"`
	ConnectedPeak  int64      `json:"connected_peak"`
	NeverConnected int        `json:"never_connected"`
	WSErrors       int64      `json:"ws_errors"`
	Reconnects     int64      `json:"reconnects"`
	ConnectTime    lt.Summary `json:"connect_time"`
}

// result is the JSON report of a run.
type result struct {
	Tool       string        `json:"tool"`
	Started    time.Time     `json:"started"`
	DurationS  float64       `json:"duration_s"`
	Sessions   sessionResult `json:"sessions"`
	Writers    int           `json:"writers"`
	RatePerSec float64       `json:"rate_per_writer"`
	Messages   kindResult    `json:"messages"`
	Edits      kindResult    `json:"edits"`
	Reactions  kindResult    `json:"reactions"`
	Typing     struct {
		Sent     int64 `json:"sent"`
		Received int64 `json:"received"`
	} `json:"typing"`
	BadFrames int64               `json:"bad_frames"`
	Failures  map[string]*failure `json:"request_failures,omitempty"`
	Server    lt.ServerSummary    `json:"server"`
	// GeneratorCores is the CPU this tool used itself, in cores on average.
	GeneratorCores float64    `json:"generator_cpu_cores"`
	Thresholds     thresholds `json:"thresholds"`
	Violations     []string   `json:"violations"`
	OK             bool       `json:"ok"`
}

func (r *run) failureSnapshot() map[string]*failure {
	r.failMu.Lock()
	defer r.failMu.Unlock()
	out := map[string]*failure{}
	for k, f := range r.failures {
		c := *f
		out[k] = &c
	}
	return out
}

func (r *run) kindResult(k kind) kindResult {
	tot := r.track.totals(k)
	st := &r.req[k]
	return kindResult{
		Sent: tot.Sent, Expected: tot.Expected, Delivered: tot.Got, Lost: tot.Lost,
		Duplicates: tot.Duplicates, LossPct: tot.lossPct(),
		SendAttempts: st.attempts.Load(), SendErrors: st.errors.Load(), RateLimited: st.rateLimited.Load(),
		Latency: r.track.lat[k].Summarize(),
	}
}

func (r *run) result(started time.Time, server lt.ServerSummary) *result {
	res := &result{
		Tool: "chat", Started: started, DurationS: time.Since(started).Seconds(),
		Writers: r.cfg.writers, RatePerSec: r.cfg.rate,
		Sessions: sessionResult{
			Requested: r.cfg.sessions, ConnectedPeak: r.connectedPeak.Load(),
			NeverConnected: r.cfg.sessions - int(r.sessionsEver.Load()),
			WSErrors:       r.wsErrors.Load(), Reconnects: r.reconnects.Load(),
			ConnectTime: r.connectMs.Summarize(),
		},
		Messages: r.kindResult(kindMessage), Edits: r.kindResult(kindEdit), Reactions: r.kindResult(kindReaction),
		BadFrames: r.framesBad.Load(), Failures: r.failureSnapshot(), Server: server, Thresholds: r.cfg.th,
	}
	res.Typing.Sent, res.Typing.Received = r.typingSent.Load(), r.typingReceived.Load()
	checks := evaluate(res, r.cfg.th)
	res.Violations, res.OK = checks.Violations, checks.OK()
	if res.Violations == nil {
		res.Violations = []string{}
	}
	return res
}

// printSummary writes the human-readable report.
func printSummary(w io.Writer, res *result) {
	s := res.Sessions
	fmt.Fprintf(w, "\n=== chat load test ===\n")
	fmt.Fprintf(w, "sessions   %d requested, peak %d connected, %d never connected, %d ws errors, %d reconnects, connect p50/p95 %.0f/%.0f ms\n",
		s.Requested, s.ConnectedPeak, s.NeverConnected, s.WSErrors, s.Reconnects, s.ConnectTime.P50Ms, s.ConnectTime.P95Ms)
	fmt.Fprintf(w, "writers    %d at %.2f msg/s each, %d typing indicators sent, %d received\n",
		res.Writers, res.RatePerSec, res.Typing.Sent, res.Typing.Received)
	printKind(w, "messages", res.Messages)
	printKind(w, "edits", res.Edits)
	printKind(w, "reactions", res.Reactions)
	printFailures(w, res.Failures)
	printServer(w, res.Server)
	fmt.Fprintf(w, "generator  this tool used %.2f cores on average (a generator near the host's core count measures itself)\n", res.GeneratorCores)
	if res.OK {
		fmt.Fprintln(w, "\nOK: all thresholds met")
		return
	}
	fmt.Fprintf(w, "\nFAILED (%d):\n", len(res.Violations))
	for _, v := range res.Violations {
		fmt.Fprintf(w, "  - %s\n", v)
	}
}

func printKind(w io.Writer, name string, k kindResult) {
	if k.SendAttempts == 0 && k.Expected == 0 {
		return
	}
	fmt.Fprintf(w, "%-10s %d sent (%d failed, %d rate limited), %d of %d deliveries (%.3f%% lost, %d duplicates), delivery p50/p95/p99/max %.1f/%.1f/%.1f/%.1f ms\n",
		name, k.Sent, k.SendErrors, k.RateLimited, k.Delivered, k.Expected, k.LossPct, k.Duplicates,
		k.Latency.P50Ms, k.Latency.P95Ms, k.Latency.P99Ms, k.Latency.MaxMs)
}

func printServer(w io.Writer, s lt.ServerSummary) {
	if !s.Measured {
		fmt.Fprintln(w, "server     not measured (no METRICS_TOKEN)")
		return
	}
	fmt.Fprintf(w, "server     CPU %.2f cores in hold (%.2f over the whole load, peak %.2f), memory peak %.0f MB resident / %.0f MB heap, goroutines %.0f -> peak %.0f -> %.0f\n",
		s.CPUCoresHold, s.CPUCoresAvg, s.CPUCoresPeak, s.RSSPeakMB, s.HeapPeakMB, s.GoroutinesPre, s.GoroutinesPeak, s.GoroutinesPost)
}

func printFailures(w io.Writer, failures map[string]*failure) {
	keys := make([]string, 0, len(failures))
	for k := range failures {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(w, "failed     %s x%d, first answer: %s\n", k, failures[k].Count, failures[k].Sample)
	}
}
