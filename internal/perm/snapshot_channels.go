package perm

import (
	"github.com/google/uuid"
)

func (sc *scope) apply(p Set, s *Snapshot, user uuid.UUID) Set {
	if sc == nil {
		return p
	}
	if ow, ok := sc.roles[s.everyoneID]; ok && !s.bots[user] {
		p = p&^ow.deny | ow.allow
	}
	var allow, deny Set
	for _, id := range s.userRoles[user] {
		if ow, ok := sc.roles[id]; ok {
			allow |= ow.allow
			deny |= ow.deny
		}
	}
	p = p&^deny | allow
	if ow, ok := sc.members[user]; ok {
		p = p&^ow.deny | ow.allow
	}
	return p
}

// KnowsChannel reports whether the snapshot has seen the channel.
func (s *Snapshot) KnowsChannel(ch uuid.UUID) bool {
	_, ok := s.channelCat[ch]
	return ok
}

// IsStage reports whether the channel is a Stage: a Talk where only members
// with talk power may send audio, camera or screen.
func (s *Snapshot) IsStage(ch uuid.UUID) bool { return s.stages[ch] }

// CategoryOf returns the category of a channel (uuid.Nil: none).
func (s *Snapshot) CategoryOf(ch uuid.UUID) (uuid.UUID, bool) {
	cat, ok := s.channelCat[ch]
	return cat, ok
}

// CanSeeAllChannelsIn reports whether sub may view every channel of a
// category (always for the Owner and Administrators). Changing a category's
// overwrites, or deleting it, can widen access to all its channels, so it is
// only allowed to someone who can see them all.
func (s *Snapshot) CanSeeAllChannelsIn(sub Subject, cat uuid.UUID) bool {
	for ch, c := range s.channelCat {
		if c == cat && !s.Channel(sub, ch).Has(ViewChannel) {
			return false
		}
	}
	return true
}

// HasCategoryOverwrites reports whether a category carries any overwrite.
func (s *Snapshot) HasCategoryOverwrites(cat uuid.UUID) bool { return s.categoryOv[cat] != nil }

// VisibleCategories lists the categories sub may see (see CategoryVisible).
func (s *Snapshot) VisibleCategories(sub Subject) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(s.categories))
	for id := range s.categories {
		if s.CategoryVisible(sub, id) {
			out = append(out, id)
		}
	}
	return out
}

// KnowsCategory reports whether the snapshot has seen the category.
func (s *Snapshot) KnowsCategory(cat uuid.UUID) bool { return s.categories[cat] }

// Channel is what sub may do in a channel. A channel the snapshot does not
// know gives a non-owner nothing.
func (s *Snapshot) Channel(sub Subject, ch uuid.UUID) Set {
	if members, ok := s.dms[ch]; ok {
		return s.dmPerms(sub, members) // membership only: not even the Owner gets in
	}
	if dm, ok := s.dmTalks[ch]; ok && !s.dms[dm][sub.ID] {
		return 0 // a Talk of a conversation: its members only, not even the Owner
	}
	if s.IsOwner(sub) {
		return All
	}
	cat, ok := s.channelCat[ch]
	if !ok {
		return 0
	}
	base := s.globalUnheld(sub)
	if base.Has(Administrator) {
		return base
	}
	p := base
	if cat != uuid.Nil {
		p = s.categoryOv[cat].apply(p, s, sub.ID)
	}
	p = s.channelOv[ch].apply(p, s, sub.ID)
	if !p.Has(ViewChannel) {
		p &^= ChannelScoped
	}
	// Overwrites must not hand back what the timeout took.
	p = s.silence(sub.ID, p)
	if _, ok := s.dmTalks[ch]; ok {
		return p // the Talk of a conversation: its members vouch for each other
	}
	return s.hold(sub.ID, p)
}

// CategoryPerms is what sub holds under a category's own overwrites: the
// server-wide permissions with those overwrites applied, so a bit the category
// denies them does not count as held.
func (s *Snapshot) CategoryPerms(sub Subject, cat uuid.UUID) Set {
	base := s.Global(sub)
	if s.IsOwner(sub) || base.Has(Administrator) {
		return base
	}
	return s.categoryOv[cat].apply(base, s, sub.ID)
}

