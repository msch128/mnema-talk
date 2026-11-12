package ws

import (
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/safego"
)

// enforceFailClosed runs the enforcement fn for userIDs (nil: everybody). The
// change that asked for it is committed already and nothing else re-checks
// live connections, so a panic part-way must not leave members with a seat or
// a right the change took away. The panic is counted and logged, then the
// members concerned lose their connections and seats, which are checked again
// when they reconnect and rejoin. The caller goes on: the request that made
// the change is not turned into an error after it was saved. A panic in the
// fail-closed clean-up itself is not recovered here.
func (h *Hub) enforceFailClosed(userIDs []uuid.UUID, fn func()) {
	safego.Run("ws_perm_enforce", func() { h.disconnectForRecheck(userIDs) }, fn)
}

// disconnectForRecheck closes every connection of userIDs (nil: everybody)
// and ends their Talk seats, so nobody keeps what an interrupted enforcement
// did not get to take away. Locks are released by safego.Locked and the
// deferred unlocks of the paths below, so the hub is usable afterwards.
func (h *Hub) disconnectForRecheck(userIDs []uuid.UUID) {
	want := wantUserIDs(userIDs)
	users := map[uuid.UUID]struct{}{}
	for _, c := range h.clientsMatching(want) {
		users[c.User.ID] = struct{}{}
	}
	safego.Locked(h.mu.RLocker(), func() {
		for _, room := range h.voice {
			for id := range room {
				if want == nil || want[id] {
					users[id] = struct{}{}
				}
			}
		}
		for key := range h.grace {
			if want == nil || want[key.user] {
				users[key.user] = struct{}{}
			}
		}
	})
	for id := range users {
		h.DisconnectUser(id)
	}
	for id := range users {
		// One member's failing seat clean-up must not spare the next one.
		safego.Run("ws_perm_enforce_seat", safego.Abandon, func() { h.KickFromVoice(id) })
	}
}
