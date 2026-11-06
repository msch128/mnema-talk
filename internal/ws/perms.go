package ws

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/events"
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
	dm := snap != nil && snap.IsDM(channelID)
	if dm {
		// A conversation's events go to its members only: never to webhooks
		// or bots, and not to a client that does not know conversations.
		h.dmTapped(channelID, eventType, payload)
	} else {
		h.tapped(channelID, eventType, payload) // only what is really published
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		if (!dm || c.dmAware) && c.can(snap, channelID, perm.ViewChannel) {
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
		// The screen's sound is a voice too: no talking over the share.
		denied = denied.With(sfu.SourceAudio).With(sfu.SourceScreenAudio)
	}
	if !p.Has(perm.Video) {
		denied = denied.With(sfu.SourceScreen).With(sfu.SourceScreenAudio).With(sfu.SourceCamera)
	}
	if snap.IsStage(ch) && !c.hub.mayTalkOnStage(snap, voiceKey{c.User.ID, ch}) {
		// A Stage is quiet without talk power: no sound, no camera, no screen.
		denied = denied.With(sfu.SourceAudio).With(sfu.SourceScreenAudio).With(sfu.SourceScreen).With(sfu.SourceCamera)
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

// permNotifyEvery is the least gap between two permissions_changed events to
// one connection; changes in between are sent once, when the gap is over.
const permNotifyEvery = time.Second

// notifyPermissions tells c to refresh its permissions: at once, unless one
// went out less than permNotifyEvery ago, then once when that time is up.
// The checks themselves (removing members from Talks) are never delayed.
func (c *Client) notifyPermissions() {
	c.mu.Lock()
	wait := permNotifyEvery - time.Since(c.permSent)
	if wait <= 0 {
		c.permSent = time.Now()
		c.mu.Unlock()
		c.SendEvent(events.PermissionsChanged, nil)
		return
	}
	if c.permPending {
		c.mu.Unlock()
		return
	}
	c.permPending = true
	c.mu.Unlock()
	time.AfterFunc(wait, func() {
		c.mu.Lock()
		c.permPending = false
		c.permSent = time.Now()
		c.mu.Unlock()
		c.SendEvent(events.PermissionsChanged, nil)
	})
}

// PermissionsChanged re-checks live connections after a role, assignment,
// overwrite or channel change (perm.Notifier). Everyone concerned is told to
// refresh (permissions_changed: channels, permissions and roles). A member who may no
// longer connect to or view their Talk is removed from it at once, and the media they may still send is narrowed to
// what they may. nil userIDs means everybody.
func (h *Hub) PermissionsChanged(userIDs []uuid.UUID) {
	want := wantUserIDs(userIDs)
	h.mu.RLock()
	clients := filterPermClients(h.clients, want)
	h.mu.RUnlock()
	for _, c := range clients {
		c.notifyPermissions()
	}
	h.enforcePermissions(userIDs)
}

// enforcePermissions removes members from Talks and stages they may no longer
// use. When the permission state cannot be loaded (it was just invalidated),
// the users are remembered and tried again with a growing pause: nothing else
// would re-check them.
func (h *Hub) enforcePermissions(userIDs []uuid.UUID) {
	snap, ok := h.permSnapshot(uuid.Nil)
	if !ok {
		h.permRetry.add(userIDs, h.enforcePendingPermissions)
		return
	}
	h.permRetry.succeeded()
	if h.Perms == nil {
		return
	}
	want := wantUserIDs(userIDs)
	h.sweepStages(snap, want)
	h.enforceVoiceAccess(snap, want)
}

func (h *Hub) enforcePendingPermissions() {
	if h.isClosed() {
		return
	}
	h.enforcePermissions(h.permRetry.take())
}

// permRetry remembers whose permissions still have to be enforced after a
// failed load. The zero value is ready; base is the first pause (1 s).
type permRetry struct {
	mu      sync.Mutex
	users   map[uuid.UUID]bool
	all     bool
	timer   *time.Timer
	attempt int
	base    time.Duration
}

const permRetryMax = time.Minute

func (r *permRetry) add(userIDs []uuid.UUID, run func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if userIDs == nil {
		r.all = true
	} else {
		if r.users == nil {
			r.users = map[uuid.UUID]bool{}
		}
		for _, id := range userIDs {
			r.users[id] = true
		}
	}
	if r.timer != nil {
		return // a retry is already scheduled and will take the merged set
	}
	wait := r.base
	if wait <= 0 {
		wait = time.Second
	}
	wait = min(wait<<r.attempt, permRetryMax)
	r.attempt++
	r.timer = time.AfterFunc(wait, run)
}

// take hands over the pending users (nil: everybody) and lets the next failure
// schedule a new retry.
func (r *permRetry) take() []uuid.UUID {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.timer = nil
	if r.all {
		r.all, r.users = false, nil
		return nil
	}
	out := make([]uuid.UUID, 0, len(r.users))
	for id := range r.users {
		out = append(out, id)
	}
	r.users = nil
	return out
}

func (r *permRetry) succeeded() {
	r.mu.Lock()
	r.attempt = 0
	r.mu.Unlock()
}

func filterPermClients(clients map[*Client]struct{}, want map[uuid.UUID]bool) []*Client {
	out := make([]*Client, 0, len(clients))
	for c := range clients {
		if want == nil || want[c.User.ID] {
			out = append(out, c)
		}
	}
	return out
}

func wantUserIDs(userIDs []uuid.UUID) map[uuid.UUID]bool {
	if userIDs == nil {
		return nil
	}
	want := make(map[uuid.UUID]bool, len(userIDs))
	for _, id := range userIDs {
		want[id] = true
	}
	return want
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
		if holds(snap, s.user, s.ch, perm.ViewChannel|perm.Connect) {
			continue
		}
		reason := "missing_permission"
		if snap.RulesPending(s.user) {
			reason = "rules_not_accepted"
		}
		h.kickUserFromChannel(s.user, s.ch, reason)
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

// VoiceChannels lists the Talks userID is in, including a lingering seat.
func (h *Hub) VoiceChannels(userID uuid.UUID) []uuid.UUID {
	h.mu.RLock()
	defer h.mu.RUnlock()
	var out []uuid.UUID
	for ch, users := range h.voice {
		if _, in := users[userID]; in {
			out = append(out, ch)
		}
	}
	return out
}

// KickFromChannel ends userID's presence in one Talk (the moderator's kick).
func (h *Hub) KickFromChannel(userID, chID uuid.UUID) { h.kickUserFromChannel(userID, chID, "") }

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

// visibleTo filters the entries of a channel-keyed collection to the
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
