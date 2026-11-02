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
	speakMask = Speak
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
		return httpx.ErrForbidden(fmt.Sprintf("new members can write in %d s", seconds(w.Write)))
	case w.React > 0 && miss&reactMask != 0:
		return httpx.ErrForbidden(fmt.Sprintf("new members can react in %d s", seconds(w.React)))
	case w.Speak > 0 && miss&speakMask != 0:
		return httpx.ErrForbidden(fmt.Sprintf("new members can speak in %d s", seconds(w.Speak)))
	}
	return nil
}

// seconds rounds a wait up to whole seconds.
func seconds(d time.Duration) int { return int((d + time.Second - 1) / time.Second) }
