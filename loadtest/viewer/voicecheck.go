package main

import (
	"github.com/msch128/mnema-talk/loadtest/viewer/internal/lt"
)

// voiceThresholds are the limits EVALUATE=1 holds a run to.
type voiceThresholds struct {
	NeverConnected int64   `json:"max_never_connected"`
	Failed         int64   `json:"max_failed"`
	Flaps          int64   `json:"max_flaps"`
	NeverReceived  int64   `json:"max_never_received"`
	LossPct        float64 `json:"max_loss_pct"`    // per class of media
	FrozenPct      float64 `json:"max_frozen_pct"`  // members with a frozen video
	MinBitratePct  float64 `json:"min_bitrate_pct"` // of the published rate
	GoroutineLeak  int64   `json:"max_goroutine_leak"`
	CPUCores       float64 `json:"max_cpu_cores"` // 0 = not checked
	RSSMB          float64 `json:"max_rss_mb"`    // 0 = not checked
}

func loadVoiceThresholds() voiceThresholds {
	return voiceThresholds{
		NeverConnected: int64(envInt("MAX_NEVER_CONNECTED", 0)),
		Failed:         int64(envInt("MAX_FAILED", 0)),
		Flaps:          int64(envInt("MAX_FLAPS", 0)),
		NeverReceived:  int64(envInt("MAX_NEVER_RECEIVED", 0)),
		LossPct:        lt.Float("MAX_LOSS_PCT", 1),
		FrozenPct:      lt.Float("MAX_FROZEN_PCT", 5),
		MinBitratePct:  lt.Float("MIN_BITRATE_PCT", 50),
		GoroutineLeak:  int64(envInt("MAX_GOROUTINE_LEAK", 30)),
		CPUCores:       lt.Float("MAX_CPU_CORES", 0),
		RSSMB:          lt.Float("MAX_RSS_MB", 0),
	}
}

// expectations are what the run set up, which the result is judged against.
type expectations struct {
	members    int
	roles      roleCounts
	cameras    bool // every member watches every camera
	relay      bool
	screenKbps int
	cameraKbps int
}

func expectationsOf(c viewerConfig) expectations {
	set := loadPublishSettings()
	return expectations{
		members: c.to - c.from, roles: c.roles, cameras: c.cameras.limit != 0, relay: c.relay,
		screenKbps: set.screenKbps, cameraKbps: set.cameraKbps,
	}
}

// finishResult adds the server's numbers and, with EVALUATE=1, the verdict.
func finishResult(res *result, c viewerConfig, server lt.ServerSummary) {
	res.Relay, res.Server = c.relay, server
	res.Stages = splitStages(res, c)
	res.OK = true
	if !lt.Bool("EVALUATE") {
		return
	}
	th := loadVoiceThresholds()
	checks := evaluateVoice(res, expectationsOf(c), th)
	res.Thresholds, res.Violations, res.OK = &th, checks.Violations, checks.OK()
}

func evaluateVoice(res *result, exp expectations, th voiceThresholds) lt.Checks {
	var c lt.Checks
	c.Zero("members that never connected", int64(res.NeverConnected), th.NeverConnected)
	c.Zero("members whose signalling failed", int64(res.Failed), th.Failed)
	c.Zero("connection flaps", res.Flaps, th.Flaps)
	if exp.relay {
		if direct := connectedMembers(res) - res.Relayed; direct > 0 {
			c.Failf("%d members connected without the relay", direct)
		}
	}
	checkMedia(&c, res, exp, th)
	checkServerLoad(&c, res.Server, th)
	return c
}

func connectedMembers(res *result) int { return res.Members - res.NeverConnected }

// checkMedia judges what the members received: that everybody got what was
// published, without freezes or unrepaired loss, at the published rate.
func checkMedia(c *lt.Checks, res *result, exp expectations, th voiceThresholds) {
	missing := missingReceivers(res, exp)
	for _, class := range allClasses {
		if n := missing[class]; n > 0 {
			c.Zero("members that never received "+string(class), int64(n), th.NeverReceived)
		}
		r, ok := res.Media[class]
		if !ok {
			continue
		}
		if class != classScreenAudio {
			c.Max(string(class)+" packet loss (%)", r.LossPct, th.LossPct)
		}
		if class.video() && r.Members > 0 {
			c.Max(string(class)+" members with a freeze (%)", float64(r.MembersFrozen)/float64(r.Members)*100, th.FrozenPct)
		}
	}
	checkBitrate(c, res.Media[classScreen], exp.roles.screens, exp.screenKbps, "screen", th)
	checkBitrate(c, res.Media[classCamera], exp.roles.cameras, exp.cameraKbps, "camera", th)
}

// checkBitrate holds the average rate of a received track to a share of the
// rate the synthetic publisher sends.
func checkBitrate(c *lt.Checks, r classResult, publishers, kbps int, name string, th voiceThresholds) {
	if publishers == 0 || kbps == 0 || r.Members == 0 || th.MinBitratePct <= 0 {
		return
	}
	if min := float64(kbps) * th.MinBitratePct / 100; r.KbpsPerTrackAvg < min {
		c.Failf("%s received at %.0f kbit/s per track, below %.0f%% of the published %d kbit/s", name, r.KbpsPerTrackAvg, th.MinBitratePct, kbps)
	}
}

// missingReceivers counts, per class, the connected members that should have
// received it and did not. A publisher does not receive its own media.
func missingReceivers(res *result, exp expectations) map[mediaClass]int {
	connected := connectedMembers(res)
	want := map[mediaClass]int{}
	if exp.roles.screens > 0 {
		want[classScreen] = connected - ifOnly(exp.roles.screens)
	}
	if exp.roles.cameras > 0 && exp.cameras {
		want[classCamera] = connected - ifOnly(exp.roles.cameras)
	}
	if exp.roles.audio > 0 {
		want[classAudio] = connected - ifOnly(exp.roles.audio)
	}
	out := map[mediaClass]int{}
	for class, n := range want {
		out[class] = max(n-res.Media[class].Members, 0)
	}
	return out
}

// ifOnly is 1 when a single member publishes (who then receives nothing of
// it), else 0: with several publishers everybody receives somebody's.
func ifOnly(publishers int) int {
	if publishers == 1 {
		return 1
	}
	return 0
}

func checkServerLoad(c *lt.Checks, s lt.ServerSummary, th voiceThresholds) {
	if !s.Measured {
		return
	}
	c.Max("server CPU (cores, hold average)", s.CPUCoresHold, th.CPUCores)
	c.Max("server memory (MB, peak resident)", s.RSSPeakMB, th.RSSMB)
	if th.GoroutineLeak > 0 && s.GoroutinesPost > 0 {
		c.Zero("server goroutines left after everyone left", int64(s.GoroutinesPost-s.GoroutinesPre), th.GoroutineLeak)
	}
}