// CategoryHeld is what sub holds under a category's overwrites and in every
// channel of it: the most they may allow or deny for the category, since the
// change reaches all of its channels.
func (s *Snapshot) CategoryHeld(sub Subject, cat uuid.UUID) Set {
	held := s.CategoryPerms(sub, cat)
	if s.IsOwner(sub) {
		return held
	}
	for ch, c := range s.channelCat {
		if c == cat {
			held &= s.Channel(sub, ch)
		}
	}
	return held
}

// CategoryVisible reports whether sub may see the category itself: its own
// overwrites leave ViewChannel in place.
func (s *Snapshot) CategoryVisible(sub Subject, cat uuid.UUID) bool {
	if s.IsOwner(sub) {
		return true
	}
	base := s.Global(sub)
	if base.Has(Administrator) {
		return true
	}
	return s.categoryOv[cat].apply(base, s, sub.ID).Has(ViewChannel)
}

// ViewableChannels lists the channels sub may view; nil means all of them
// (the Owner and Administrators, as long as no Talk belongs to a conversation:
// those are for their members only), so callers can skip filtering.
func (s *Snapshot) ViewableChannels(sub Subject) []uuid.UUID {
	if s.Global(sub).Has(Administrator) && len(s.dmTalks) == 0 {
		return nil
	}
	out := make([]uuid.UUID, 0, len(s.channelCat))
	for ch := range s.channelCat {
		if s.Channel(sub, ch).Has(ViewChannel) {
			out = append(out, ch)
		}
	}
	return out
}

// MayConnectAnywhere reports whether sub may join a Talk somewhere: they can
// view and connect to at least one channel (an Administrator can to all of
// them). It says nothing about a particular Talk; it gates what any Talk
// needs first, such as a relay credential for voice.
func (s *Snapshot) MayConnectAnywhere(sub Subject) bool {
	if s.Global(sub).Has(Administrator) {
		return true
	}
	for ch := range s.channelCat {
		if s.Channel(sub, ch).Has(ViewChannel | Connect) {
			return true
		}
	}
	return false
}

// HiddenChannels lists the channels sub may not view: the complement of
// ViewableChannels within the channels the snapshot knows.
func (s *Snapshot) HiddenChannels(sub Subject) []uuid.UUID {
	if s.Global(sub).Has(Administrator) && len(s.dmTalks) == 0 {
		return nil
	}
	var out []uuid.UUID
	for ch := range s.channelCat {
		if !s.Channel(sub, ch).Has(ViewChannel) {
			out = append(out, ch)
		}
	}
	return out
}

// ChannelPermissions maps every channel sub may view to what they may do in
// it.
func (s *Snapshot) ChannelPermissions(sub Subject) map[uuid.UUID]Set {
	out := make(map[uuid.UUID]Set, len(s.channelCat))
	for ch := range s.channelCat {
		if p := s.Channel(sub, ch); p.Has(ViewChannel) {
			out[ch] = p
		}
	}
	return out
}

// GlobalUploadMB is the global upload limit in MB: the Owner's setting, or the
// ceiling when none is set, never above the ceiling.
func (s *Snapshot) GlobalUploadMB(ceiling int) int {
	if s.globalUploadMB == nil {
		return ceiling
	}
	return min(*s.globalUploadMB, ceiling)
}

// UploadMB is the largest file sub may upload, in MB. The Owner gets the
// ceiling. Otherwise it is the highest override among @everyone and the
// member's roles, or the global limit when none sets one; never above the
// ceiling.
func (s *Snapshot) UploadMB(sub Subject, ceiling int) int {
	if s.IsOwner(sub) {
		return ceiling
	}
	best := 0
	if ev, ok := s.roles[s.everyoneID]; ok && ev.MaxUploadMB != nil {
		best = *ev.MaxUploadMB
	}
	for _, id := range s.userRoles[sub.ID] {
		if v := s.roles[id].MaxUploadMB; v != nil {
			best = max(best, *v)
		}
	}
	if best == 0 {
		return s.GlobalUploadMB(ceiling)
	}
	return min(best, ceiling)
}
