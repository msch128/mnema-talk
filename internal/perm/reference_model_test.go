package perm_test

import (
	"time"

	"github.com/google/uuid"
	. "github.com/msch128/mnema-talk/internal/perm"
)

// A second, deliberately plain implementation of permission resolution, read
// from the documentation (the Snapshot comment in snapshot.go, snapshot_dm.go,
// newmember.go and the comments on the bits in perm.go) rather than from the
// code. It is slow and has no caches; reference_compare_test.go compares it
// with the Snapshot on generated communities.

// The masks are restated here from their documentation, not imported, so a
// changed constant shows up as a disagreement.
const (
	refTimeout   = SendMessages | AttachFiles | AddReactions | MentionEveryone | Connect | Speak | Video | SendDirectMessages | EditWiki | ManageWiki | CreateMemberInvites
	refWriteWait = SendMessages | AttachFiles | MentionEveryone | EditWiki | CreateMemberInvites
	refReactWait = AddReactions
	refSpeakWait = Speak | Video
	refCallWait  = Connect | Speak | Video // a conversation's call: joining waits too
	refCallChat  = ManageMessages | MentionEveryone
)

type refModel struct {
	st  State
	now time.Time
	// loaded is when the snapshot was taken: a member without a join time
	// counts as having joined then.
	loaded time.Time
}

func (m refModel) owner(u uuid.UUID) bool { return m.st.Owners[u] }
func (m refModel) bot(u uuid.UUID) bool   { return m.st.Bots[u] }

// rolePerms is @everyone (not for bots) plus every role of the member.
func (m refModel) rolePerms(u uuid.UUID, withEveryone bool) Set {
	var p Set
	if withEveryone {
		p = m.st.Everyone
	}
	for _, r := range m.st.UserRoles[u] {
		p |= m.st.Roles[r].Permissions
	}
	return p
}

func (m refModel) timedOut(u uuid.UUID) bool {
	t, ok := m.st.Timeouts[u]
	return ok && t.Until.After(m.now)
}

func (m refModel) rulesPending(u uuid.UUID) bool {
	return m.st.RulesVersion > 0 && !m.owner(u) && !m.bot(u) && m.st.RulesAccepted[u] < m.st.RulesVersion
}

// unheld is the server-wide set before the new-member waits.
func (m refModel) unheld(u uuid.UUID) Set {
	if m.owner(u) {
		return All
	}
	p := m.rolePerms(u, !m.bot(u))
	if p&Administrator != 0 {
		p = All
	}
	if m.timedOut(u) || m.rulesPending(u) {
		p &^= refTimeout
	}
	return p & All
}

// waits are the remaining new-member waits: write, react, speak.
func (m refModel) waits(u uuid.UUID) [3]time.Duration {
	nm := m.st.NewMember
	if nm == (NewMemberRules{}) || m.owner(u) || m.bot(u) {
		return [3]time.Duration{}
	}
	if m.rolePerms(u, true)&(Administrator|BypassNewMemberLimits) != 0 {
		return [3]time.Duration{}
	}
	joined, ok := m.st.Joined[u]
	if !ok {
		joined = m.loaded
	}
	age := m.now.Sub(joined)
	left := func(d time.Duration) time.Duration { return max(0, d-age) }
	return [3]time.Duration{left(nm.Write), left(nm.React), left(nm.Speak)}
}

func (m refModel) hold(u uuid.UUID, p Set) Set {
	w := m.waits(u)
	if w[0] > 0 {
		p &^= refWriteWait
	}
	if w[1] > 0 {
		p &^= refReactWait
	}
	if w[2] > 0 {
		p &^= refSpeakWait
	}
	return p
}

func (m refModel) global(u uuid.UUID) Set { return m.hold(u, m.unheld(u)) }

