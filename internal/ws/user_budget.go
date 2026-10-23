package ws

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	// maxUserFramesPerSec and maxUserEventsPerSec bound all of one account's
	// browser and native connections together. The per-connection budgets stop
	// one noisy socket; these stop an account from multiplying them by opening
	// several (up to maxBrowserSockets browser sockets plus native ones). They
	// leave room for a few busy tabs, which a person never keeps up.
	maxUserFramesPerSec = 2 * maxFramesPerSec
	maxUserEventsPerSec = 2 * maxEventsPerSec
	// userBudgetSweepEvery spaces the removal of idle accounts' windows.
	userBudgetSweepEvery = time.Minute
)

type userWindow struct {
	start          time.Time
	frames, events int
	handshakes     int
	handshakeBytes int
}

// userBudgets is the per-account, cross-connection flood guard. The
// media_handshake allowance is shared too, so several sockets of one account in
// a room cannot add up to a larger fan-out than one.
type userBudgets struct {
	mu        sync.Mutex
	m         map[uuid.UUID]*userWindow
	lastSweep time.Time
}

// windowLocked returns userID's current one-second window.
func (u *userBudgets) windowLocked(userID uuid.UUID, now time.Time) *userWindow {
	if u.m == nil {
		u.m = map[uuid.UUID]*userWindow{}
	}
	if now.Sub(u.lastSweep) >= userBudgetSweepEvery {
		u.lastSweep = now
		for id, w := range u.m {
			if now.Sub(w.start) >= time.Second {
				delete(u.m, id)
			}
		}
	}
	w := u.m[userID]
	if w == nil || now.Sub(w.start) >= time.Second {
		w = &userWindow{start: now}
		u.m[userID] = w
	}
	return w
}

// allowFrame counts a raw frame of the account against the overall cap.
func (u *userBudgets) allowFrame(userID uuid.UUID, now time.Time) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	w := u.windowLocked(userID, now)
	w.frames++
	return w.frames <= maxUserFramesPerSec
}

// allowEvent counts a parsed event of the account. Signaling is metered per
// connection only: it belongs to one media session.
func (u *userBudgets) allowEvent(userID uuid.UUID, eventType string, payloadLen int, now time.Time) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	w := u.windowLocked(userID, now)
	if eventType == handshakeEvent {
		w.handshakes++
		w.handshakeBytes += payloadLen
		return w.handshakes <= maxHandshakesPerSec && w.handshakeBytes <= maxHandshakeBytesPerSec
	}
	if _, signaling := signalingEvents[eventType]; signaling {
		return true
	}
	w.events++
	return w.events <= maxUserEventsPerSec
}

// forget drops the window of an account that went offline.
func (u *userBudgets) forget(userID uuid.UUID) {
	u.mu.Lock()
	delete(u.m, userID)
	u.mu.Unlock()
}
