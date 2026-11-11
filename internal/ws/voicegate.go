package ws

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/safego"
)

const (
	// voiceChurnRate and voiceChurnBurst are a user's voice join/leave
	// allowance: a join builds a media connection and a leave broadcasts, so
	// churning them is an expensive way to load the server. A person joining,
	// leaving and joining again stays far below it.
	voiceChurnRate  = 1.0 // tokens per second
	voiceChurnBurst = 3.0

	// voiceTimeFlushEvery spaces the writes of one user's voice time to the
	// database; stays that end sooner are added up and written together.
	voiceTimeFlushEvery = 10 * time.Second
)

type churnBucket struct {
	tokens float64
	last   time.Time
}

// voiceGate holds the per-user token buckets of voice join/leave churn.
type voiceGate struct {
	mu sync.Mutex
	m  map[uuid.UUID]*churnBucket
}

// take spends one token of userID and reports whether one was left.
func (g *voiceGate) take(userID uuid.UUID, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.m == nil {
		g.m = map[uuid.UUID]*churnBucket{}
	}
	b := g.m[userID]
	if b == nil {
		b = &churnBucket{tokens: voiceChurnBurst, last: now}
		g.m[userID] = b
	}
	b.tokens = min(voiceChurnBurst, b.tokens+now.Sub(b.last).Seconds()*voiceChurnRate)
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// forget drops the bucket of a user who went offline.
func (g *voiceGate) forget(userID uuid.UUID) {
	g.mu.Lock()
	delete(g.m, userID)
	g.mu.Unlock()
}

// pendingVoiceTime is voice time of one user not written yet.
type pendingVoiceTime struct {
	secs int64
	// public is the part of secs that counts in the public total.
	public    int64
	channel   uuid.UUID
	lastWrite time.Time
	timer     *time.Timer
	// idle drops the entry once it has held nothing for a while.
	idle *time.Timer
}

// seatClass says how the time of a stay in a Talk counts.
type seatClass uint8

const (
	// seatPublic: the Talk is visible to @everyone; the time counts in the
	// public total (and the private one).
	seatPublic seatClass = iota
	// seatLimited: some members cannot see the Talk; the time counts in the
	// private total only, so the public one never reveals a hidden Talk.
	seatLimited
	// seatPrivate: the call of a conversation (or a Talk whose state could
	// not be read); never counted.
	seatPrivate
)

// voiceTimes debounces the writes of voice_seconds.
type voiceTimes struct {
	mu sync.Mutex
	m  map[uuid.UUID]*pendingVoiceTime
}

// addVoiceTime adds one finished stay in a voice room to the user's total and
// tells those who can see the room the new total. Writes of one user are
// spaced voiceTimeFlushEvery apart: a stay ending sooner after the last write
// waits for a timer (or the hub's shutdown) and is written together with
// whatever follows in the same Talk; a stay in a different Talk first writes
// the held time of the previous one. class says how the stay counts (decided
// when the seat was taken): a conversation's call never does, a Talk some
// members cannot see only in the private total.
func (h *Hub) addVoiceTime(userID, chID uuid.UUID, d time.Duration, class seatClass) {
	secs := int64(d / time.Second)
	// Time in the call of a conversation stays private: the total is public.
	if h.DB == nil || secs <= 0 || class == seatPrivate {
		return
	}
	var public int64
	if class == seatPublic {
		public = secs
	}
	h.flushConflictingVoiceTime(userID, chID)
	if h.recordPendingVoiceTime(userID, chID, secs, public) {
		h.flushVoiceTime(userID)
	}
}

func (h *Hub) flushConflictingVoiceTime(userID, chID uuid.UUID) {
	h.vtimes.mu.Lock()
	p := h.vtimes.m[userID]
	if p != nil && p.secs > 0 && p.channel != chID {
		h.vtimes.mu.Unlock()
		h.flushVoiceTime(userID)
		return
	}
	h.vtimes.mu.Unlock()
}

func (h *Hub) getOrCreatePendingVoiceTime(userID uuid.UUID) *pendingVoiceTime {
	if h.vtimes.m == nil {
		h.vtimes.m = map[uuid.UUID]*pendingVoiceTime{}
	}
	p := h.vtimes.m[userID]
	if p == nil {
		p = &pendingVoiceTime{}
		h.vtimes.m[userID] = p
	}
	return p
}

func (h *Hub) recordPendingVoiceTime(userID, chID uuid.UUID, secs, public int64) bool {
	h.vtimes.mu.Lock()
	defer h.vtimes.mu.Unlock()
	p := h.getOrCreatePendingVoiceTime(userID)
	p.secs += secs
	p.public += public
	p.channel = chID
	if p.idle != nil {
		p.idle.Stop()
		p.idle = nil
	}
	now := time.Now()
	if wait := voiceTimeFlushEvery - now.Sub(p.lastWrite); wait > 0 {
		if p.timer == nil {
			p.timer = safego.AfterFunc("ws_voice_time", wait, safego.Crash, func() { h.flushVoiceTime(userID) })
		}
		return false
	}
	return true
}

// flushVoiceTime writes what is pending for userID.
func (h *Hub) flushVoiceTime(userID uuid.UUID) {
	h.vtimes.mu.Lock()
	p := h.vtimes.m[userID]
	if p == nil || p.secs == 0 {
		h.vtimes.mu.Unlock()
		return
	}
	secs, public, chID := p.secs, p.public, p.channel
	p.secs, p.public, p.lastWrite = 0, 0, time.Now()
	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}
	h.vtimes.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var total int64
	err := h.DB.QueryRow(ctx, `UPDATE users SET voice_seconds = voice_seconds + $1, public_voice_seconds = public_voice_seconds + $3
		WHERE id = $2 RETURNING public_voice_seconds`, secs, userID, public).Scan(&total)
	if err != nil {
		slog.Warn("record voice time", "user", userID, "err", err)
		h.restorePendingVoiceTime(userID, chID, secs, public)
		return
	}
	h.pruneWhenIdle(userID)
	// Only the public total is shown, and it only grew by a stay in a Talk
	// everyone can see; time in other Talks announces nothing.
	if public > 0 {
		h.BroadcastChannel(chID, events.UserStats, map[string]any{"user_id": userID, "voice_seconds": total})
	}
}

