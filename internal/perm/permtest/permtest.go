// Package permtest builds permission state by hand, for tests of the
// packages that consume internal/perm. The server never imports it.
package permtest

import (
	"context"
	"errors"
	"maps"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Fixture is a hand-built permission state.
type Fixture struct {
	st   perm.State
	snap *perm.Snapshot // refreshed in place by every change, so a held Snapshot stays current
}

// touch rebuilds the snapshot from the state after a change.
func (f *Fixture) touch() *Fixture {
	next := perm.NewSnapshot(f.st, time.Now())
	if f.snap == nil {
		f.snap = next
	} else {
		*f.snap = *next
	}
	return f
}

// NewFixture returns an empty state with the default @everyone.
func NewFixture() *Fixture {
	id := uuid.New()
	return (&Fixture{st: perm.State{
		EveryoneID: id,
		Everyone:   perm.DefaultEveryone,
		Roles: map[uuid.UUID]perm.Role{
			id: {ID: id, Name: "@everyone", Permissions: perm.DefaultEveryone, Everyone: true},
		},
		UserRoles:     map[uuid.UUID][]uuid.UUID{},
		Owners:        map[uuid.UUID]bool{},
		Bots:          map[uuid.UUID]bool{},
		ChannelCat:    map[uuid.UUID]uuid.UUID{},
		Stages:        map[uuid.UUID]bool{},
		Categories:    map[uuid.UUID]bool{},
		DMs:           map[uuid.UUID]map[uuid.UUID]bool{},
		ChannelOv:     map[uuid.UUID]perm.Scope{},
		CategoryOv:    map[uuid.UUID]perm.Scope{},
		Timeouts:      map[uuid.UUID]perm.Timeout{},
		RulesAccepted: map[uuid.UUID]int{},
		Joined:        map[uuid.UUID]time.Time{},
	}}).touch()
}

// Everyone sets what @everyone holds.
func (f *Fixture) Everyone(p perm.Set) *Fixture {
	f.st.Everyone = p
	r := f.st.Roles[f.st.EveryoneID]
	r.Permissions = p
	f.st.Roles[f.st.EveryoneID] = r
	return f.touch()
}

// EveryoneID is the ID of the @everyone role, for overwrites.
func (f *Fixture) EveryoneID() uuid.UUID { return f.st.EveryoneID }

// Role adds a custom role and returns its ID.
func (f *Fixture) Role(position int, p perm.Set) uuid.UUID {
	id := uuid.New()
	f.st.Roles[id] = perm.Role{ID: id, Name: id.String()[:8], Position: position, Permissions: p}
	f.touch()
	return id
}

// RoleUpload sets a role's upload override (nil inherits).
func (f *Fixture) RoleUpload(role uuid.UUID, mb *int) *Fixture {
	r := f.st.Roles[role]
	r.MaxUploadMB = mb
	f.st.Roles[role] = r
	return f.touch()
}

// GlobalUpload sets the Owner's global upload limit (nil = the ceiling).
func (f *Fixture) GlobalUpload(mb *int) *Fixture {
	f.st.GlobalUploadMB = mb
	return f.touch()
}

// ManagedRole adds a role the server maintains (a bot's role) and returns its ID.
func (f *Fixture) ManagedRole(position int, p perm.Set) uuid.UUID {
	id := f.Role(position, p)
	r := f.st.Roles[id]
	r.Managed = true
	f.st.Roles[id] = r
	f.touch()
	return id
}

// Assign gives a member a role.
func (f *Fixture) Assign(user, role uuid.UUID) *Fixture {
	f.st.UserRoles[user] = append(slices.Clip(f.st.UserRoles[user]), role)
	return f.touch()
}

// Unassign takes a role from a member.
func (f *Fixture) Unassign(user, role uuid.UUID) *Fixture {
	roles := f.st.UserRoles[user]
	for i, id := range roles {
		if id == role {
			f.st.UserRoles[user] = append(roles[:i:i], roles[i+1:]...)
			break
		}
	}
	return f.touch()
}

// Bot marks user as a bot account (no @everyone).
func (f *Fixture) Bot(user uuid.UUID) *Fixture {
	f.st.Bots[user] = true
	return f.touch()
}

// Owner marks a member as the Owner.
func (f *Fixture) Owner(user uuid.UUID) *Fixture {
	f.st.Owners[user] = true
	return f.touch()
}

// Timeout silences a member until the given time.
func (f *Fixture) Timeout(user uuid.UUID, until time.Time, reason string) *Fixture {
	f.st.Timeouts[user] = perm.Timeout{Until: until, Reason: reason}
	return f.touch()
}

// Rules sets the version of the community rules in force (0: none).
func (f *Fixture) Rules(version int) *Fixture {
	f.st.RulesVersion = version
	return f.touch()
}

// AcceptedRules records the rules version a member has accepted.
func (f *Fixture) AcceptedRules(user uuid.UUID, version int) *Fixture {
	f.st.RulesAccepted[user] = version
	return f.touch()
}

// NewMemberRules sets the minimum membership ages (zero: that gate is off).
func (f *Fixture) NewMemberRules(write, react, speak time.Duration) *Fixture {
	f.st.NewMember = perm.NewMemberRules{Write: write, React: react, Speak: speak}
	return f.touch()
}

// Joined records when a member joined.
func (f *Fixture) Joined(user uuid.UUID, at time.Time) *Fixture {
	f.st.Joined[user] = at
	return f.touch()
}

// Category adds a category.
func (f *Fixture) Category() uuid.UUID {
	id := uuid.New()
	f.st.Categories[id] = true
	f.touch()
	return id
}

// Channel adds a channel; category is uuid.Nil for none.
func (f *Fixture) Channel(category uuid.UUID) uuid.UUID {
	id := uuid.New()
	f.st.ChannelCat[id] = category
	f.touch()
	return id
}

// ChannelWithID adds a channel under a chosen ID; category is uuid.Nil for none.
func (f *Fixture) ChannelWithID(id, category uuid.UUID) {
	f.st.ChannelCat[id] = category
	f.touch()
}

// Stage makes a channel a Stage (or, with false, a normal Talk again).
func (f *Fixture) Stage(ch uuid.UUID, on bool) *Fixture {
	f.st.Stages = maps.Clone(f.st.Stages)
	if on {
		f.st.Stages[ch] = true
	} else {
		delete(f.st.Stages, ch)
	}
	return f.touch()
}

func (f *Fixture) scope(channel bool, target uuid.UUID) perm.Scope {
	m := f.st.CategoryOv
	if channel {
		m = f.st.ChannelOv
	}
	sc, ok := m[target]
	if !ok {
		sc = perm.Scope{Roles: map[uuid.UUID]perm.AllowDeny{}, Members: map[uuid.UUID]perm.AllowDeny{}}
		m[target] = sc
	}
	return sc
}

// RoleOverwrite sets a role's overwrite on a channel (or category).
func (f *Fixture) RoleOverwrite(channel bool, target, role uuid.UUID, allow, deny perm.Set) *Fixture {
	f.scope(channel, target).Roles[role] = perm.AllowDeny{Allow: allow, Deny: deny}
	return f.touch()
}

// MemberOverwrite sets a member's overwrite on a channel (or category).
func (f *Fixture) MemberOverwrite(channel bool, target, user uuid.UUID, allow, deny perm.Set) *Fixture {
	f.scope(channel, target).Members[user] = perm.AllowDeny{Allow: allow, Deny: deny}
	return f.touch()
}

// DM adds a direct message conversation with the given members.
func (f *Fixture) DM(members ...uuid.UUID) uuid.UUID {
	id := uuid.New()
	f.st.DMs[id] = map[uuid.UUID]bool{}
	for _, m := range members {
		f.st.DMs[id][m] = true
	}
	f.touch()
	return id
}

// AddDMMember adds a member to the conversation dm.
func (f *Fixture) AddDMMember(dm, member uuid.UUID) *Fixture {
	f.st.DMs[dm][member] = true
	return f.touch()
}

// DMTalk adds a Talk that belongs to the conversation dm.
func (f *Fixture) DMTalk(dm uuid.UUID) uuid.UUID {
	id := f.Channel(uuid.Nil)
	if f.st.DMTalks == nil {
		f.st.DMTalks = map[uuid.UUID]uuid.UUID{}
	}
	f.st.DMTalks[id] = dm
	f.touch()
	return id
}

// DMEnabled sets the community's direct message switch (on by default).
func (f *Fixture) DMEnabled(on bool) *Fixture {
	f.st.DMsDisabled = !on
	return f.touch()
}

// Snapshot returns the state built so far.
func (f *Fixture) Snapshot() *perm.Snapshot { return f.snap }

// Service returns a perm.Service answering from this state, without a
// database. It sees changes made to the Fixture afterwards.
func (f *Fixture) Service() *perm.Service {
	return perm.NewServiceFrom(func(context.Context, time.Time) (*perm.Snapshot, error) {
		return f.snap, nil
	})
}

// Fork returns an independent copy of the state, for a test that swaps the
// permissions a running component sees without touching the ones it reads.
func (f *Fixture) Fork() *Fixture {
	s := f.st
	s.Roles, s.UserRoles, s.Owners, s.Bots = maps.Clone(s.Roles), maps.Clone(s.UserRoles), maps.Clone(s.Owners), maps.Clone(s.Bots)
	s.ChannelCat, s.Categories = maps.Clone(s.ChannelCat), maps.Clone(s.Categories)
	s.DMs, s.Stages = maps.Clone(s.DMs), maps.Clone(s.Stages)
	s.Timeouts, s.RulesAccepted = maps.Clone(s.Timeouts), maps.Clone(s.RulesAccepted)
	s.ChannelOv, s.CategoryOv = cloneScopes(s.ChannelOv), cloneScopes(s.CategoryOv)
	return (&Fixture{st: s}).touch()
}

func cloneScopes(in map[uuid.UUID]perm.Scope) map[uuid.UUID]perm.Scope {
	out := make(map[uuid.UUID]perm.Scope, len(in))
	for k, v := range in {
		out[k] = perm.Scope{Roles: maps.Clone(v.Roles), Members: maps.Clone(v.Members)}
	}
	return out
}

// Unloadable returns a Service whose state can never be loaded, for tests of
// the fail-closed path.
func Unloadable() *perm.Service {
	return perm.NewServiceFrom(func(context.Context, time.Time) (*perm.Snapshot, error) {
		return nil, errors.New("permtest: state unavailable")
	})
}
