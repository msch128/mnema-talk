package ws

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/sfu"
)

// permTimeout bounds one permission-state load made on behalf of an event.
const permTimeout = 3 * time.Second

// subject is who c is for permission questions.
func (c *Client) subject() perm.Subject {
	return perm.Subject{ID: c.User.ID, Owner: c.User.IsAdmin()}
}

// permSnapshot returns the permission state for an event about channel ch
// (uuid.Nil: not about one channel). It must be called without h.mu held: a
// stale snapshot reloads from the database. A hub without Perms (tests that
// do not exercise permissions) imposes no limits (nil, true); a load failure
// fails closed (nil, false): nobody receives the event.
func (h *Hub) permSnapshot(ch uuid.UUID) (*perm.Snapshot, bool) {
	if h.Perms == nil {
		return nil, true
	}
	ctx, cancel := context.WithTimeout(context.Background(), permTimeout)
	defer cancel()
	var (
		snap *perm.Snapshot
		err  error
	)
	if ch == uuid.Nil {
		snap, err = h.Perms.Snapshot(ctx)
	} else {
		snap, err = h.Perms.ForChannel(ctx, ch)
	}
	if err != nil {
		slog.Warn("permissions unavailable, dropping channel event", "channel", ch, "err", err)
		return nil, false
	}
	return snap, true
}

// can reports whether c holds need in channel ch under snap (nil snap: no
// permission model, everything is allowed).
func (c *Client) can(snap *perm.Snapshot, ch uuid.UUID, need perm.Set) bool {
	return snap == nil || snap.Channel(c.subject(), ch).Has(need)
}

// BroadcastChannel sends an event to every connection whose user may view the
// channel.
func (h *Hub) BroadcastChannel(channelID uuid.UUID, eventType string, payload any) {
	data := encode(eventType, payload)
	if data == nil {
		return
	}
	snap, ok := h.permSnapshot(channelID)
	if !ok {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		if c.can(snap, channelID, perm.ViewChannel) {
			c.deliver(data)
		}
	}
}

// mediaDenied is the sources c may not publish in channel ch.
func (c *Client) mediaDenied(snap *perm.Snapshot, ch uuid.UUID) sfu.SourceSet {
	if snap == nil {
		return 0
	}
	p := snap.Channel(c.subject(), ch)
	var denied sfu.SourceSet
	if !p.Has(perm.Speak) {
		denied = denied.With(sfu.SourceAudio)
	}
	if !p.Has(perm.Video) {
		denied = denied.With(sfu.SourceScreen).With(sfu.SourceScreenAudio).With(sfu.SourceCamera)
	}
	return denied
}

// applyMediaPolicy sets which sources c's media peer may publish, from the
// current permissions, and unpublishes whatever it may no longer send. Used
// after joining and whenever permissions change.
func (h *Hub) applyMediaPolicy(c *Client, snap *perm.Snapshot) {
	cur := c.currentVoice()
	if cur == nil {
		return
	}
	peer := c.peer()
	if peer == nil {
		return
	}
	denied := c.mediaDenied(snap, *cur)
	peer.SetDeniedSources(denied)
	if h.SFU == nil {
		return
	}
	if room := h.SFU.Room(*cur); room != nil {
		for _, src := range []sfu.Source{sfu.SourceAudio, sfu.SourceScreen, sfu.SourceCamera} {
			if denied.Has(src) {
				room.RevokeUserSource(c.User.ID, src)
			}
		}
	}
}

// PermissionsChanged re-checks live connections after a role, assignment,
// overwrite or channel change (perm.Notifier). Everyone concerned is told to
// refresh (permissions_changed: channels, permissions and roles), a member who may no longer connect to or view their Talk is
// removed from it at once, and the media they may still send is narrowed to
// what they may. nil userIDs means everybody.
func (h *Hub) PermissionsChanged(userIDs []uuid.UUID) {
	snap, ok := h.permSnapshot(uuid.Nil)
	if !ok {
		return
	}
	var want map[uuid.UUID]bool
	if userIDs != nil {
		want = make(map[uuid.UUID]bool, len(userIDs))
		for _, id := range userIDs {
			want[id] = true
		}
	}
	h.mu.RLock()
	clients := make([]*Client, 0, len(h.clients))
	for c := range h.clients {
		if want == nil || want[c.User.ID] {
			clients = append(clients, c)
		}
	}
	h.mu.RUnlock()
	for _, c := range clients {
		c.SendEvent("permissions_changed", nil)
	}
	if h.Perms == nil {
		return
	}
	h.enforceVoiceAccess(snap, want)
}

// enforceVoiceAccess removes every member of a Talk (present or lingering in
// the grace period) who may no longer connect, and re-applies the media
// policy to those who stay. want limits it to some users (nil: all).
func (h *Hub) enforceVoiceAccess(snap *perm.Snapshot, want map[uuid.UUID]bool) {
	type seat struct{ user, ch uuid.UUID }
	h.mu.RLock()
	var seats []seat
	for ch, users := range h.voice {
		for id := range users {
			if want == nil || want[id] {
				seats = append(seats, seat{id, ch})
			}
		}
	}
	h.mu.RUnlock()
	for _, s := range seats {
		owner := snap.IsOwner(perm.Subject{ID: s.user})
		p := snap.Channel(perm.Subject{ID: s.user, Owner: owner}, s.ch)
		if p.Has(perm.ViewChannel | perm.Connect) {
			continue
		}
		h.kickUserFromChannel(s.user, s.ch, "missing_permission")
	}
	for _, c := range h.clientsMatching(want) {
		h.applyMediaPolicy(c, snap)
	}
}

// clientsMatching lists the connections of the wanted users (nil: all).
func (h *Hub) clientsMatching(want map[uuid.UUID]bool) []*Client {
	h.mu.RLock()
	defer h.mu.RUnlock()
	var out []*Client
	for c := range h.clients {
		if want == nil || want[c.User.ID] {
			out = append(out, c)
		}
	}
	return out
}

// kickUserFromChannel ends userID's presence in one Talk on every connection
// and in a pending grace period, telling the connections why.
func (h *Hub) kickUserFromChannel(userID, chID uuid.UUID, reason string) {
	for _, c := range h.clientsOf(userID) {
		h.kickClientReason(c, &chID, reason)
	}
	if g := h.takeGrace(voiceKey{userID, chID}); g != nil {
		h.removePresence(userID, chID)
		return
	}
	// Still listed (a second tab raced the kick): make sure the seat is gone.
	h.removePresenceUnlessPresent(userID, chID, nil)
}

// allowedChannels filters the entries of a channel-keyed collection to the
// ones c may view.
func visibleTo[V any](c *Client, snap *perm.Snapshot, in map[uuid.UUID]V) map[uuid.UUID]V {
	if snap == nil {
		return in
	}
	out := make(map[uuid.UUID]V, len(in))
	for ch, v := range in {
		if c.can(snap, ch, perm.ViewChannel) {
			out[ch] = v
		}
	}
	return out
}

// deniedSnapshot stands in when the permission state cannot be loaded.
var deniedSnapshot = perm.EmptySnapshot()
