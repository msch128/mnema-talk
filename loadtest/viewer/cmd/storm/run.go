package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mnema-xyz/talk/loadtest/viewer/internal/lt"
)

type run struct {
	cfg config

	upgrade, firstSync, back atomic.Pointer[lt.Histogram]

	ready    atomic.Int64
	backN    atomic.Int64
	drops    atomic.Int64
	dropOnce sync.Once
	dropAt   atomic.Int64 // unix nanos of the first drop
	allBack  chan struct{}
	backOnce sync.Once

	mu       sync.Mutex
	statuses map[string]map[int]int64
	latency  map[string]*lt.Histogram
}

// latencyOf is the latency histogram of one endpoint.
func (r *run) latencyOf(name string) *lt.Histogram {
	r.mu.Lock()
	defer r.mu.Unlock()
	h := r.latency[name]
	if h == nil {
		h = &lt.Histogram{}
		r.latency[name] = h
	}
	return h
}

func newRun(cfg config) *run {
	r := &run{
		cfg:      cfg,
		allBack:  make(chan struct{}),
		statuses: map[string]map[int]int64{},
		latency:  map[string]*lt.Histogram{},
	}
	r.upgrade.Store(&lt.Histogram{})
	r.firstSync.Store(&lt.Histogram{})
	r.back.Store(&lt.Histogram{})
	return r
}

// browserClient is one browser's connections to the server: HTTP/1.1 keeps at
// most six per origin, reused between requests.
func browserClient() *http.Client {
	tr := &http.Transport{MaxConnsPerHost: 6, MaxIdleConnsPerHost: 6, IdleConnTimeout: 90 * time.Second}
	return &http.Client{Transport: tr, Timeout: 30 * time.Second}
}

func (r *run) count(name string, status int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m := r.statuses[name]
	if m == nil {
		m = map[int]int64{}
		r.statuses[name] = m
	}
	m[status]++
}

// connected serves one open socket: it reads it, resyncs and reports the
// session back (after a drop) once the resync is done. It returns when the
// socket ends.
func (s *session) connected(ctx context.Context, conn *websocket.Conn, reconnect bool) {
	done := make(chan struct{})
	go func() { hold(conn); close(done) }()
	opened := time.Now()
	s.resync(ctx, reconnect)
	s.r.firstSync.Load().Add(time.Since(opened))
	if reconnect && !s.back {
		s.back = true
		s.r.sessionBack()
	} else if !reconnect {
		s.r.ready.Add(1)
	}
	select {
	case <-done:
	case <-ctx.Done():
		conn.Close()
		<-done
	}
}

func (r *run) sessionBack() {
	at := r.dropAt.Load()
	if at != 0 {
		r.back.Load().Add(time.Since(time.Unix(0, at)))
	}
	if r.backN.Add(1) == int64(r.cfg.sessions) {
		r.backOnce.Do(func() { close(r.allBack) })
	}
}

func (r *run) dropped() {
	r.drops.Add(1)
	r.dropOnce.Do(func() { r.dropAt.Store(time.Now().UnixNano()) })
}

// live is one session's whole life: the first connect, then for every drop
// the client's way back.
func (s *session) live(ctx context.Context) {
	var conn *websocket.Conn
	for conn == nil {
		if conn = s.open(ctx); conn == nil && !sleep(ctx, 2*time.Second) {
			return
		}
	}
	s.connected(ctx, conn, false)
	for ctx.Err() == nil {
		s.r.dropped()
		if conn = s.reconnect(ctx); conn == nil {
			return
		}
		s.connected(ctx, conn, true)
	}
}

func (r *run) execute(parent context.Context) (*result, error) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	var wg sync.WaitGroup
	tick := time.NewTicker(time.Second / time.Duration(r.cfg.perSecond))
	for i := range r.cfg.sessions {
		<-tick.C
		s := &session{idx: i, tok: r.cfg.tokens[i], r: r, http: browserClient()}
		wg.Go(func() { s.live(ctx) })
	}
	tick.Stop()
	if err := r.waitReady(ctx); err != nil {
		return nil, err
	}
	fmt.Fprintf(os.Stderr, "storm: %d sessions connected; restart the server now\n", r.cfg.sessions)
	r.markReady()
	res := r.waitBack(ctx)
	cancel()
	wg.Wait()
	return res, nil
}

func (r *run) waitReady(ctx context.Context) error {
	for r.ready.Load() < int64(r.cfg.sessions) {
		if r.drops.Load() > 0 {
			return errors.New("a session dropped before all were connected")
		}
		if !sleep(ctx, 100*time.Millisecond) {
			return ctx.Err()
		}
	}
	return nil
}

func (r *run) markReady() {
	if r.cfg.readyFile != "" {
		_ = os.WriteFile(r.cfg.readyFile, []byte("ready\n"), 0o600)
	}
	// What the first connects counted is not the storm.
	r.mu.Lock()
	r.statuses = map[string]map[int]int64{}
	r.latency = map[string]*lt.Histogram{}
	r.mu.Unlock()
	r.upgrade.Store(&lt.Histogram{})
	r.firstSync.Store(&lt.Histogram{})
}

func (r *run) waitBack(ctx context.Context) *result {
	for r.dropAt.Load() == 0 {
		if !sleep(ctx, 50*time.Millisecond) {
			return r.result(false)
		}
	}
	timeout := time.NewTimer(r.cfg.timeout)
	defer timeout.Stop()
	select {
	case <-r.allBack:
		return r.result(true)
	case <-timeout.C:
	case <-ctx.Done():
	}
	return r.result(false)
}
