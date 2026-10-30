package main

import "github.com/msch128/mnema-talk/loadtest/viewer/internal/lt"

// thresholds fail the run when exceeded; a zero limit that says "off" is
// marked in the README.
type thresholds struct {
	LossPct        float64 `json:"max_loss_pct"`
	P95Ms          float64 `json:"max_p95_ms"`
	P99Ms          float64 `json:"max_p99_ms"`
	SendErrorPct   float64 `json:"max_send_error_pct"`
	WSErrors       int64   `json:"max_ws_errors"`
	Reconnects     int64   `json:"max_reconnects"`
	NeverConnected int64   `json:"max_never_connected"`
	GoroutineLeak  int64   `json:"max_goroutine_leak"`
	CPUCores       float64 `json:"max_cpu_cores"` // 0 = not checked
	RSSMB          float64 `json:"max_rss_mb"`    // 0 = not checked
}

func loadThresholds() thresholds {
	return thresholds{
		LossPct:        lt.Float("MAX_LOSS_PCT", 0.1),
		P95Ms:          lt.Float("MAX_P95_MS", 500),
		P99Ms:          lt.Float("MAX_P99_MS", 1500),
		SendErrorPct:   lt.Float("MAX_SEND_ERROR_PCT", 1),
		WSErrors:       int64(lt.Int("MAX_WS_ERRORS", 0)),
		Reconnects:     int64(lt.Int("MAX_RECONNECTS", 0)),
		NeverConnected: int64(lt.Int("MAX_NEVER_CONNECTED", 0)),
		GoroutineLeak:  int64(lt.Int("MAX_GOROUTINE_LEAK", 30)),
		CPUCores:       lt.Float("MAX_CPU_CORES", 0),
		RSSMB:          lt.Float("MAX_RSS_MB", 0),
	}
}

// evaluate checks a finished run against the thresholds.
func evaluate(r *result, th thresholds) lt.Checks {
	var c lt.Checks
	c.Zero("sessions that never connected", int64(r.Sessions.NeverConnected), th.NeverConnected)
	c.Zero("unexpected WebSocket errors", r.Sessions.WSErrors, th.WSErrors)
	c.Zero("reconnects", r.Sessions.Reconnects, th.Reconnects)
	checkKind(&c, "message", r.Messages, th, true)
	checkKind(&c, "edit", r.Edits, th, false)
	checkKind(&c, "reaction", r.Reactions, th, false)
	if r.Messages.Sent == 0 && r.Writers > 0 {
		c.Failf("no message was delivered to the writers' sessions: nothing was measured")
	}
	if r.Messages.SendAttempts > 0 {
		c.Max("failed sends (%)", float64(r.Messages.SendErrors)/float64(r.Messages.SendAttempts)*100, th.SendErrorPct)
	}
	checkServer(&c, r.Server, th)
	return c
}

// checkKind judges the loss and latency of one kind of event; the delay of
// edits and reactions is held to the same limits as that of messages.
func checkKind(c *lt.Checks, name string, k kindResult, th thresholds, required bool) {
	if k.Expected == 0 {
		if required {
			c.Failf("no %s was expected anywhere: nothing was measured", name)
		}
		return
	}
	c.Max(name+" loss (%)", k.LossPct, th.LossPct)
	c.Max(name+" delivery p95 (ms)", k.Latency.P95Ms, th.P95Ms)
	c.Max(name+" delivery p99 (ms)", k.Latency.P99Ms, th.P99Ms)
}

func checkServer(c *lt.Checks, s lt.ServerSummary, th thresholds) {
	if !s.Measured {
		return
	}
	c.Max("server CPU (cores, hold average)", s.CPUCoresHold, th.CPUCores)
	c.Max("server memory (MB, peak resident)", s.RSSPeakMB, th.RSSMB)
	if th.GoroutineLeak > 0 && s.GoroutinesPost > 0 {
		c.Zero("server goroutines left after everyone left", int64(s.GoroutinesPost-s.GoroutinesPre), th.GoroutineLeak)
	}
}
