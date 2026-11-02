package perm

import (
	"github.com/google/uuid"
)

// Direct messages are conversations kept in the channels table (type 'dm').
// They are not channels as far as permissions go: no category, no overwrites,
// no roles. The one rule is membership. A member of a conversation may read
// it; writing, reacting and attaching additionally need Send direct messages
// (and the matching channel permission), the community's direct message
// switch to be on, and no timeout or pending rules. Everybody else, the Owner
// and Administrators included, gets nothing: Channel answers 0 before the
// Owner shortcut, so every caller that asks the snapshot (REST, WebSocket
// hub, media, push, search) inherits the rule.

// dmWritePerms are the channel permissions a member keeps in a conversation
// when they may write there. Manage messages, mention-everyone and the
// voice permissions never apply: nobody moderates a conversation.
const dmWritePerms = SendMessages | AttachFiles | AddReactions

// addDMs records the conversations and their members.
func (s *Snapshot) addDMs(rows []dmMemberRow, enabled bool) {
	s.dmEnabled = enabled
	for _, r := range rows {
		m := s.dms[r.Channel]
		if m == nil {
			m = map[uuid.UUID]bool{}
			s.dms[r.Channel] = m
		}
		m[r.User] = true
	}
}

type dmMemberRow struct{ Channel, User uuid.UUID }

type dmTalkRow struct{ Talk, DM uuid.UUID }

// KnowsDM reports whether the snapshot has seen the conversation.
func (s *Snapshot) KnowsDM(ch uuid.UUID) bool { _, ok := s.dms[ch]; return ok }

// IsDM reports whether ch is a direct message conversation.
func (s *Snapshot) IsDM(ch uuid.UUID) bool { return s.KnowsDM(ch) }

// DMMembers lists the members of a conversation.
func (s *Snapshot) DMMembers(ch uuid.UUID) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(s.dms[ch]))
	for id := range s.dms[ch] {
		out = append(out, id)
	}
	return out
}

// DMsOf lists the conversations userID is in.
func (s *Snapshot) DMsOf(userID uuid.UUID) []uuid.UUID {
	var out []uuid.UUID
	for ch, members := range s.dms {
		if members[userID] {
			out = append(out, ch)
		}
	}
	return out
}

// DirectMessagesEnabled reports the Owner's community switch.
func (s *Snapshot) DirectMessagesEnabled() bool { return s.dmEnabled }

// CanStartDM reports whether sub may start conversations, add members to one
// and write in them: they hold Send direct messages, the switch is on, and
// no timeout or pending rules take it away.
func (s *Snapshot) CanStartDM(sub Subject) bool {
	return s.dmEnabled && s.globalUnheld(sub).Has(SendDirectMessages)
}

// dmPerms is what sub may do in a conversation with the given members.
func (s *Snapshot) dmPerms(sub Subject, members map[uuid.UUID]bool) Set {
	if !members[sub.ID] {
		return 0
	}
	p := ViewChannel
	if s.CanStartDM(sub) {
		p |= s.globalUnheld(sub) & dmWritePerms // conversations are exempt from the waits of new members
	}
	return p
}
