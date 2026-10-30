package main

import (
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/loadtest/viewer/internal/lt"
)

func healthy() *result {
	k := kindResult{Sent: 100, Expected: 10_000, Delivered: 10_000, SendAttempts: 100, Latency: lt.Summary{Count: 10_000, P95Ms: 20, P99Ms: 40}}
	return &result{
		Writers:  10,
		Sessions: sessionResult{Requested: 100, ConnectedPeak: 100},
		Messages: k, Edits: k, Reactions: k,
		Server: lt.ServerSummary{Measured: true, CPUCoresHold: 0.5, RSSPeakMB: 300, GoroutinesPre: 100, GoroutinesPost: 105},
	}
}

func defaults() thresholds {
	t := loadThresholds()
	return t
}

func has(c lt.Checks, part string) bool {
	for _, v := range c.Violations {
		if strings.Contains(v, part) {
			return true
		}
	}
	return false
}

func TestAHealthyRunPasses(t *testing.T) {
	if c := evaluate(healthy(), defaults()); !c.OK() {
		t.Fatalf("violations: %v", c.Violations)
	}
}

func TestLossAboveTheLimitFails(t *testing.T) {
	r := healthy()
	r.Messages.LossPct = 0.5
	c := evaluate(r, defaults())
	if !has(c, "message loss") {
		t.Fatalf("violations: %v", c.Violations)
	}
	r.Messages.LossPct = 0.1 // exactly the limit is still fine
	if c := evaluate(r, defaults()); !c.OK() {
		t.Fatalf("violations at the limit: %v", c.Violations)
	}
}

func TestSlowTailFailsOnP95AndP99Separately(t *testing.T) {
	r := healthy()
	r.Messages.Latency.P95Ms = 800
	r.Reactions.Latency.P99Ms = 4000
	c := evaluate(r, defaults())
	if !has(c, "message delivery p95") || !has(c, "reaction delivery p99") || has(c, "message delivery p99") {
		t.Fatalf("violations: %v", c.Violations)
	}
}

func TestSessionProblemsFailWithZeroTolerance(t *testing.T) {
	r := healthy()
	r.Sessions.NeverConnected = 1
	r.Sessions.WSErrors = 2
	r.Sessions.Reconnects = 1
	c := evaluate(r, defaults())
	for _, want := range []string{"never connected", "WebSocket errors", "reconnects"} {
		if !has(c, want) {
			t.Errorf("no violation for %q: %v", want, c.Violations)
		}
	}
	th := defaults()
	th.NeverConnected, th.WSErrors, th.Reconnects = 1, 2, 1
	if c := evaluate(r, th); !c.OK() {
		t.Fatalf("allowed counts still fail: %v", c.Violations)
	}
}

func TestFailedSendsAreJudgedAsAShareOfAttempts(t *testing.T) {
	r := healthy()
	r.Messages.SendErrors = 5 // 5 % of 100
	if c := evaluate(r, defaults()); !has(c, "failed sends") {
		t.Fatalf("violations: %v", c.Violations)
	}
	r.Messages.SendErrors = 1 // 1 %: the limit
	if c := evaluate(r, defaults()); !c.OK() {
		t.Fatalf("violations: %v", c.Violations)
	}
}

func TestAnEmptyRunIsNotAPass(t *testing.T) {
	r := healthy()
	r.Messages = kindResult{}
	r.Edits, r.Reactions = kindResult{}, kindResult{}
	if c := evaluate(r, defaults()); c.OK() {
		t.Fatal("a run that measured nothing passed")
	}
}

func TestServerLimitsApplyOnlyWhenSetAndMeasured(t *testing.T) {
	r := healthy()
	r.Server.CPUCoresHold = 3.5
	if c := evaluate(r, defaults()); !c.OK() {
		t.Fatalf("CPU is unchecked by default: %v", c.Violations)
	}
	th := defaults()
	th.CPUCores, th.RSSMB = 2, 200
	c := evaluate(r, th)
	if !has(c, "server CPU") || !has(c, "server memory") {
		t.Fatalf("violations: %v", c.Violations)
	}
	r.Server = lt.ServerSummary{} // no METRICS_TOKEN: nothing to judge
	if c := evaluate(r, th); !c.OK() {
		t.Fatalf("unmeasured server failed: %v", c.Violations)
	}
}

func TestGoroutineLeakAfterEveryoneLeft(t *testing.T) {
	r := healthy()
	r.Server.GoroutinesPost = r.Server.GoroutinesPre + 500
	if c := evaluate(r, defaults()); !has(c, "goroutines") {
		t.Fatalf("violations: %v", c.Violations)
	}
}
