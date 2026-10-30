package ws

import (
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/pion/webrtc/v4"
)

// joinVoice puts c into ch's voice room. Re-joining the same room keeps the
// presence entry but renegotiates the SFU peer, which is how a client attaches
// its microphone once getUserMedia resolves.
//
// Another connection of the same user in the same room is told
// "joined_elsewhere"; one in a different room keeps its seat.
func (h *Hub) joinVoice(c *Client, ch *chat.ChannelInfo) {
	h.seatVoice(c, ch, joinMode{evictReason: kickJoinedElsewhere})
}

// resumeVoice is the join a client sends after its WebSocket reconnected, to
// resume the call it was in. When a takeover ended that call for the user and
// another connection of theirs holds voice, the resume is refused with
// "voice_taken_over" instead of taking the voice back. A resume never fails
// because of the user's own stale socket (a network flap): only a takeover
// leaves the mark that refuses it.
func (h *Hub) resumeVoice(c *Client, ch *chat.ChannelInfo) {
	h.seatVoice(c, ch, joinMode{evictReason: kickJoinedElsewhere, resume: true})
}

// joinMode says how a join treats the user's other connections.
type joinMode struct {
	// evictReason is what the same room's other connections are told.
	evictReason string
	// resume marks the rejoin of a reconnecting client (see resumeVoice).
	resume bool
}

// takeOverVoice is a join the user confirmed to move their voice here: once
// c is seated, every other connection of the user leaves voice (in this room
// or any other) with "voice_taken_over". Their sockets stay up.
func (h *Hub) takeOverVoice(c *Client, ch *chat.ChannelInfo) {
	// Without c.voiceMu: the others' voiceMu is taken one at a time below,
	// and two takeovers that hold their own would wait on each other.
	if h.seatVoice(c, ch, joinMode{evictReason: kickTakenOver}) {
		h.endSiblingVoices(c)
	}
}

// The voice_kicked reasons of a connection that gave its voice to another.
const (
	kickJoinedElsewhere = "joined_elsewhere"
	kickTakenOver       = "voice_taken_over"
)

// seatVoice is the join behind joinVoice, takeOverVoice and resumeVoice. It
// reports whether c ended up seated.
func (h *Hub) seatVoice(c *Client, ch *chat.ChannelInfo, mode joinMode) bool {
	c.voiceMu.Lock()
	defer c.voiceMu.Unlock()
	if c.closed.Load() {
		return false
	}
	if !h.checkJoinPermission(c, ch.ID) {
		return false
	}
	rejoin, done := h.leaveOtherRooms(c, ch.ID)
	if done {
		return true
	}
	if mode.resume && !rejoin && h.taken.has(voiceKey{c.User.ID, ch.ID}, c.tabID()) && h.voiceHeldElsewhere(c) {
		c.SendEvent(events.VoiceKicked, map[string]any{"channel_id": ch.ID, "reason": kickTakenOver})
		return false
	}
	seat, ok := h.takeSeat(c, ch, rejoin)
	if !ok {
		if rejoin {
			h.restoreGraceIfAbandoned(c, ch.ID)
		}
		return false
	}
	id := ch.ID
	c.setVoice(&id)
	seat.joined.TalkPower, seat.joined.HandRaisedAt = h.seatStage(c.User.ID, id)

	// Before the seat is final: the eviction it does may mark the room again.
	if !mode.resume {
		h.taken.clear(voiceKey{c.User.ID, ch.ID})
	}
	if !h.finalizeVoiceSeat(c, ch.ID, mode.evictReason) {
		return false
	}
	if c.seatStamp() == 0 {
		c.stampSeat(h.seatClock.Add(1)) // no SFU: the seat is live now
	}
	slog.Info("voice join", "user", c.User.Username, "channel", ch.ID, "rejoin", seat.rejoin)
	h.announceVoiceJoin(c, ch.ID, seat)
	return true
}

// voiceHeldElsewhere reports whether another connection of c's user is seated
// in a voice room.
func (h *Hub) voiceHeldElsewhere(c *Client) bool {
	for _, o := range h.clientsOf(c.User.ID) {
		if o != c && o.currentVoice() != nil && o.seatStamp() != 0 {
			return true
		}
	}
	return false
}