// overwrite applies one category's or channel's overwrites: @everyone (bots
// do not have it), then the member's roles together, then the member.
func (m refModel) overwrite(sc Scope, ok bool, u uuid.UUID, p Set) Set {
	if !ok {
		return p
	}
	if o, has := sc.Roles[m.st.EveryoneID]; has && !m.bot(u) {
		p = p&^o.Deny | o.Allow
	}
	var allow, deny Set
	for _, r := range m.st.UserRoles[u] {
		if o, has := sc.Roles[r]; has {
			allow |= o.Allow
			deny |= o.Deny
		}
	}
	p = p&^deny | allow
	if o, has := sc.Members[u]; has {
		p = p&^o.Deny | o.Allow
	}
	return p
}

// channel is what u may do in ch.
func (m refModel) channel(u uuid.UUID, ch uuid.UUID) Set {
	if members, isDM := m.st.DMs[ch]; isDM {
		return m.conversation(u, members)
	}
	dm, isCall := m.st.DMTalks[ch]
	if isCall && !m.st.DMs[dm][u] {
		return 0
	}
	if isCall {
		return m.call(u, ch)
	}
	return m.community(u, ch)
}

// call: a conversation's call Talk, for a member of the conversation
// (doc/talks.md, "Start a Talk from a direct message"). Membership decides
// that they see it; nothing of the community's channel rules (View channels,
// category or channel overwrites) grants or takes anything there. What they
// may do is their server-wide set (a timeout and pending rules included), the
// speak wait holds the call as a whole, and nobody but the Owner moderates its
// chat.
func (m refModel) call(u, ch uuid.UUID) Set {
	if m.owner(u) {
		return All
	}
	if _, known := m.st.ChannelCat[ch]; !known {
		return 0
	}
	p := m.unheld(u) | ViewChannel
	if m.waits(u)[2] > 0 {
		p &^= refCallWait
	}
	return p &^ refCallChat
}

// conversation: members read; writing needs Send direct messages and the
// switch, reactions also the react wait.
func (m refModel) conversation(u uuid.UUID, members map[uuid.UUID]bool) Set {
	if !members[u] {
		return 0
	}
	p := ViewChannel
	if !m.st.DMsDisabled && m.unheld(u)&SendDirectMessages != 0 {
		p |= m.unheld(u) & (SendMessages | AttachFiles)
		p |= m.global(u) & AddReactions
	}
	return p
}

// community resolves a channel or Talk that is not a conversation or its call.
func (m refModel) community(u, ch uuid.UUID) Set {
	if m.owner(u) {
		return All
	}
	cat, known := m.st.ChannelCat[ch]
	if !known {
		return 0
	}
	base := m.unheld(u)
	if base&Administrator != 0 {
		return base
	}
	p := base
	if cat != uuid.Nil {
		sc, ok := m.st.CategoryOv[cat]
		p = m.overwrite(sc, ok, u, p)
	}
	sc, ok := m.st.ChannelOv[ch]
	p = m.overwrite(sc, ok, u, p)
	if p&ViewChannel == 0 {
		p &^= ChannelScoped
	}
	if m.timedOut(u) || m.rulesPending(u) {
		p &^= refTimeout
	}
	return m.hold(u, p)
}

// categoryVisible: the category's own overwrites leave View channels.
func (m refModel) categoryVisible(u, cat uuid.UUID) bool {
	g := m.global(u)
	if m.owner(u) || g&Administrator != 0 {
		return true
	}
	sc, ok := m.st.CategoryOv[cat]
	return m.overwrite(sc, ok, u, g)&ViewChannel != 0
}

// everyoneSees: a member with no role and no overwrite of their own would see
// ch, and ch is not private to a conversation.
func (m refModel) everyoneSees(ch uuid.UUID) bool {
	if _, ok := m.st.DMTalks[ch]; ok {
		return false
	}
	if _, ok := m.st.DMs[ch]; ok {
		return false
	}
	return m.channel(uuid.New(), ch)&ViewChannel != 0
}
