package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/opwarn"
	"github.com/msch128/mnema-talk/internal/setupcheck"
)

// setupLogDelay is how long after the start the setup checks are logged:
// long enough for object storage and the clock measurement to settle.
var setupLogDelay = 20 * time.Second

// clockRecheck is how often the clock is compared with NTP again.
const clockRecheck = 6 * time.Hour

// startSetupChecks measures the clock in the background (CLOCK_CHECK_ENABLED)
// and logs the setup checks once shortly after the start; the admin status
// page evaluates the same checks on every request.
func startSetupChecks(ctx context.Context, cfg *config.Config, rt *setupcheck.Runtime, ping func(context.Context) error, storageReady func() error, turnProbe func() string) {
	if cfg.ClockCheck {
		go watchClock(ctx, rt, setupcheck.DefaultNTPServers, clockRecheck)
	}
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(setupLogDelay):
		}
		in := setupcheck.Input{
			Cfg:               cfg,
			Runtime:           rt,
			Warnings:          opwarn.Default.List(),
			DBReachable:       ping(ctx) == nil,
			StorageConfigured: true,
			StorageErr:        storageReady(),
		}
		if turnProbe != nil {
			in.TURNProbe = turnProbe()
		}
		setupcheck.Log(slog.Default(), setupcheck.Run(in))
	}()
}

// watchClock records the clock skew now and then every interval. A failed
// measurement (no NTP server reachable) leaves the check out of the list.
func watchClock(ctx context.Context, rt *setupcheck.Runtime, servers []string, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		mctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		skew, err := setupcheck.MeasureClockSkew(mctx, servers, 3*time.Second)
		cancel()
		if err != nil {
			slog.Debug("clock check: no NTP answer", "err", err)
		} else {
			rt.SetClockSkew(skew)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
