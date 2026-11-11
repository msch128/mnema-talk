package ws

import (
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/sfu"
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

// seatPeer makes peer, which the SFU just gave c for chID's room, c's media
// peer and settles which connection of the user holds the seat there. The
// SFU keeps one peer per user and room, and each new one replaces the one
// before: the seat belongs to the connection whose peer is the latest, the
// only one that carries media. The seats are ordered by the peers' own
// numbers, not by who gets here first, so two tabs that join at once cannot
// end with the seat on a peer the SFU has already replaced. It reports
// false when c's peer was replaced by a sibling's meanwhile: c is told and
// gives way, and the seat stays with that sibling.
func (h *Hub) seatPeer(c *Client, chID uuid.UUID, peer *sfu.Peer, evictReason string) bool {
	c.setPeer(peer)
	mine := peer.Order()
	c.stampSeat(mine)
	if h.evictSiblingConnections(c, chID, mine, evictReason) {
		return true
	}
	if c.currentVoice() != nil { // else that sibling has told c already
		c.setVoice(nil)
		c.SendEvent(events.Untyped(events.VoiceKicked, map[string]any{"channel_id": chID, "reason": kickJoinedElsewhere}))
	}
	return false
}

// evictSiblingConnections takes the media of chID's room away from the other
// connections of c's user (a second tab or device) whose peer c's replaced.
// They are told instead of being left on a dead connection. Presence stays;
// c now holds the seat. Their voiceMu is not taken: two tabs joining at once
// would wait on each other, and detaching only touches the connection's own
// state. A sibling still joining, without a peer of its own yet, stays and
// settles this itself when it gets one. It reports false when a sibling
// holds a later peer than c's.
func (h *Hub) evictSiblingConnections(c *Client, chID uuid.UUID, mine uint64, reason string) bool {
	holds := true
	for _, o := range h.clientsOf(c.User.ID) {
		cur, stamp := o.currentVoice(), o.seatStamp()
		if o == c || cur == nil || *cur != chID || stamp == 0 {
			continue
		}
		if stamp > mine {
			holds = false
			continue
		}
		if reason == kickTakenOver {
			h.taken.mark(voiceKey{o.User.ID, chID}, o.tabID())
		}
		o.setVoice(nil)
		o.SendEvent(events.Untyped(events.VoiceKicked, map[string]any{"channel_id": chID, "reason": reason}))
	}
	return holds
}
