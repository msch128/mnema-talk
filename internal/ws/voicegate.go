package ws

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
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
	secs      int64
	channel   uuid.UUID
	lastWrite time.Time
	timer     *time.Timer
}

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
// the held time of the previous one.
func (h *Hub) addVoiceTime(userID, chID uuid.UUID, d time.Duration) {
	secs := int64(d / time.Second)
	if h.DB == nil || secs <= 0 {
		return
	}
	h.flushConflictingVoiceTime(userID, chID)
	if h.recordPendingVoiceTime(userID, chID, secs) {
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

func (h *Hub) recordPendingVoiceTime(userID, chID uuid.UUID, secs int64) bool {
	h.vtimes.mu.Lock()
	defer h.vtimes.mu.Unlock()
	p := h.getOrCreatePendingVoiceTime(userID)
	p.secs += secs
	p.channel = chID
	now := time.Now()
	if wait := voiceTimeFlushEvery - now.Sub(p.lastWrite); wait > 0 {
		if p.timer == nil {
			p.timer = time.AfterFunc(wait, func() { h.flushVoiceTime(userID) })
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
	secs, chID := p.secs, p.channel
	p.secs, p.lastWrite = 0, time.Now()
	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}
	h.vtimes.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var total int64
	err := h.DB.QueryRow(ctx, `UPDATE users SET voice_seconds = voice_seconds + $1 WHERE id = $2 RETURNING voice_seconds`,
		secs, userID).Scan(&total)
	if err != nil {
		slog.Warn("record voice time", "user", userID, "err", err)
		return
	}
	// A total that just grew by a stay in a Talk reveals that stay, so only
	// those who can view that Talk learn it.
	h.BroadcastChannel(chID, "user_stats", map[string]any{"user_id": userID, "voice_seconds": total})
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
