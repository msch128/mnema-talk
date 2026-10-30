package ws

import (
	"sync"
	"time"
)

// takeoverMarks remembers which pages' stays a takeover ended, so such a page,
// reconnecting later and resuming the call, is told instead of taking it back.
// The page that took over is not affected: its resume carries another tab id.
// A page's mark ends when that page joins the room on purpose, and after an hour.
type takeoverMarks struct {
	mu sync.Mutex
	at map[voiceKey]map[string]time.Time // victim tab ids by stay
}

const takeoverMarkTTL = time.Hour

func (m *takeoverMarks) mark(k voiceKey, tab string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if m.at == nil {
		m.at = map[voiceKey]map[string]time.Time{}
	}
	for key, tabs := range m.at {
		for id, t := range tabs {
			if now.Sub(t) > takeoverMarkTTL {
				delete(tabs, id)
			}
		}
		if len(tabs) == 0 {
			delete(m.at, key)
		}
	}
	if m.at[k] == nil {
		m.at[k] = map[string]time.Time{}
	}
	m.at[k][tab] = now
}

// clear ends the mark of a page that joined the room on purpose.
func (m *takeoverMarks) clear(k voiceKey, tab string) {
	m.mu.Lock()
	delete(m.at[k], tab)
	m.mu.Unlock()
}

// has reports whether a takeover ended the stay of the page with this tab id.
// Pages without an id (older clients) are never refused.
func (m *takeoverMarks) has(k voiceKey, tab string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.at[k][tab]
	return ok && tab != "" && time.Since(t) <= takeoverMarkTTL
}
