package ws

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
)

// A Stage is a Talk where nobody sends audio, camera or screen without talk
// power (doc/talks.md). Talk power and raised hands belong to one stay in the
// Talk: they live here, in memory, and are forgotten when the member leaves or
// the Talk stops being a Stage. The media itself is refused where it is
// decided anyway (mediaDenied), so a client that ignores the interface gains
// nothing.

// stageActionsPer is how many Stage commands (hand, talk power) one member
// may send per stageActionWindow.
const (
	stageActionsPer   = 30
	stageActionWindow = 10 * time.Second
)

// stageBook is the Stage state of every seat, guarded by its own lock (never
// held together with the hub's).
type stageBook struct {
	mu sync.Mutex
	// grant is talk power handed out by a manager.
	grant map[voiceKey]bool
	// hand is when a member raised their hand.
	hand map[voiceKey]time.Time
	// shown is whether the seat's talk power (granted or implied by Manage
	// stage) was last announced as on, so changes are announced once.
	shown map[voiceKey]bool
}

func (b *stageBook) granted(key voiceKey) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.grant[key]
}

func (b *stageBook) setGrant(key voiceKey, on bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if on {
		if b.grant == nil {
			b.grant = map[voiceKey]bool{}
		}
		b.grant[key] = true
		return
	}
	delete(b.grant, key)
}

// setHand records or clears a raised hand and reports whether that changed it.
func (b *stageBook) setHand(key voiceKey, raised bool, now time.Time) (time.Time, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	prev, was := b.hand[key]
	if raised == was {
		return prev, false
	}
	if !raised {
		delete(b.hand, key)
		return time.Time{}, true
	}
	if b.hand == nil {
		b.hand = map[voiceKey]time.Time{}
	}
	b.hand[key] = now
	return now, true
}

// setShown stores the announced talk power and reports whether it changed.
func (b *stageBook) setShown(key voiceKey, on bool) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.shown[key] == on {
		return false
	}
	if !on {
		delete(b.shown, key)
		return true
	}
	if b.shown == nil {
		b.shown = map[voiceKey]bool{}
	}
	b.shown[key] = true
	return true
}

// view is what a seat shows others: talk power and the raised hand.
func (b *stageBook) view(key voiceKey) (power bool, hand *time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if at, ok := b.hand[key]; ok {
		hand = &at
	}
	return b.shown[key], hand
}

// forget drops everything about one seat.
func (b *stageBook) forget(key voiceKey) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.grant, key)
	delete(b.hand, key)
	delete(b.shown, key)
}

// seats lists every seat the book holds anything about.
func (b *stageBook) seats() []voiceKey {
	b.mu.Lock()
	defer b.mu.Unlock()
	seen := map[voiceKey]bool{}
	for k := range b.grant {
		seen[k] = true
	}
	for k := range b.hand {
		seen[k] = true
	}
	for k := range b.shown {
		seen[k] = true
	}
	out := make([]voiceKey, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return out
}

// holds reports whether a member holds a permission in a channel.
func holds(snap *perm.Snapshot, user, ch uuid.UUID, need perm.Set) bool {
	sub := perm.Subject{ID: user}
	sub.Owner = snap.IsOwner(sub)
	return snap.Channel(sub, ch).Has(need)
}

// mayTalkOnStage is whether the seat has talk power: granted, or implied by
// Manage stage.
func (h *Hub) mayTalkOnStage(snap *perm.Snapshot, key voiceKey) bool {
	return h.stage.granted(key) || holds(snap, key.user, key.ch, perm.ManageStage)
}

func (h *Hub) inRoom(ch, user uuid.UUID) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.voice[ch][user]
	return ok
}

// seatStage settles the talk power of a member who just took a seat and
// returns what to announce with their join.
func (h *Hub) seatStage(user, ch uuid.UUID) (power bool, hand *time.Time) {
	snap, ok := h.permSnapshot(ch)
	if !ok || snap == nil || !snap.IsStage(ch) {
		return false, nil
	}
	key := voiceKey{user, ch}
	h.stage.setShown(key, h.mayTalkOnStage(snap, key))
	return h.stage.view(key)
}

// refreshStageSeat recomputes a seat's talk power, announces a change to the
// Talk and narrows or widens the media the member's connections may send.
func (h *Hub) refreshStageSeat(snap *perm.Snapshot, key voiceKey) {
	power := h.mayTalkOnStage(snap, key)
	if power {
		h.lowerHand(key)
	}
	if !h.stage.setShown(key, power) {
		return
	}
	h.sendToVoiceRoom(key.ch, events.StageTalkPower, map[string]any{"channel_id": key.ch, "user_id": key.user, "granted": power})
	for _, c := range h.clientsOf(key.user) {
		h.applyMediaPolicy(c, snap)
	}
}

