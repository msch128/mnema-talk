// Command chat is the text-chat load test: N users hold a WebSocket each, a
// share of them writes messages (with typing indicators, reactions and edits),
// all of them read, and the test measures how long delivery takes, how many
// events never arrive and what the server spends on it. It speaks the
// protocol the web client speaks: the WebSocket at /api/ws for everything the
// server pushes and for typing, REST for sending, editing and reacting.
//
// Configuration is read from the environment:
//
//	BASE_URL      instance URL (http:// or https://)
//	TOKENS        JSON file [{"id": "...", "token": "..."}] (see cmd/mint)
//	CHANNEL_ID    text channel every user may view and write in
//	SESSIONS      WebSocket sessions, the first N tokens (default 100)
//	WRITER_PCT    share of the sessions that write, in percent (default 10)
//	RATE          messages per second per writer (default 0.2)
//	TYPING=0      no typing indicator before a message
//	REACT_PCT     percent of messages that get a reaction (default 5)
//	EDIT_PCT      percent of messages that get edited (default 3)
//	MSG_BYTES     message size (default 80)
//	FETCH_HISTORY=0  do not load the channel history on connect
//	RAMP_PER_MIN  WebSocket connects per minute (default 100; the server allows
//	              120 upgrades a minute per address), 0 = all at once
//	SPOOF_XFF=1   send a distinct X-Forwarded-For per session; only meaningful
//	              when the server trusts the sender as a proxy (README)
//	HOLD_S        seconds of load once everyone is connected (default 120)
//	SETTLE_S      seconds to wait for the last deliveries (default 10)
//	METRICS_TOKEN server METRICS_TOKEN: samples CPU and memory every 5 s
//	SESSION_COOKIE   cookie name when PUBLIC_URL is https but BASE_URL is not
//	OUT           path of the JSON result
//
// Thresholds (the run exits 1 when one is violated): MAX_LOSS_PCT (0.1),
// MAX_P95_MS (500), MAX_P99_MS (1500), MAX_SEND_ERROR_PCT (1), MAX_WS_ERRORS
// (0), MAX_RECONNECTS (0), MAX_NEVER_CONNECTED (0), MAX_GOROUTINE_LEAK (30),
// MAX_CPU_CORES and MAX_RSS_MB (off).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/msch128/mnema-talk/loadtest/viewer/internal/lt"
)

func main() {
	ok, err := mainErr()
	if err != nil {
		fmt.Fprintln(os.Stderr, "chat:", err)
		os.Exit(2)
	}
	if !ok {
		os.Exit(1)
	}
}

func mainErr() (bool, error) {
	cfg, err := loadConfig()
	if err != nil {
		return false, err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	started := time.Now()
	cpu := lt.StartCPUMeter()
	r := newRun(cfg)
	sampler := lt.StartSampler(lt.NewScraper(cfg.base, cfg.metrics), 5*time.Second)
	sampler.Mark("pre")

	sessions, wg, cancel := r.startSessions(ctx)
	r.waitConnected(ctx, sampler)
	r.hold(sampler, sessions)

	sampler.Mark("settle")
	sleep(ctx, cfg.settle)
	cancel()
	waitBounded(wg, 15*time.Second)
	sampler.SetPhase("post")
	time.Sleep(5 * time.Second) // the server finishes tearing the sessions down
	sampler.Mark("post")
	samples, scrapeErrors := sampler.Stop()

	res := r.result(started, lt.SummarizeServer(samples, scrapeErrors))
	res.GeneratorCores = cpu.Cores()
	printSummary(os.Stdout, res)
	return res.OK, writeResult(cfg.out, res)
}

func writeResult(path string, res *result) error {
	if path == "" {
		return nil
	}
	b, err := json.MarshalIndent(res, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// startSessions starts every session at the ramp rate. The returned cancel
// stops the sessions and the writers.
func (r *run) startSessions(ctx context.Context) ([]*session, *sync.WaitGroup, context.CancelFunc) {
	sctx, cancel := context.WithCancel(ctx)
	pace := newPacer(r.cfg.rampPerMn)
	var wg sync.WaitGroup
	sessions := make([]*session, r.cfg.sessions)
	for i := range sessions {
		sessions[i] = &session{idx: i, tok: r.cfg.tokens[i], r: r}
		wg.Go(func() { sessions[i].loop(sctx, pace) })
	}
	r.sctx = sctx
	return sessions, &wg, cancel
}

// waitConnected waits until every session is connected or the ramp, plus a
// minute of grace, is over.
func (r *run) waitConnected(ctx context.Context, sampler *lt.Sampler) {
	sampler.SetPhase("ramp")
	ramp := time.Duration(r.cfg.sessions) * time.Minute / time.Duration(max(r.cfg.rampPerMn, 1))
	if r.cfg.rampPerMn <= 0 {
		ramp = 0
	}
	deadline := time.Now().Add(ramp + time.Minute)
	for time.Now().Before(deadline) && int(r.connected.Load()) < r.cfg.sessions {
		if !sleep(ctx, time.Second) {
			return
		}
	}
	fmt.Printf("%d of %d sessions connected\n", r.connected.Load(), r.cfg.sessions)
}

// hold runs the writers for the hold time.
func (r *run) hold(sampler *lt.Sampler, sessions []*session) {
	sampler.Mark("hold")
	wctx, stop := context.WithTimeout(r.sctx, r.cfg.hold)
	defer stop()
	var wg sync.WaitGroup
	step := float64(len(sessions)) / float64(max(r.cfg.writers, 1))
	for i := range r.cfg.writers {
		w := newWriter(sessions[int(float64(i)*step)])
		wg.Go(func() { w.run(wctx) })
	}
	go r.progress(wctx)
	wg.Wait()
}

// progress prints one line every 10 s.
func (r *run) progress(ctx context.Context) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tot := r.track.totals(kindMessage)
			fmt.Printf("%s connected=%d messages sent=%d delivered=%d/%d wsErrors=%d reconnects=%d\n",
				time.Now().Format("15:04:05"), r.connected.Load(), tot.Sent, tot.Got, tot.Expected, r.wsErrors.Load(), r.reconnects.Load())
		}
	}
}

// waitBounded waits for wg, at most limit: a goroutine stuck in a network call
// must not keep the report from being written.
func waitBounded(wg *sync.WaitGroup, limit time.Duration) {
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(limit):
		fmt.Println("some sessions did not close within", limit)
	}
}
