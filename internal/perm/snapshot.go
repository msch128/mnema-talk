package perm

import (
	"context"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// Subject is who a permission question is about. Owner comes from the
// account's role ('admin'); the snapshot also knows the owners it loaded, so
// a Subject built from just an ID is enough for people other than the caller.
type Subject struct {
	ID    uuid.UUID
	Owner bool
}

// Role is one role as stored. @everyone (Everyone) is implicit: every member
// has it, it sits at position 0 and cannot be renamed, moved or deleted.
type Role struct {
	ID          uuid.UUID `json:"id" format:"uuid"`
	Name        string    `json:"name" minLength:"1" maxLength:"32"`
	Color       int       `json:"color" minimum:"0" maximum:"16777215"`
	Position    int       `json:"position" minimum:"0"`
	Permissions Set       `json:"permissions" swaggertype:"integer" minimum:"0"`
	Hoist       bool      `json:"hoist"`
	Mentionable bool      `json:"mentionable"`
	Everyone    bool      `json:"is_everyone"`
}

type overwrite struct{ allow, deny Set }

// scope is the overwrites of one category or channel.
type scope struct {
	roles   map[uuid.UUID]overwrite
	members map[uuid.UUID]overwrite
}

// Snapshot is the complete permission state at one moment: roles, role
// assignments, channels with their categories and all overwrites. A single
// community is small, so keeping all of it in memory makes every check a few
// map lookups and lets the WebSocket hub filter events without touching the
// database. It is immutable once loaded; changes replace it.
//
// Resolution (Discord's order):
//
//  1. The Owner has everything, always.
//  2. base = @everyone | the union of the member's roles.
//  3. Administrator in base short-circuits to everything.
//  4. The category's overwrites, then the channel's, each applied as:
//     @everyone, then all of the member's roles combined (deny, then allow),
//     then the member's own (deny, then allow). The channel therefore wins
//     over its category.
//  5. Without ViewChannel nothing channel-scoped remains.
type Snapshot struct {
	loaded     time.Time
	everyoneID uuid.UUID
	everyone   Set
	roles      map[uuid.UUID]Role
	userRoles  map[uuid.UUID][]uuid.UUID
	owners     map[uuid.UUID]bool
	channelCat map[uuid.UUID]uuid.UUID // uuid.Nil: uncategorized
	categories map[uuid.UUID]bool
	channelOv  map[uuid.UUID]*scope
	categoryOv map[uuid.UUID]*scope
}

func newSnapshot(now time.Time) *Snapshot {
	return &Snapshot{
		loaded:     now,
		everyone:   DefaultEveryone,
		roles:      map[uuid.UUID]Role{},
		userRoles:  map[uuid.UUID][]uuid.UUID{},
		owners:     map[uuid.UUID]bool{},
		channelCat: map[uuid.UUID]uuid.UUID{},
		categories: map[uuid.UUID]bool{},
		channelOv:  map[uuid.UUID]*scope{},
		categoryOv: map[uuid.UUID]*scope{},
	}
}

// loadSnapshot reads the whole permission state in one consistent view.
func loadSnapshot(ctx context.Context, p *db.Pool, now time.Time) (*Snapshot, error) {
	s := newSnapshot(now)
	err := pgx.BeginTxFunc(ctx, p, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		return s.read(ctx, tx)
	})
	if err != nil {
		return nil, fmt.Errorf("load permissions: %w", err)
	}
	return s, nil
}

// reader runs the queries of one snapshot load; after the first failure it
// does nothing more and keeps that error.
type reader struct {
	ctx context.Context
	q   db.Querier
	err error
}

// load runs sql and collects its rows with scan.
func load[T any](r *reader, sql string, scan pgx.RowToFunc[T]) []T {
	if r.err != nil {
		return nil
	}
	rows, err := r.q.Query(r.ctx, sql)
	if err != nil {
		r.err = err
		return nil
	}
	out, err := pgx.CollectRows(rows, scan)
	r.err = err
	return out
}

type roleRow struct {
	ID          uuid.UUID
	Name        string
	Color       int
	Position    int
	Permissions int64
	Hoist       bool
	Mentionable bool
	Everyone    bool
}

type userRoleRow struct{ UserID, RoleID uuid.UUID }

type channelRow struct {
	ID       uuid.UUID
	Category *uuid.UUID
}

type overwriteRow struct {
	Channel, Category, Role, User *uuid.UUID
	Allow, Deny                   int64
}