// endSiblingVoices ends the voice of c's user on their other connections that
// were seated before c, in whatever room: they are told, their presence there
// ends, and nothing else of them (sockets, text) is touched. A sibling still
// joining has no seat yet and is left alone; when it finishes it is newer and
// takes over from c itself, so of two takeovers at once the later wins.
func (h *Hub) endSiblingVoices(c *Client) {
	mine := c.seatStamp()
	for _, o := range h.clientsOf(c.User.ID) {
		if o == c {
			continue
		}
		o.voiceMu.Lock()
		if cur := o.currentVoice(); cur != nil && o.seatStamp() != 0 && o.seatStamp() < mine {
			h.taken.mark(voiceKey{o.User.ID, *cur}, o.tabID())
			h.leaveVoiceLocked(o, *cur)
			o.SendEvent(events.VoiceKicked, map[string]any{"channel_id": *cur, "reason": kickTakenOver})
		}
		o.voiceMu.Unlock()
	}
}

func (h *Hub) checkJoinPermission(c *Client, chID uuid.UUID) bool {
	snap, ok := h.permSnapshot(chID)
	if !ok || !c.can(snap, chID, perm.ViewChannel|perm.Connect) {
		slog.Info("voice join refused, missing permission", "user", c.User.Username, "channel", chID)
		c.SendEvent(events.VoiceKicked, map[string]any{"channel_id": chID, "reason": c.joinRefusal(snap)})
		return false
	}
	return true
}

func (h *Hub) finalizeVoiceSeat(c *Client, chID uuid.UUID, evictReason string) bool {
	if h.afterSeat != nil {
		h.afterSeat() // tests change permissions exactly here
	}
	// A permission change may have run its sweep before this seat existed:
	// check again, on the state as it is now, now that the seat can be found.
	if _, ok := h.recheckJoin(c, chID); !ok {
		return false
	}
	if h.SFU != nil && !h.attachPeer(c, chID, evictReason) {
		return false
	}
	return true
}

func (h *Hub) announceVoiceJoin(c *Client, chID uuid.UUID, seat voiceSeat) {
	if seat.rejoin {
		return
	}
	if h.Presence != nil {
		h.Presence(c.User.ID, chID, true)
	}
	c.emitApplicationEvent(events.VoiceStateUpdate, map[string]any{
		"action":     "join",
		"channel_id": chID,
		"user":       seat.joined,
		"started_at": seat.roomStarted,
	}, false, nil, chID)
}

// leaveOtherRooms settles what c's earlier voice presence means for a join of
// chID. A user returning within the grace period resumes silently in the same
// room; joining a different room ends the lingering presence first. rejoin
// reports that c is already in chID; done that its media connection is live
// too, so there is nothing to do.
func (h *Hub) leaveOtherRooms(c *Client, chID uuid.UUID) (rejoin, done bool) {
	for _, g := range h.takeUserGraces(c.User.ID) {
		if g.channelID == chID {
			rejoin = true
		} else {
			h.removePresenceUnlessPresent(c.User.ID, g.channelID, c)
		}
	}
	cur := c.currentVoice()
	if cur == nil {
		return rejoin, false
	}
	if *cur != chID {
		h.leaveVoiceLocked(c, *cur)
		return rejoin, false
	}
	// Already in this room with a live media connection: nothing to
	// renegotiate. (A client that wants a fresh connection leaves
	// first, or reconnects its socket.)
	return true, h.SFU != nil && c.peer().Healthy()
}

// restoreGraceIfAbandoned runs when a join that resumed a lingering presence
// was refused after the grace timer was already cancelled: with no connection
// in the room and no timer left, nothing would ever end that presence.
func (h *Hub) restoreGraceIfAbandoned(c *Client, chID uuid.UUID) {
	if h.inRoom(chID, c.User.ID) && !h.userInVoice(c.User.ID, chID, nil) {
		h.startGrace(voiceKey{c.User.ID, chID})
	}
}

// voiceSeat is what a join leaves behind for its announcement.
type voiceSeat struct {
	rejoin      bool
	joined      VoiceUser
	roomStarted time.Time
}

