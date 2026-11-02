package perm

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// NewMemberRules is how long after joining a member has to wait before they
// may write, react and speak in Talks (raid protection, doc/moderation.md).
// A zero duration turns that gate off.
type NewMemberRules struct{ Write, React, Speak time.Duration }

// NewMemberWaits is what is left of those waits for one member; zero: allowed.
type NewMemberWaits struct{ Write, React, Speak time.Duration }

// The permissions each gate takes while it holds. Direct messages are exempt
// (they do not pass through the gate), and so are Talks of conversations.
const (
	writeMask = SendMessages | AttachFiles | MentionEveryone
	reactMask = AddReactions
	speakMask = Speak | Video // camera and screen share count as speaking
	// dmCallMask is what the speak wait takes in the call of a conversation.
	dmCallMask = Connect | Speak | Video
)

// mask is what the waits take from the member.
func (w NewMemberWaits) mask() Set {
	var m Set
	if w.Write > 0 {
		m |= writeMask
	}
	if w.React > 0 {
		m |= reactMask
	}
	if w.Speak > 0 {
		m |= speakMask
	}
	return m
}

// NewMemberWaits returns what a member still has to wait. The Owner, bots and
// holders of Administrator or Bypass new member limits never wait. A member
// the snapshot does not know yet counts as having joined when it was loaded.
func (s *Snapshot) NewMemberWaits(userID uuid.UUID) NewMemberWaits {
	r := s.newMember
	if r == (NewMemberRules{}) || s.owners[userID] || s.bots[userID] {
		return NewMemberWaits{}
	}
	joined, ok := s.joined[userID]
	if !ok {
		// Signed up since the snapshot was loaded: counted from the load, at most a snapshot TTL
		// short of the real time, and exact from the next load on.
		joined = s.loaded
	}
	age := clock().Sub(joined)
	w := NewMemberWaits{Write: max(0, r.Write-age), React: max(0, r.React-age), Speak: max(0, r.Speak-age)}
	if w == (NewMemberWaits{}) || s.bypassesNewMemberLimits(userID) {
		return NewMemberWaits{}
	}
	return w
}

// bypassesNewMemberLimits reports whether a role of the member (or @everyone)
// grants Administrator or Bypass new member limits.
func (s *Snapshot) bypassesNewMemberLimits(userID uuid.UUID) bool {
	p := s.everyone
	for _, id := range s.userRoles[userID] {
		p |= s.roles[id].Permissions
	}
	return p.HasAny(Administrator | BypassNewMemberLimits)
}

// hold removes what the member's remaining waits take from p.
func (s *Snapshot) hold(userID uuid.UUID, p Set) Set {
	if s.newMember == (NewMemberRules{}) {
		return p
	}
	return p &^ s.NewMemberWaits(userID).mask()
}

// NewMemberRefusal is the refusal for need when the member's waits are what
// holds it back, or nil when they are not.
func (s *Snapshot) NewMemberRefusal(userID uuid.UUID, need Set) error {
	return s.newMemberRefusal(userID, 0, need)
}

// newMemberRefusal is the refusal for a permission the waits took from the
// member, or nil when the missing permissions have another reason.
func (s *Snapshot) newMemberRefusal(userID uuid.UUID, p, need Set) error {
	w := s.NewMemberWaits(userID)
	miss := need &^ p
	switch {
	case w.Write > 0 && miss&writeMask != 0:
		return NewMemberWriteError(w.Write)
	case w.React > 0 && miss&reactMask != 0:
		return httpx.ErrForbidden(fmt.Sprintf("new members can react in %d s", seconds(w.React)))
	case w.Speak > 0 && miss&speakMask != 0:
		return NewMemberSpeakError(w.Speak)
	}
	return nil
}

// NewMemberWriteError is the refusal for writing (or starting a conversation)
// while the member's write wait runs.
func NewMemberWriteError(wait time.Duration) error {
	return httpx.ErrForbidden(fmt.Sprintf("new members can write in %d s", seconds(wait)))
}

// NewMemberSpeakError is the refusal for speaking (or joining a call) while
// the member's speak wait runs.
func NewMemberSpeakError(wait time.Duration) error {
	return httpx.ErrForbidden(fmt.Sprintf("new members can speak in %d s", seconds(wait)))
}

// DenyIn is Deny for a request about channel ch: joining the call of a
// conversation, which the speak wait holds as a whole, answers with that wait.
func (s *Snapshot) DenyIn(sub Subject, ch uuid.UUID, p, need Set) error {
	err := s.Deny(sub, p, need)
	if _, call := s.dmTalks[ch]; err != nil && call && p.Has(ViewChannel) && need&^p&dmCallMask != 0 {
		if w := s.NewMemberWaits(sub.ID); w.Speak > 0 {
			return NewMemberSpeakError(w.Speak)
		}
	}
	return err
}

// seconds rounds a wait up to whole seconds.
func seconds(d time.Duration) int { return int((d + time.Second - 1) / time.Second) }

// nextWaitEnd is the first moment after now at which a member's wait ends,
// or false when no member is waiting.
func (s *Snapshot) nextWaitEnd(now time.Time) (time.Time, bool) {
	var next time.Time
	for _, joined := range s.joined {
		for _, d := range []time.Duration{s.newMember.Write, s.newMember.React, s.newMember.Speak} {
			if end := joined.Add(d); d > 0 && end.After(now) && (next.IsZero() || end.Before(next)) {
				next = end
			}
		}
	}
	return next, !next.IsZero()
}

// waitsEndedSince lists the members one of whose waits ended in (since, now].
func (s *Snapshot) waitsEndedSince(since, now time.Time) []uuid.UUID {
	var out []uuid.UUID
	for id, joined := range s.joined {
		for _, d := range []time.Duration{s.newMember.Write, s.newMember.React, s.newMember.Speak} {
			if end := joined.Add(d); d > 0 && end.After(since) && !end.After(now) {
				out = append(out, id)
				break
			}
		}
	}
	return out
}
