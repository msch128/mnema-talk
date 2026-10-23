package push

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// key is one member's notifications from one channel.
type key struct {
	user, channel uuid.UUID
}

// held is a push waiting for its window to end, with what building it needs.
type held struct {
	user    uuid.UUID
	notice  Notice
	locale  string
	preview bool
}

type entry struct {
	lastSent time.Time
	waiting  *held
	timer    *time.Timer
}

// coalescer keeps a burst of messages in one channel from becoming a burst of
// notifications: the first goes out at once, and what arrives within the
// window afterwards is sent as one push ("3 new messages") when the window
// ends. A mention is never held back.
type coalescer struct {
	mu      sync.Mutex
	window  time.Duration
	now     func() time.Time
	entries map[key]*entry
}

func newCoalescer(window time.Duration, now func() time.Time) *coalescer {
	return &coalescer{window: window, now: now, entries: map[key]*entry{}}
}

// add records a notice. It returns the push to send now (possibly merged with
// earlier held ones), or nil when the notice was held; flush is called with
// the merged push when the window ends.
func (c *coalescer) add(k key, h held, flush func(held)) *held {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	e := c.entries[k]
	if e == nil {
		e = &entry{}
		c.entries[k] = e
	}
	if now.Sub(e.lastSent) >= c.window || h.notice.Mention {
		return c.release(e, h, now)
	}
	if e.waiting == nil {
		h.notice.Count = 1
		e.waiting = &h
		e.timer = time.AfterFunc(e.lastSent.Add(c.window).Sub(now), func() { c.fire(k, flush) })
		return nil
	}
	h.notice.Count = e.waiting.notice.Count + 1
	e.waiting.notice = h.notice
	return nil
}

// release merges the entry's held pushes into h and marks the send.
func (c *coalescer) release(e *entry, h held, now time.Time) *held {
	h.notice.Count = 1
	if e.waiting != nil {
		h.notice.Count += e.waiting.notice.Count
		e.timer.Stop()
		e.waiting, e.timer = nil, nil
	}
	e.lastSent = now
	return &h
}

// fire sends what was held when its window ended.
func (c *coalescer) fire(k key, flush func(held)) {
	c.mu.Lock()
	e := c.entries[k]
	var out *held
	if e != nil && e.waiting != nil {
		out = e.waiting
		e.waiting, e.timer = nil, nil
		e.lastSent = c.now()
	}
	c.mu.Unlock()
	if out != nil {
		flush(*out)
	}
}

// sweep forgets channels nobody was told about for a while.
func (c *coalescer) sweep() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	for k, e := range c.entries {
		if e.waiting == nil && now.Sub(e.lastSent) > 2*c.window {
			delete(c.entries, k)
		}
	}
}

// stop cancels every waiting timer (shutdown).
func (c *coalescer) stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.entries {
		if e.timer != nil {
			e.timer.Stop()
		}
	}
}