func (h *Hub) lowerHand(key voiceKey) {
	if _, changed := h.stage.setHand(key, false, time.Time{}); changed {
		h.sendToVoiceRoom(key.ch, events.StageHand, map[string]any{"channel_id": key.ch, "user_id": key.user, "raised": false})
	}
}

// sweepStages brings the Stage state in line with the permission state after
// a change: a Talk that is no Stage any more forgets its talk power and
// hands (telling the Talk), and the seats of Stages are recomputed (Manage
// stage may have been given or taken). want limits the recomputing to some
// users (nil: all).
func (h *Hub) sweepStages(snap *perm.Snapshot, want map[uuid.UUID]bool) {
	if snap == nil {
		return
	}
	for _, key := range h.stage.seats() {
		if !snap.IsStage(key.ch) {
			h.endStageSeat(key)
		}
	}
	h.mu.RLock()
	var seats []voiceKey
	for ch, users := range h.voice {
		if !snap.IsStage(ch) {
			continue
		}
		for id := range users {
			if want == nil || want[id] {
				seats = append(seats, voiceKey{id, ch})
			}
		}
	}
	h.mu.RUnlock()
	for _, key := range seats {
		h.refreshStageSeat(snap, key)
	}
}

// endStageSeat clears a seat whose Talk is no Stage and tells the Talk what
// was lifted.
func (h *Hub) endStageSeat(key voiceKey) {
	power, hand := h.stage.view(key)
	granted := h.stage.granted(key)
	h.stage.forget(key)
	if hand != nil {
		h.sendToVoiceRoom(key.ch, events.StageHand, map[string]any{"channel_id": key.ch, "user_id": key.user, "raised": false})
	}
	if power || granted {
		h.sendToVoiceRoom(key.ch, events.StageTalkPower, map[string]any{"channel_id": key.ch, "user_id": key.user, "granted": false})
	}
}

// stageContext is the Stage a command is about: the Talk c is in, if it is a
// Stage, and the permission state that says so.
func (c *Client) stageContext() (ch uuid.UUID, snap *perm.Snapshot, ok bool) {
	cur := c.currentVoice()
	if cur == nil || !c.hub.stageGate.allow(c.User.ID, time.Now(), stageActionsPer, stageActionWindow) {
		return uuid.Nil, nil, false
	}
	snap, ok = c.hub.permSnapshot(*cur)
	if !ok || snap == nil || !snap.IsStage(*cur) {
		return uuid.Nil, nil, false
	}
	return *cur, snap, true
}

// handleStageHand raises or lowers the member's own hand, or (with Manage
// stage) dismisses somebody else's.
func (c *Client) handleStageHand(payload json.RawMessage) {
	var in struct {
		UserID uuid.UUID `json:"user_id"`
		Raised bool      `json:"raised"`
	}
	if json.Unmarshal(payload, &in) != nil {
		return
	}
	ch, snap, ok := c.stageContext()
	if !ok {
		return
	}
	h := c.hub
	target := in.UserID
	if target == uuid.Nil {
		target = c.User.ID
	}
	key := voiceKey{target, ch}
	switch {
	case target == c.User.ID && in.Raised:
		// Whoever may not speak at all, or already may on this Stage, has
		// nothing to ask for.
		if !c.can(snap, ch, perm.Speak) || h.mayTalkOnStage(snap, key) {
			return
		}
	case target != c.User.ID:
		if in.Raised || !c.can(snap, ch, perm.ManageStage) {
			return
		}
	}
	if !h.inRoom(ch, target) {
		return
	}
	at, changed := h.stage.setHand(key, in.Raised, time.Now())
	if !changed {
		return
	}
	msg := map[string]any{"channel_id": ch, "user_id": target, "raised": in.Raised}
	if in.Raised {
		msg["at"] = at
	}
	h.sendToVoiceRoom(ch, events.StageHand, msg)
}

// handleStageTalkPower gives or takes a member's talk power (Manage stage).
func (c *Client) handleStageTalkPower(payload json.RawMessage) {
	var in struct {
		UserID  uuid.UUID `json:"user_id"`
		Granted bool      `json:"granted"`
	}
	if json.Unmarshal(payload, &in) != nil || in.UserID == uuid.Nil {
		return
	}
	ch, snap, ok := c.stageContext()
	if !ok || !c.can(snap, ch, perm.ManageStage) || !c.hub.inRoom(ch, in.UserID) {
		return
	}
	key := voiceKey{in.UserID, ch}
	c.hub.stage.setGrant(key, in.Granted)
	c.hub.refreshStageSeat(snap, key)
}