// takeSeat records c in ch's room. It refuses (telling the client) when the
// hub is closing or the room is full.
func (h *Hub) takeSeat(c *Client, ch *chat.ChannelInfo, rejoin bool) (voiceSeat, bool) {
	key := voiceKey{c.User.ID, ch.ID}
	bypass := h.mayExceedLimit(c, ch) // before h.mu: it asks the permission cache
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return voiceSeat{}, false
	}
	if !bypass && h.isVoiceRoomFull(ch, c.User.ID) {
		slog.Info("voice join refused, room full", "user", c.User.Username, "channel", ch.ID, "limit", ch.UserLimit)
		// The client ends the call on voice_kicked; reason tells it why.
		c.SendEvent(events.VoiceKicked, map[string]any{"channel_id": ch.ID, "reason": "room_full"})
		return voiceSeat{}, false
	}
	now := time.Now()
	h.initVoiceRoomLocked(ch.ID, now)
	_, present := h.voice[ch.ID][c.User.ID]
	rejoin = rejoin || present
	h.recordVoiceJoinTimeLocked(key, rejoin, now)

	profile := h.profileLocked(c)
	h.voice[ch.ID][c.User.ID] = profile
	seat := voiceSeat{
		rejoin:      rejoin,
		joined:      VoiceUser{User: profile, JoinedAt: h.voiceSince[key], MuteState: h.voiceMute[key]},
		roomStarted: h.roomSince[ch.ID],
	}
	return seat, true
}

// mayExceedLimit reports whether c may take a seat in ch beyond its user
// limit: Disconnect members in that Talk (Administrators and the Owner hold
// every permission). Only asked for a Talk that has a limit.
func (h *Hub) mayExceedLimit(c *Client, ch *chat.ChannelInfo) bool {
	if ch.UserLimit <= 0 {
		return false
	}
	snap, ok := h.permSnapshot(ch.ID)
	return ok && snap != nil && c.can(snap, ch.ID, perm.DisconnectMembers)
}

func (h *Hub) isVoiceRoomFull(ch *chat.ChannelInfo, userID uuid.UUID) bool {
	if ch.UserLimit <= 0 {
		return false
	}
	_, present := h.voice[ch.ID][userID]
	if present {
		return false
	}
	return len(h.voice[ch.ID]) >= ch.UserLimit
}

func (h *Hub) initVoiceRoomLocked(chID uuid.UUID, now time.Time) {
	if h.voice[chID] == nil {
		h.voice[chID] = map[uuid.UUID]auth.User{}
	}
	if _, ok := h.roomSince[chID]; !ok {
		h.roomSince[chID] = now
	}
}

func (h *Hub) recordVoiceJoinTimeLocked(key voiceKey, rejoin bool, now time.Time) {
	if _, ok := h.voiceSince[key]; !ok || !rejoin {
		h.voiceSince[key] = now
	}
}

// recheckJoin decides again, on the permission state as it is now, whether c
// may be in chID. If not, the seat is given up and the client told.
func (h *Hub) recheckJoin(c *Client, chID uuid.UUID) (*perm.Snapshot, bool) {
	snap, ok := h.permSnapshot(chID)
	if !ok || !c.can(snap, chID, perm.ViewChannel|perm.Connect) {
		slog.Info("voice join revoked, permission changed meanwhile", "user", c.User.Username, "channel", chID)
		h.leaveVoiceLocked(c, chID)
		c.SendEvent(events.VoiceKicked, map[string]any{"channel_id": chID, "reason": c.joinRefusal(snap)})
		return nil, false
	}
	return snap, true
}

// evictSiblingConnections takes the media of chID's room away from the other
// connections of c's user (a second tab or device): the SFU keeps one peer per
// user and room, so the join replaces theirs. They are told instead of
// being left on a dead connection. Presence stays; c now holds the seat.
// Their voiceMu is not taken: two tabs joining at once would wait on each
// other, and detaching only touches the connection's own state. A sibling
// that was seated after c (or is still joining, without a seat) stays: of two
// joins at once the later one wins, whichever evicts first.
func (h *Hub) evictSiblingConnections(c *Client, chID uuid.UUID, reason string) {
	mine := c.seatStamp()
	for _, o := range h.clientsOf(c.User.ID) {
		if o == c {
			continue
		}
		if cur := o.currentVoice(); cur != nil && *cur == chID && o.seatStamp() < mine {
			if reason == kickTakenOver {
				h.taken.mark(voiceKey{o.User.ID, chID}, o.tabID())
			}
			o.setVoice(nil)
			o.SendEvent(events.VoiceKicked, map[string]any{"channel_id": chID, "reason": reason})
		}
	}
}

