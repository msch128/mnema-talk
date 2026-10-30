package main

import (
	"net/url"
	"testing"
)

func TestWriterCountRoundsToTheNearestAndNeverDropsBelowOne(t *testing.T) {
	for _, c := range []struct {
		sessions int
		pct      float64
		want     int
	}{{100, 10, 10}, {300, 10, 30}, {1000, 5, 50}, {20, 1, 1}, {20, 0, 0}, {10, 100, 10}, {10, 250, 10}} {
		if got := writerCount(c.sessions, c.pct); got != c.want {
			t.Errorf("writerCount(%d, %v) = %d, want %d", c.sessions, c.pct, got, c.want)
		}
	}
}

func TestConfigRefusesWhatTheServerWouldRateLimit(t *testing.T) {
	ok := config{base: &url.URL{}, rate: 0.5, writers: 3, rampPerMn: 100, msgBytes: 80}
	if err := ok.validate(); err != nil {
		t.Fatal(err)
	}
	fast := ok
	fast.rate = 9.5 // 570 requests a minute for messages alone
	if fast.validate() == nil {
		t.Error("a rate near the per-account API limit was accepted")
	}
	burst := ok
	burst.rampPerMn = 500
	if burst.validate() == nil {
		t.Error("a ramp above the WebSocket upgrade limit was accepted")
	}
	burst.spoofXFF = true
	if err := burst.validate(); err != nil {
		t.Errorf("a ramp with SPOOF_XFF was refused: %v", err)
	}
	small := ok
	small.msgBytes = 10
	if small.validate() == nil {
		t.Error("a message too short for its marker was accepted")
	}
}

func TestReactionsAndEditsCountAgainstTheRequestBudget(t *testing.T) {
	c := config{rate: 8.5, writers: 1, rampPerMn: 100, msgBytes: 80}
	if err := c.validate(); err != nil {
		t.Fatalf("messages alone fit: %v", err)
	}
	c.reactPct, c.editPct = 50, 50
	if c.validate() == nil {
		t.Error("messages plus edits and reactions exceed the budget but were accepted")
	}
}
