package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/msch128/mnema-talk/loadtest/viewer/internal/lt"
)

// requestStats counts the REST requests of one kind of event.
type requestStats struct {
	attempts, errors, rateLimited atomic.Int64
}

// run is the state shared by the sessions and writers of one test.
type run struct {
	cfg    config
	id     string // identifies this run's messages
	marker string // the start of every message this run sends
	track  *tracker
	http   *http.Client

	sctx           context.Context // ends the sessions and the writers
	connected      atomic.Int64
	connectedPeak  atomic.Int64
	sessionsEver   atomic.Int64 // sessions that connected at least once
	wsErrors       atomic.Int64
	reconnects     atomic.Int64
	typingSent     atomic.Int64
	typingReceived atomic.Int64
	framesBad      atomic.Int64
	connectMs      lt.Histogram
	req            [kinds]requestStats

	failMu   sync.Mutex
	failures map[string]*failure
}

// failure counts the failed requests of one kind and status, with the first
// answer as a sample.
type failure struct {
	Count  int64  `json:"count"`
	Sample string `json:"sample"`
}

func newRun(cfg config) *run {
	var b [4]byte
	_, _ = rand.Read(b[:])
	id := hex.EncodeToString(b[:])
	return &run{
		cfg: cfg, id: id, marker: "lt " + id + " ", track: newTracker(),
		http: &http.Client{
			Timeout: 20 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns: 2048, MaxIdleConnsPerHost: 2048, IdleConnTimeout: 60 * time.Second,
				DialContext: (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			},
		},
	}
}

// sessionUp and sessionDown keep the count of connected sessions, which is
// what a message is expected by.
func (r *run) sessionUp() {
	n := r.connected.Add(1)
	for {
		peak := r.connectedPeak.Load()
		if n <= peak || r.connectedPeak.CompareAndSwap(peak, n) {
			return
		}
	}
}

func (r *run) sessionDown() { r.connected.Add(-1) }

// pacer hands out slots at a fixed rate, for the WebSocket upgrades.
type pacer struct {
	every time.Duration
	next  atomic.Int64 // unix nanoseconds of the next free slot
}

func newPacer(perMinute int) *pacer {
	if perMinute <= 0 {
		return &pacer{}
	}
	return &pacer{every: time.Minute / time.Duration(perMinute)}
}

// wait blocks until the caller's slot arrives or ctx ends.
func (p *pacer) wait(ctx context.Context) error {
	if p.every == 0 {
		return ctx.Err()
	}
	now := time.Now().UnixNano()
	var slot int64
	for {
		cur := p.next.Load()
		slot = max(cur, now)
		if p.next.CompareAndSwap(cur, slot+int64(p.every)) {
			break
		}
	}
	select {
	case <-time.After(time.Duration(slot - now)):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