// attachPeer joins c to the SFU room and brings it up to date. It reports
// false when the permission check after the join revoked the seat.
func (h *Hub) attachPeer(c *Client, chID uuid.UUID, evictReason string) bool {
	sendOffer := func(offer webrtc.SessionDescription) { c.SendEvent(events.WebrtcOffer, offer) }
	sendICE := func(cand *webrtc.ICECandidateInit) { c.SendEvent(events.WebrtcCandidate, cand) }
	_, peer, err := h.SFU.Join(chID, c.User.ID, sendOffer, sendICE)
	if err != nil {
		// Without a media connection the seat is worth nothing: give it up
		// instead of announcing a member nobody can hear.
		slog.Error("sfu join failed", "user", c.User.ID, "channel", chID, "err", err)
		h.leaveVoiceLocked(c, chID)
		c.SendEvent(events.VoiceKicked, map[string]any{"channel_id": chID, "reason": "media_unavailable"})
		return false
	}
	// Only now: the join replaced the siblings' peers. After a failed join
	// they still own theirs, and the other tabs keep their media.
	c.stampSeat(h.seatClock.Add(1))
	h.evictSiblingConnections(c, chID, evictReason)
	c.setPeer(peer)
	if peer != nil {
		// A media connection that fails after the join leaves the seat
		// without media: tell the client so it can rejoin.
		peer.OnLost(func() { h.kickLostPeer(c, peer) })
	}
	if h.afterPeer != nil {
		h.afterPeer() // tests change permissions exactly here
	}
	// A sweep between the check and now found a seat without a peer and
	// skipped it: decide again on the state as it is now.
	snap, ok := h.recheckJoin(c, chID)
	if !ok {
		return false
	}
	h.applyMediaPolicy(c, snap)
	h.sendRoomState(c, chID)
	return true
}

// sendRoomState tells a late joiner who shares a screen or runs a camera, and
// who watches each share.
func (h *Hub) sendRoomState(c *Client, chID uuid.UUID) {
	room := h.SFU.Room(chID)
	if room == nil {
		return
	}
	for uid, st := range room.MediaStates() {
		if uid != c.User.ID {
			c.SendEvent(events.WebrtcMediaState, mediaStatePayload(chID, uid, st))
		}
	}
	for sharer, viewers := range room.ScreenViewers() {
		c.SendEvent(events.ScreenViewers, screenViewersPayload(chID, sharer, viewers))
	}
	for sharer, layers := range room.ScreenLayers() {
		c.SendEvent(events.ScreenLayers, screenLayersPayload(chID, sharer, layers))
	}
}

// joinRefusal is the voice_kicked reason of a refused join: the rules the
// member has not accepted yet, otherwise a missing permission.
func (c *Client) joinRefusal(snap *perm.Snapshot) string {
	if snap != nil && snap.RulesPending(c.User.ID) {
		return "rules_not_accepted"
	}
	return "missing_permission"
}

// takeoverMarks remembers which page's stay a takeover ended, so that page,
// reconnecting later and resuming the call, is told instead of taking it back.
// The page that took over is not affected: its resume carries another tab id.
// A mark ends when the user joins that room on purpose, or after an hour.
type takeoverMarks struct {
	mu sync.Mutex
	at map[voiceKey]takeoverMark
}

type takeoverMark struct {
	tab string
	at  time.Time
}

const takeoverMarkTTL = time.Hour

func (m *takeoverMarks) mark(k voiceKey, tab string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if m.at == nil {
		m.at = map[voiceKey]takeoverMark{}
	}
	for old, e := range m.at {
		if now.Sub(e.at) > takeoverMarkTTL {
			delete(m.at, old)
		}
	}
	m.at[k] = takeoverMark{tab: tab, at: now}
}

func (m *takeoverMarks) clear(k voiceKey) {
	m.mu.Lock()
	delete(m.at, k)
	m.mu.Unlock()
}

// has reports whether a takeover ended the stay of the page with this tab id.
// Pages without an id (older clients) are never refused.
func (m *takeoverMarks) has(k voiceKey, tab string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.at[k]
	return ok && tab != "" && e.tab == tab && time.Since(e.at) <= takeoverMarkTTL
}
