package main

import (
	"os"
	"time"

	"github.com/msch128/mnema-talk/loadtest/viewer/internal/lt"
)

// The packet split compares what the generator sent and received with what the
// server counted, so all of them must cover the same stretch of time. That
// stretch is the window inside the hold: it begins when every member has had
// time to join (WINDOW_WARMUP_S, default 10) and ends when the hold does.
// Members are counted as audience only if they were connected at both ends.

// windowSnap is the state of the generator and the server at one moment.
type windowSnap struct {
	sent      map[string]int64 // packets published, by member
	recv      map[string]int64 // packets received, by member
	connected map[string]bool
	roles     map[string]string
	server    lt.Sample
	hasServer bool
}

func takeWindow(members []*member, sampler *lt.Sampler) windowSnap {
	w := windowSnap{sent: map[string]int64{}, recv: map[string]int64{}, connected: map[string]bool{}, roles: map[string]string{}}
	w.server, w.hasServer = sampler.Mark("hold")
	now := time.Now()
	for _, m := range members {
		w.sent[m.id] = m.sentPkt.Load()
		w.connected[m.id] = m.connected.Load()
		w.roles[m.id] = m.role.String()
		for _, tracks := range m.media(now) {
			for _, t := range tracks {
				w.recv[m.id] += t.packets
			}
		}
	}
	return w
}

// stageInput is what happened between two snapshots.
type stageInput struct {
	window    time.Duration
	sent      map[string]int64
	recv      map[string]int64
	audience  map[string]bool
	roles     map[string]string
	server    map[string]float64
	hasServer bool
}

func (a windowSnap) until(b windowSnap, window time.Duration) stageInput {
	in := stageInput{window: window, sent: map[string]int64{}, recv: map[string]int64{}, audience: map[string]bool{}, roles: b.roles}
	for id := range b.sent {
		in.sent[id] = b.sent[id] - a.sent[id]
		in.recv[id] = b.recv[id] - a.recv[id]
		in.audience[id] = a.connected[id] && b.connected[id]
	}
	if a.hasServer && b.hasServer {
		in.server, in.hasServer = lt.CounterDeltas(a.server, b.server), true
	}
	return in
}

// runHold lets the members run for the hold and returns the packet window.
func runHold(members []*member, c viewerConfig, sampler *lt.Sampler) stageInput {
	stop := os.Getenv("STOP_FILE")
	warm := min(time.Duration(envInt("WINDOW_WARMUP_S", 10))*time.Second, c.hold/4)
	watchMembers(members, warm, stop)
	start, began := takeWindow(members, sampler), time.Now()
	watchMembers(members, c.hold-warm, stop)
	end := takeWindow(members, sampler)
	return start.until(end, time.Since(began))
}
