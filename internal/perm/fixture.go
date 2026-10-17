package perm

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Fixture builds permission state by hand, for tests of the packages that
// consume it. The server never uses it.
type Fixture struct{ s *Snapshot }

// NewFixture returns an empty state with the default @everyone.
func NewFixture() *Fixture {
	f := &Fixture{s: newSnapshot(time.Unix(0, 0))}
	f.s.everyoneID = uuid.New()
	f.s.roles[f.s.everyoneID] = Role{ID: f.s.everyoneID, Name: "@everyone", Permissions: DefaultEveryone, Everyone: true}
	return f
}

// Everyone sets what @everyone holds.
func (f *Fixture) Everyone(p Set) *Fixture {
	f.s.everyone = p
	r := f.s.roles[f.s.everyoneID]
	r.Permissions = p
	f.s.roles[f.s.everyoneID] = r
	return f
}

// EveryoneID is the ID of the @everyone role, for overwrites.
func (f *Fixture) EveryoneID() uuid.UUID { return f.s.everyoneID }

// Role adds a custom role and returns its ID.
func (f *Fixture) Role(position int, p Set) uuid.UUID {
	id := uuid.New()
	f.s.roles[id] = Role{ID: id, Name: id.String()[:8], Position: position, Permissions: p}
	return id
}

// Assign gives a member a role.
func (f *Fixture) Assign(user, role uuid.UUID) *Fixture {
	f.s.userRoles[user] = append(f.s.userRoles[user], role)
	return f
}

// Unassign takes a role from a member.
func (f *Fixture) Unassign(user, role uuid.UUID) *Fixture {
	roles := f.s.userRoles[user]
	for i, id := range roles {
		if id == role {
			f.s.userRoles[user] = append(roles[:i:i], roles[i+1:]...)
			break
		}
	}
	return f
}

// Bot marks user as a bot account (no @everyone).
func (f *Fixture) Bot(user uuid.UUID) *Fixture {
	f.s.bots[user] = true
	return f
}

// Owner marks a member as the Owner.
func (f *Fixture) Owner(user uuid.UUID) *Fixture {
	f.s.owners[user] = true
	return f
}

// Timeout silences a member until the given time.
func (f *Fixture) Timeout(user uuid.UUID, until time.Time, reason string) *Fixture {
	f.s.timeouts[user] = Timeout{Until: until, Reason: reason}
	return f
}

// Category adds a category.
func (f *Fixture) Category() uuid.UUID {
	id := uuid.New()
	f.s.categories[id] = true
	return id
}

// Channel adds a channel; category is uuid.Nil for none.
func (f *Fixture) Channel(category uuid.UUID) uuid.UUID {
	id := uuid.New()
	f.s.channelCat[id] = category
	return id
}

func (f *Fixture) scope(channel bool, target uuid.UUID) *scope {
	m := f.s.categoryOv
	if channel {
		m = f.s.channelOv
	}
	sc := m[target]
	if sc == nil {
		sc = &scope{roles: map[uuid.UUID]overwrite{}, members: map[uuid.UUID]overwrite{}}
		m[target] = sc
	}
	return sc
}

// RoleOverwrite sets a role's overwrite on a channel (or category).
func (f *Fixture) RoleOverwrite(channel bool, target, role uuid.UUID, allow, deny Set) *Fixture {
	f.scope(channel, target).roles[role] = overwrite{allow: allow, deny: deny}
	return f
}

// MemberOverwrite sets a member's overwrite on a channel (or category).
func (f *Fixture) MemberOverwrite(channel bool, target, user uuid.UUID, allow, deny Set) *Fixture {
	f.scope(channel, target).members[user] = overwrite{allow: allow, deny: deny}
	return f
}

// Snapshot returns the state built so far.
func (f *Fixture) Snapshot() *Snapshot { return f.s }

// Service returns a Service answering from this state forever, without a
// database.
func (f *Fixture) Service() *Service {
	svc := &Service{now: func() time.Time { return f.s.loaded }}
	svc.cur.Store(f.s)
	return svc
}

var errNoDatabase = errors.New("permissions: no database")
