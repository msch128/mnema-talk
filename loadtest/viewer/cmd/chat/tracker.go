package main

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/msch128/mnema-talk/loadtest/viewer/internal/lt"
)

// kind is what happened to a message: it was created, edited, or reacted to.
// Each kind is delivered to every session that was connected when it was sent.
type kind int

const (
	kindMessage kind = iota
	kindEdit
	kindReaction
	kinds
)

func (k kind) String() string { return [...]string{"message", "edit", "reaction"}[k] }

// slot is the accounting of one message and of what was done to it later.
type slot struct {
	startNS  [kinds]atomic.Int64 // when the request was started, 0 = never
	expected [kinds]atomic.Int32 // sessions that should receive the event
	got      [kinds]atomic.Int32 // events received
}

// tracker accounts for every event the writers cause and every one the
// sessions receive. Latency is the time from the start of the request that
// caused the event until a session reads it, measured on one clock.
type tracker struct {
	epoch time.Time

	mu    sync.RWMutex
	slots []*slot
	ids   sync.Map // message id -> sequence number

	lat [kinds]lt.Histogram
}

func newTracker() *tracker { return &tracker{epoch: time.Now()} }

func (t *tracker) now() int64 { return int64(time.Since(t.epoch)) }

// newMessage opens the accounting of a message to be sent to connected
// sessions and returns its sequence number.
func (t *tracker) newMessage(connected int) int {
	s := &slot{}
	s.startNS[kindMessage].Store(t.now() + 1) // +1: zero means "not started"
	s.expected[kindMessage].Store(int32(connected))
	t.mu.Lock()
	defer t.mu.Unlock()
	t.slots = append(t.slots, s)
	return len(t.slots) - 1
}

func (t *tracker) slot(seq int) *slot {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if seq < 0 || seq >= len(t.slots) {
		return nil
	}
	return t.slots[seq]
}

// begin starts the accounting of a later event (edit, reaction) of message seq.
func (t *tracker) begin(k kind, seq, connected int) {
	if s := t.slot(seq); s != nil {
		s.expected[k].Store(int32(connected))
		s.startNS[k].Store(t.now() + 1)
	}
}

// cancel withdraws an event whose request failed: nobody should have got it.
func (t *tracker) cancel(k kind, seq int) {
	if s := t.slot(seq); s != nil {
		s.expected[k].Store(0)
	}
}

// delivered records that a session read the event of kind k of message seq.
// It reports false for an event the tracker does not know (another run's, or
// a message sent by someone else).
func (t *tracker) delivered(k kind, seq int) bool {
	s := t.slot(seq)
	if s == nil {
		return false
	}
	start := s.startNS[k].Load()
	if start == 0 {
		return false
	}
	s.got[k].Add(1)
	t.lat[k].Add(time.Duration(t.now() + 1 - start))
	return true
}

// totals sums, over all messages, the events expected, read, lost (expected
// but not read) and read more than once beyond what was expected.
type totals struct {
	Sent, Expected, Got, Lost, Duplicates int64
}

func (t *tracker) totals(k kind) totals {
	t.mu.RLock()
	defer t.mu.RUnlock()
	var tot totals
	for _, s := range t.slots {
		exp, got := int64(s.expected[k].Load()), int64(s.got[k].Load())
		if s.startNS[k].Load() != 0 && exp > 0 {
			tot.Sent++
		}
		tot.Expected += exp
		tot.Got += got
		tot.Lost += max(exp-got, 0)
		tot.Duplicates += max(got-exp, 0)
	}
	return tot
}

// lossPct is the share of expected events that never arrived.
func (tot totals) lossPct() float64 {
	if tot.Expected == 0 {
		return 0
	}
	return float64(tot.Lost) / float64(tot.Expected) * 100
}
