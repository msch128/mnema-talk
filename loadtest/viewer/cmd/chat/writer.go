package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"
)

// writer is a session that sends messages at a steady rate, announces them
// with a typing indicator, and now and then edits or reacts to one of its own.
type writer struct {
	s *session
	// padding fills the message to the configured size.
	padding string
}

func newWriter(s *session) *writer {
	pad := max(s.r.cfg.msgBytes-len(s.r.marker)-8, 0)
	return &writer{s: s, padding: strings.Repeat("x", pad)}
}

// run sends until ctx ends. The first message is delayed by a random part of
// the interval so the writers do not fire together; every interval varies by
// 20 % either way.
func (w *writer) run(ctx context.Context) {
	interval := time.Duration(float64(time.Second) / w.s.r.cfg.rate)
	if !sleep(ctx, time.Duration(rand.Int64N(int64(interval)))) {
		return
	}
	for ctx.Err() == nil {
		think := time.Duration(0)
		if w.s.r.cfg.typing {
			w.s.sendTyping()
			think = min(interval/3, 800*time.Millisecond)
			if !sleep(ctx, think) {
				return
			}
		}
		wait := w.send(ctx)
		next := time.Duration(float64(interval)*(0.8+0.4*rand.Float64())) - think
		if !sleep(ctx, max(next, wait)) {
			return
		}
	}
}

func sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-time.After(d):
		return true
	case <-ctx.Done():
		return false
	}
}

// send posts one message and plans its edit and reaction. It returns how long
// to hold back when the server asked for it.
func (w *writer) send(ctx context.Context) time.Duration {
	r := w.s.r
	seq := r.track.newMessage(int(r.connected.Load()))
	content := fmt.Sprintf("%s%d %s", r.marker, seq, w.padding)
	body, out, retry := w.s.call(kindMessage, "POST", "/api/channels/"+r.cfg.channel+"/messages", map[string]string{"content": content})
	if out != outcomeOK {
		r.track.cancel(kindMessage, seq)
		return retry
	}
	var m struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(body, &m) != nil || m.ID == "" {
		return 0
	}
	r.track.ids.Store(m.ID, seq)
	w.plan(ctx, seq, m.ID)
	return 0
}

// plan schedules an edit and a reaction for a share of the messages.
func (w *writer) plan(ctx context.Context, seq int, id string) {
	cfg := w.s.r.cfg
	if rand.IntN(100) < cfg.reactPct {
		time.AfterFunc(100*time.Millisecond+time.Duration(rand.Int64N(int64(400*time.Millisecond))), func() { w.react(ctx, seq, id) })
	}
	if rand.IntN(100) < cfg.editPct {
		time.AfterFunc(200*time.Millisecond+time.Duration(rand.Int64N(int64(600*time.Millisecond))), func() { w.edit(ctx, seq, id) })
	}
}

func (w *writer) edit(ctx context.Context, seq int, id string) {
	if ctx.Err() != nil {
		return
	}
	r := w.s.r
	r.track.begin(kindEdit, seq, int(r.connected.Load()))
	content := fmt.Sprintf("%s%d %s", r.marker, seq, editedTag)
	_, out, _ := w.s.call(kindEdit, "PUT", "/api/channels/"+r.cfg.channel+"/messages/"+id, map[string]string{"content": content})
	if out != outcomeOK {
		r.track.cancel(kindEdit, seq)
	}
}

func (w *writer) react(ctx context.Context, seq int, id string) {
	if ctx.Err() != nil {
		return
	}
	r := w.s.r
	r.track.begin(kindReaction, seq, int(r.connected.Load()))
	_, out, _ := w.s.call(kindReaction, "POST", "/api/messages/"+id+"/reactions", map[string]string{"emoji": "\U0001F44D"})
	if out != outcomeOK {
		r.track.cancel(kindReaction, seq)
	}
}
