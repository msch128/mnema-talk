package main

import (
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/loadtest/viewer/internal/lt"
)

func vdefaults() voiceThresholds {
	return voiceThresholds{NeverConnected: 0, Failed: 0, Flaps: 0, NeverReceived: 0, LossPct: 1, FrozenPct: 5, MinBitratePct: 50, GoroutineLeak: 30}
}

// mixedRun is a healthy result of the mixed scenario: 100 members, 1 screen,
// 10 cameras, 3 speakers, all watching everything.
func mixedRun() (*result, expectations) {
	res := &result{
		Members: 100,
		Media: map[mediaClass]classResult{
			classScreen: {Members: 99, Tracks: 99, KbpsPerTrackAvg: 1500},
			classCamera: {Members: 100, Tracks: 990, KbpsPerTrackAvg: 300},
			classAudio:  {Members: 100, Tracks: 300, KbpsPerTrackAvg: 36},
		},
	}
	exp := expectations{members: 100, roles: roleCounts{1, 10, 3}, cameras: true, screenKbps: 1500, cameraKbps: 300}
	return res, exp
}

func vhas(c lt.Checks, part string) bool {
	for _, v := range c.Violations {
		if strings.Contains(v, part) {
			return true
		}
	}
	return false
}

func TestAHealthyMixedRunPasses(t *testing.T) {
	res, exp := mixedRun()
	if c := evaluateVoice(res, exp, vdefaults()); !c.OK() {
		t.Fatalf("violations: %v", c.Violations)
	}
}

func TestMembersThatNeverConnectedOrNeverReceivedFail(t *testing.T) {
	res, exp := mixedRun()
	res.NeverConnected = 2
	cam := res.Media[classCamera]
	cam.Members = 96 // 98 connected members should all see the cameras
	res.Media[classCamera] = cam
	c := evaluateVoice(res, exp, vdefaults())
	if !vhas(c, "never connected") || !vhas(c, "never received camera") {
		t.Fatalf("violations: %v", c.Violations)
	}
}

func TestASingleSharerDoesNotReceiveItsOwnScreen(t *testing.T) {
	res, exp := mixedRun() // 99 of 100 received the screen: fine
	if c := evaluateVoice(res, exp, vdefaults()); vhas(c, "screen") {
		t.Fatalf("violations: %v", c.Violations)
	}
	exp.roles.screens = 2 // with two sharers everybody receives one
	if c := evaluateVoice(res, exp, vdefaults()); !vhas(c, "never received screen") {
		t.Fatalf("violations: %v", c.Violations)
	}
}

func TestLossFreezesAndLowBitrateFail(t *testing.T) {
	res, exp := mixedRun()
	cam := res.Media[classCamera]
	cam.LossPct = 3
	cam.MembersFrozen = 20
	cam.KbpsPerTrackAvg = 100
	res.Media[classCamera] = cam
	c := evaluateVoice(res, exp, vdefaults())
	for _, want := range []string{"camera packet loss", "camera members with a freeze", "camera received at 100 kbit/s"} {
		if !vhas(c, want) {
			t.Errorf("no violation for %q: %v", want, c.Violations)
		}
	}
	// The same numbers pass at the limits.
	cam.LossPct, cam.MembersFrozen, cam.KbpsPerTrackAvg = 1, 5, 150
	res.Media[classCamera] = cam
	if c := evaluateVoice(res, exp, vdefaults()); !c.OK() {
		t.Fatalf("violations at the limits: %v", c.Violations)
	}
}

func TestRelayModeRequiresEveryConnectedMemberToBeRelayed(t *testing.T) {
	res, exp := mixedRun()
	exp.relay = true
	res.Relayed = 97
	c := evaluateVoice(res, exp, vdefaults())
	if !vhas(c, "3 members connected without the relay") {
		t.Fatalf("violations: %v", c.Violations)
	}
	res.Relayed = 100
	if c := evaluateVoice(res, exp, vdefaults()); !c.OK() {
		t.Fatalf("violations: %v", c.Violations)
	}
}

func TestServerLimitsApplyOnlyWhenMeasuredAndSet(t *testing.T) {
	res, exp := mixedRun()
	res.Server = lt.ServerSummary{Measured: true, CPUCoresHold: 3.9, RSSPeakMB: 900, GoroutinesPre: 100, GoroutinesPost: 400}
	c := evaluateVoice(res, exp, vdefaults())
	if vhas(c, "CPU") || vhas(c, "memory") || !vhas(c, "goroutines") {
		t.Fatalf("violations: %v", c.Violations)
	}
	th := vdefaults()
	th.CPUCores, th.RSSMB = 2, 500
	if c := evaluateVoice(res, exp, th); !vhas(c, "CPU") || !vhas(c, "memory") {
		t.Fatalf("violations: %v", c.Violations)
	}
}

func TestRolesFollowThePositionInTheSlice(t *testing.T) {
	rc := roleCounts{screens: 1, cameras: 2, audio: 1}
	want := []string{"screen", "camera", "camera", "audio", "viewer", "viewer"}
	for i, w := range want {
		if got := rc.roleOf(i).String(); got != w {
			t.Errorf("position %d: %s, want %s", i, got, w)
		}
	}
}

func TestConfigRefusesMorePublishersThanMembers(t *testing.T) {
	c := viewerConfig{channel: "c", sharers: []string{"s"}, from: 0, to: 5, roles: roleCounts{1, 5, 0}}
	if c.validate() == nil {
		t.Fatal("6 publishers among 5 members accepted")
	}
	c.roles = roleCounts{1, 2, 0}
	if err := c.validate(); err != nil {
		t.Fatal(err)
	}
	c.sharers = nil
	if err := c.validate(); err != nil {
		t.Fatalf("cameras are something to watch: %v", err)
	}
	c.roles = roleCounts{0, 0, 2}
	if err := c.validate(); err != nil {
		t.Fatalf("an audio-only run is a valid shape: %v", err)
	}
	c.roles = roleCounts{}
	if c.validate() == nil {
		t.Fatal("a run that watches nothing was accepted")
	}
}
