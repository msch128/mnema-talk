package perm

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
)

// clock is the time source of timeout checks (replaced by tests).
var clock = time.Now

func newSnapshot(now time.Time) *Snapshot {
	return &Snapshot{
		loaded:     now,
		everyone:   DefaultEveryone,
		roles:      map[uuid.UUID]Role{},
		userRoles:  map[uuid.UUID][]uuid.UUID{},
		owners:     map[uuid.UUID]bool{},
		bots:       map[uuid.UUID]bool{},
		channelCat: map[uuid.UUID]uuid.UUID{},
		categories: map[uuid.UUID]bool{},
		channelOv:  map[uuid.UUID]*scope{},
		categoryOv: map[uuid.UUID]*scope{},
		timeouts:   map[uuid.UUID]Timeout{},
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
	Managed     bool
	MaxUploadMB *int
	BotUserID   *uuid.UUID
}

type userRoleRow struct{ UserID, RoleID uuid.UUID }

type timeoutRow struct {
	ID     uuid.UUID
	Until  time.Time
	Reason string
}

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
	roles := load(r, `SELECT id, name, color, position, permissions, hoist, mentionable, is_everyone, managed, max_upload_mb, bot_user_id FROM roles`, pgx.RowToStructByPos[roleRow])
	assigned := load(r, `SELECT user_id, role_id FROM user_roles`, pgx.RowToStructByPos[userRoleRow])
	owners := load(r, `SELECT id FROM users WHERE role = 'admin'`, pgx.RowTo[uuid.UUID])
	settings := load(r, `SELECT max_upload_mb FROM instance_settings`, pgx.RowTo[*int])
	timeouts := load(r, `SELECT id, timeout_until, timeout_reason FROM users WHERE timeout_until > NOW()`, pgx.RowToStructByPos[timeoutRow])
	bots := load(r, `SELECT user_id FROM bots`, pgx.RowTo[uuid.UUID])
	cats := load(r, `SELECT id FROM categories`, pgx.RowTo[uuid.UUID])
	chans := load(r, `SELECT id, category_id FROM channels`, pgx.RowToStructByPos[channelRow])
	ows := load(r, `SELECT channel_id, category_id, role_id, user_id, allow, deny FROM permission_overwrites`, pgx.RowToStructByPos[overwriteRow])
	if r.err != nil {
		return r.err
	}

	s.addRoles(roles)
	s.addAssignments(assigned)
	if len(settings) > 0 {
		s.globalUploadMB = settings[0]
	}
	for _, id := range owners {
		s.owners[id] = true
	}
	for _, t := range timeouts {
		s.timeouts[t.ID] = Timeout{Until: t.Until, Reason: t.Reason}
	}
	for _, id := range bots {
		s.bots[id] = true
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
	s.addOverwrites(ows)
	return nil
}

func (s *Snapshot) addRoles(rows []roleRow) {
	for _, row := range rows {
		role := Role{ID: row.ID, Name: row.Name, Color: row.Color, Position: row.Position,
			Permissions: Set(row.Permissions), Hoist: row.Hoist, Mentionable: row.Mentionable, Everyone: row.Everyone, Managed: row.Managed, MaxUploadMB: row.MaxUploadMB, BotUserID: row.BotUserID}
		s.roles[role.ID] = role
		if role.Everyone {
			s.everyoneID, s.everyone = role.ID, role.Permissions
		}
	}
}

// addAssignments records who holds which role, each member's roles ordered
// highest first.
func (s *Snapshot) addAssignments(rows []userRoleRow) {
	for _, row := range rows {
		s.userRoles[row.UserID] = append(s.userRoles[row.UserID], row.RoleID)
	}
	for id, list := range s.userRoles {
		// Highest role first, so RoleIDs reads as the member's hierarchy.
		slices.SortFunc(list, func(a, b uuid.UUID) int { return s.roles[b].Position - s.roles[a].Position })
		s.userRoles[id] = list
	}
}

func (s *Snapshot) addOverwrites(rows []overwriteRow) {
	for _, o := range rows {
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
}