// restorePendingVoiceTime puts seconds whose write failed back, so the next
// flush (a retry timer, a later stay or the shutdown) writes them again.
func (h *Hub) restorePendingVoiceTime(userID, chID uuid.UUID, secs, public int64) {
	h.vtimes.mu.Lock()
	defer h.vtimes.mu.Unlock()
	p := h.getOrCreatePendingVoiceTime(userID)
	p.secs += secs
	p.public += public
	p.channel = chID
	if p.idle != nil {
		p.idle.Stop()
		p.idle = nil
	}
	if p.timer == nil {
		p.timer = safego.AfterFunc("ws_voice_time", voiceTimeFlushEvery, safego.Crash, func() { h.flushVoiceTime(userID) })
	}
}

// pruneWhenIdle forgets userID's entry once it held nothing for a debounce
// period (it only has to outlive the write spacing it enforces).
func (h *Hub) pruneWhenIdle(userID uuid.UUID) {
	h.vtimes.mu.Lock()
	defer h.vtimes.mu.Unlock()
	p := h.vtimes.m[userID]
	if p == nil || p.secs != 0 || p.timer != nil {
		return
	}
	if p.idle != nil {
		p.idle.Stop()
	}
	p.idle = safego.AfterFunc("ws_voice_time", voiceTimeFlushEvery, safego.Crash, func() {
		h.vtimes.mu.Lock()
		defer h.vtimes.mu.Unlock()
		if h.vtimes.m[userID] == p && p.secs == 0 && p.timer == nil {
			delete(h.vtimes.m, userID)
		}
	})
}

// flushAllVoiceTime writes every pending total (shutdown, while the
// database is still open).
func (h *Hub) flushAllVoiceTime() {
	h.vtimes.mu.Lock()
	ids := make([]uuid.UUID, 0, len(h.vtimes.m))
	for id := range h.vtimes.m {
		ids = append(ids, id)
	}
	h.vtimes.mu.Unlock()
	for _, id := range ids {
		h.flushVoiceTime(id)
	}
}

// classifySeat decides how a stay in ch counts. When the permission state
// cannot be read the stay is not counted at all: it is kept out of the public
// total rather than leaked into it.
func (h *Hub) classifySeat(ch uuid.UUID) seatClass {
	return h.classifyWith(ch, seatPrivate)
}

// settleSeat decides how a stay that is ending counts: the stricter of how it
// was classed at its start and how the Talk is classed now, so a Talk hidden
// from @everyone mid-stay does not count its whole time publicly. A failed
// lookup now counts as limited: it keeps the time out of the public total
// without discarding a stay that began in the open.
func (h *Hub) settleSeat(ch uuid.UUID, start seatClass) seatClass {
	return max(start, h.classifyWith(ch, seatLimited))
}

func (h *Hub) classifyWith(ch uuid.UUID, onFailure seatClass) seatClass {
	snap, ok := h.permSnapshot(ch)
	switch {
	case !ok:
		return onFailure
	case snap.IsDMTalk(ch):
		return seatPrivate
	case snap.EveryoneCanView(ch):
		return seatPublic
	default:
		return seatLimited
	}
}