func (s *Snapshot) read(ctx context.Context, q db.Querier) error {
	r := &reader{ctx: ctx, q: q}
	roles := load(r, `SELECT id, name, color, position, permissions, hoist, mentionable, is_everyone FROM roles`, pgx.RowToStructByPos[roleRow])
	assigned := load(r, `SELECT user_id, role_id FROM user_roles`, pgx.RowToStructByPos[userRoleRow])
	owners := load(r, `SELECT id FROM users WHERE role = 'admin'`, pgx.RowTo[uuid.UUID])
	cats := load(r, `SELECT id FROM categories`, pgx.RowTo[uuid.UUID])
	chans := load(r, `SELECT id, category_id FROM channels`, pgx.RowToStructByPos[channelRow])
	ows := load(r, `SELECT channel_id, category_id, role_id, user_id, allow, deny FROM permission_overwrites`, pgx.RowToStructByPos[overwriteRow])
	if r.err != nil {
		return r.err
	}

	for _, row := range roles {
		role := Role{ID: row.ID, Name: row.Name, Color: row.Color, Position: row.Position,
			Permissions: Set(row.Permissions), Hoist: row.Hoist, Mentionable: row.Mentionable, Everyone: row.Everyone}
		s.roles[role.ID] = role
		if role.Everyone {
			s.everyoneID, s.everyone = role.ID, role.Permissions
		}
	}
	for _, row := range assigned {
		s.userRoles[row.UserID] = append(s.userRoles[row.UserID], row.RoleID)
	}
	for id, list := range s.userRoles {
		// Highest role first, so RoleIDs reads as the member's hierarchy.
		slices.SortFunc(list, func(a, b uuid.UUID) int { return s.roles[b].Position - s.roles[a].Position })
		s.userRoles[id] = list
	}
	for _, id := range owners {
		s.owners[id] = true
	}
	for _, id := range cats {
		s.categories[id] = true
	}
	for _, c := range chans {
		s.channelCat[c.ID] = uuid.Nil
		if c.Category != nil {
			s.channelCat[c.ID] = *c.Category
		}
	}
	for _, o := range ows {
		target, id := s.channelOv, o.Channel
		if id == nil {
			target, id = s.categoryOv, o.Category
		}
		sc := target[*id]
		if sc == nil {
			sc = &scope{roles: map[uuid.UUID]overwrite{}, members: map[uuid.UUID]overwrite{}}
			target[*id] = sc
		}
		ow := overwrite{allow: Set(o.Allow), deny: Set(o.Deny)}
		if o.Role != nil {
			sc.roles[*o.Role] = ow
		} else {
			sc.members[*o.User] = ow
		}
	}
	return nil
}

// Age is how long ago the snapshot was loaded.
func (s *Snapshot) age(now time.Time) time.Duration { return now.Sub(s.loaded) }

// IsOwner reports whether sub is the Owner.
func (s *Snapshot) IsOwner(sub Subject) bool { return sub.Owner || s.owners[sub.ID] }

// RoleIDs lists the custom roles of a member, highest first.
func (s *Snapshot) RoleIDs(userID uuid.UUID) []uuid.UUID {
	return slices.Clone(s.userRoles[userID])
}

// Roles lists every role, highest first; @everyone is last.
func (s *Snapshot) Roles() []Role {
	out := make([]Role, 0, len(s.roles))
	for _, r := range s.roles {
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b Role) int { return b.Position - a.Position })
	return out
}

// Role returns one role.
func (s *Snapshot) Role(id uuid.UUID) (Role, bool) {
	r, ok := s.roles[id]
	return r, ok
}

// Global is what sub may do server-wide: the Owner everything, otherwise
// @everyone plus the member's roles, or everything for an Administrator.
func (s *Snapshot) Global(sub Subject) Set {
	if s.IsOwner(sub) {
		return All
	}
	p := s.everyone
	for _, id := range s.userRoles[sub.ID] {
		p |= s.roles[id].Permissions
	}
	if p.Has(Administrator) {
		return All
	}
	return p & All
}

func (sc *scope) apply(p Set, s *Snapshot, user uuid.UUID) Set {
	if sc == nil {
		return p
	}
	if ow, ok := sc.roles[s.everyoneID]; ok {
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

// KnowsCategory reports whether the snapshot has seen the category.
func (s *Snapshot) KnowsCategory(cat uuid.UUID) bool { return s.categories[cat] }

// Channel is what sub may do in a channel. A channel the snapshot does not
// know gives a non-owner nothing.
func (s *Snapshot) Channel(sub Subject, ch uuid.UUID) Set {
	if s.IsOwner(sub) {
		return All
	}
	cat, ok := s.channelCat[ch]
	if !ok {
		return 0
	}
	base := s.Global(sub)
	if base.Has(Administrator) {
		return All
	}
	p := base
	if cat != uuid.Nil {
		p = s.categoryOv[cat].apply(p, s, sub.ID)
	}
	p = s.channelOv[ch].apply(p, s, sub.ID)
	if !p.Has(ViewChannel) {
		p &^= ChannelScoped
	}
	return p
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
// (the Owner and Administrators), so callers can skip filtering.
func (s *Snapshot) ViewableChannels(sub Subject) []uuid.UUID {
	if s.Global(sub).Has(Administrator) {
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

// TopPosition is the position of sub's highest role (0 without custom roles);
// the Owner is above every position.
func (s *Snapshot) TopPosition(sub Subject) int {
	if s.IsOwner(sub) {
		return math.MaxInt32
	}
	top := 0
	for _, id := range s.userRoles[sub.ID] {
		top = max(top, s.roles[id].Position)
	}
	return top
}

// ErrOwnerProtected is returned for any action aimed at the Owner.
var ErrOwnerProtected = httpx.ErrForbidden("the Owner cannot be targeted")

// CanModerate checks that actor may act on target as a member: never on the
// Owner, and only on members whose highest role is below the actor's.
func (s *Snapshot) CanModerate(actor, target Subject) error {
	if s.IsOwner(target) {
		return ErrOwnerProtected
	}
	if s.IsOwner(actor) || actor.ID == target.ID {
		return nil
	}
	if s.TopPosition(actor) <= s.TopPosition(target) {
		return httpx.ErrForbidden("that member's highest role is not below yours")
	}
	return nil
}

// CanManageRole checks that actor's highest role is above the role.
func (s *Snapshot) CanManageRole(actor Subject, role Role) error {
	if s.IsOwner(actor) {
		return nil
	}
	if s.TopPosition(actor) <= role.Position {
		return httpx.ErrForbidden("that role is not below your highest role")
	}
	return nil
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

// EmptySnapshot knows no channel: everybody but the Owner gets nothing from
// it. Callers use it to fail closed when the real state cannot be loaded.
func EmptySnapshot() *Snapshot { return newSnapshot(time.Time{}) }
