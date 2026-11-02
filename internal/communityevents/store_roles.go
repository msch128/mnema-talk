package communityevents

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// Promotion says that a member's place changed without their doing: they moved
// up from the waitlist of a role (Kind empty) or, when a role is removed and
// its members move to a full one, down to its waitlist (SignupKindWaitlisted).
type Promotion struct {
	EventID  uuid.UUID
	UserID   uuid.UUID
	RoleName string
	Kind     string
}

// roleRow is a role as the transactions of the roster see it.
type roleRow struct {
	ID       uuid.UUID
	Name     string
	Capacity *int
}

// inTx runs fn in a transaction.
func (s *Store) inTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	b, ok := s.DB.(interface {
		Begin(context.Context) (pgx.Tx, error)
	})
	if !ok {
		return errors.New("the event store has no transactions")
	}
	return pgx.BeginFunc(ctx, b, fn)
}

// lockRoles locks the roles of an event (in id order, so two roster changes
// never wait for each other in a circle) and returns them. Every change of a
// roster holds these locks, so the capacity check and the change that follows
// it cannot be interleaved with another sign-up.
func lockRoles(ctx context.Context, tx pgx.Tx, eventID uuid.UUID) ([]roleRow, error) {
	rows, err := tx.Query(ctx, `SELECT id, name, capacity FROM event_roles WHERE event_id = $1 ORDER BY id FOR UPDATE`, eventID)
	if err != nil {
		return nil, fmt.Errorf("lock roles: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (roleRow, error) {
		var x roleRow
		return x, r.Scan(&x.ID, &x.Name, &x.Capacity)
	})
}

// openEvent returns the status of an event inside tx, failing for an unknown one.
func openEvent(ctx context.Context, tx pgx.Tx, eventID uuid.UUID, lock bool) (string, error) {
	q := `SELECT status FROM events WHERE id = $1`
	if lock {
		// NO KEY UPDATE serializes the role editors with each other without
		// conflicting with the FOR KEY SHARE that inserting a sign-up takes on
		// the event row through its foreign key (FOR UPDATE would deadlock
		// against a sign-up that holds the role locks).
		q += ` FOR NO KEY UPDATE`
	}
	var status string
	err := tx.QueryRow(ctx, q, eventID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", httpx.ErrNotFound("event not found")
	}
	return status, err
}

func isOpen(status string) bool { return status == StatusScheduled || status == StatusActive }

// promoteRole moves the oldest waitlisted members of a role up while it has
// room (all of them when it is unlimited) and returns who moved.
func promoteRole(ctx context.Context, tx pgx.Tx, eventID uuid.UUID, role roleRow) ([]Promotion, error) {
	rows, err := tx.Query(ctx, `
		WITH room AS (
		    SELECT CASE WHEN $2::int IS NULL THEN 1000000
		                ELSE GREATEST($2::int - (SELECT COUNT(*) FROM event_signups WHERE role_id = $1 AND state = 'confirmed'), 0)
		           END AS n
		), pick AS (
		    SELECT user_id FROM event_signups
		    WHERE role_id = $1 AND state = 'waitlist'
		    ORDER BY created_at, user_id
		    LIMIT (SELECT n FROM room)
		)
		UPDATE event_signups SET state = 'confirmed'
		WHERE role_id = $1 AND user_id IN (SELECT user_id FROM pick)
		RETURNING user_id`, role.ID, role.Capacity)
	if err != nil {
		return nil, fmt.Errorf("promote waitlist: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, err
	}
	out := make([]Promotion, len(ids))
	for i, id := range ids {
		out[i] = Promotion{EventID: eventID, UserID: id, RoleName: role.Name}
	}
	return out, nil
}

// validateRoles checks a role list: its size, the names (non-empty, unique
// without regard to case), the emoji and the capacities.
func validateRoles(in []RoleInput) ([]RoleInput, error) {
	if len(in) > MaxEventRoles {
		return nil, httpx.ErrInvalidInput(fmt.Sprintf("an event has at most %d roles", MaxEventRoles))
	}
	seen := map[string]bool{}
	out := make([]RoleInput, len(in))
	for i, r := range in {
		clean, err := cleanRole(r)
		if err != nil {
			return nil, err
		}
		key := strings.ToLower(clean.Name)
		if seen[key] {
			return nil, httpx.ErrConflict("two roles are called " + clean.Name)
		}
		seen[key] = true
		out[i] = clean
	}
	return out, nil
}

func cleanRole(r RoleInput) (RoleInput, error) {
	name, err := httpx.CleanText("name", r.Name, MaxRoleNameLen, true)
	if err != nil {
		return r, err
	}
	emoji, err := httpx.CleanText("emoji", r.Emoji, MaxRoleEmojiLen, false)
	if err != nil {
		return r, err
	}
	if r.Capacity != nil && (*r.Capacity < 1 || *r.Capacity > MaxRoleCapacity) {
		return r, httpx.ErrInvalidInput(fmt.Sprintf("capacity must be between 1 and %d", MaxRoleCapacity))
	}
	if utf8.RuneCountInString(name) == 0 {
		return r, httpx.ErrInvalidInput("name is required")
	}
	return RoleInput{ID: r.ID, Name: name, Emoji: emoji, Capacity: r.Capacity}, nil
}

// SetRoles replaces the role list of an event with in, in this order, and
// returns the members who moved up because capacities grew. A role named by id
// keeps its sign-ups; a role that is not in the list goes, and with it its
// sign-ups unless it has any (then the change is refused: move them first). A
// capacity below the confirmed members is refused with the number to remove.
func (s *Store) SetRoles(ctx context.Context, eventID uuid.UUID, in []RoleInput) ([]Promotion, error) {
	roles, err := validateRoles(in)
	if err != nil {
		return nil, err
	}
	var moved []Promotion
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		status, err := openEvent(ctx, tx, eventID, true)
		if err != nil {
			return err
		}
		if !isOpen(status) {
			return httpx.ErrConflict("this event is " + status + " and its roles can no longer change")
		}
		existing, err := lockRoles(ctx, tx, eventID)
		if err != nil {
			return err
		}
		if moved, err = applyRoles(ctx, tx, eventID, existing, roles); err != nil {
			return err
		}
		return nil
	})
	return moved, err
}

func applyRoles(ctx context.Context, tx pgx.Tx, eventID uuid.UUID, existing []roleRow, roles []RoleInput) ([]Promotion, error) {
	byID := make(map[uuid.UUID]roleRow, len(existing))
	for _, r := range existing {
		byID[r.ID] = r
	}
	kept := map[uuid.UUID]bool{}
	for _, r := range roles {
		if r.ID == nil {
			continue
		}
		if _, ok := byID[*r.ID]; !ok {
			return nil, httpx.ErrNotFound("role not found")
		}
		kept[*r.ID] = true
	}
	if err := checkRemovedRoles(ctx, tx, existing, kept); err != nil {
		return nil, err
	}
	if err := checkCapacities(ctx, tx, byID, roles); err != nil {
		return nil, err
	}
	// Names go out of the way first: a swap of two names would meet the unique index.
	if _, err := tx.Exec(ctx, `UPDATE event_roles SET name = id::text WHERE event_id = $1 AND id = ANY($2)`, eventID, keys(kept)); err != nil {
		return nil, fmt.Errorf("rename roles: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM event_roles WHERE event_id = $1 AND NOT (id = ANY($2))`, eventID, keys(kept)); err != nil {
		return nil, fmt.Errorf("delete roles: %w", err)
	}
	return writeRoles(ctx, tx, eventID, roles)
}

func keys(m map[uuid.UUID]bool) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// checkRemovedRoles refuses to drop a role that still has sign-ups.
func checkRemovedRoles(ctx context.Context, tx pgx.Tx, existing []roleRow, kept map[uuid.UUID]bool) error {
	for _, r := range existing {
		if kept[r.ID] {
			continue
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM event_signups WHERE role_id = $1`, r.ID).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return httpx.ErrConflict(fmt.Sprintf("the role %s has %d sign-ups; move them to another role before it is removed", r.Name, n))
		}
	}
	return nil
}

// checkCapacities refuses a capacity below the members already confirmed.
func checkCapacities(ctx context.Context, tx pgx.Tx, byID map[uuid.UUID]roleRow, roles []RoleInput) error {
	for _, r := range roles {
		if r.ID == nil || r.Capacity == nil {
			continue
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM event_signups WHERE role_id = $1 AND state = 'confirmed'`, *r.ID).Scan(&n); err != nil {
			return err
		}
		if n > *r.Capacity {
			return httpx.ErrConflict(fmt.Sprintf("the role %s has %d confirmed members; remove %d before lowering its capacity to %d", byID[*r.ID].Name, n, n-*r.Capacity, *r.Capacity))
		}
	}
	return nil
}

// writeRoles stores names, emoji, capacities and positions (creating new roles)
// and promotes the waitlist of every role that has room.
func writeRoles(ctx context.Context, tx pgx.Tx, eventID uuid.UUID, roles []RoleInput) ([]Promotion, error) {
	var promoted []Promotion
	for pos, r := range roles {
		var id uuid.UUID
		var err error
		if r.ID != nil {
			id = *r.ID
			_, err = tx.Exec(ctx, `UPDATE event_roles SET name = $2, emoji = $3, capacity = $4, position = $5 WHERE id = $1`, id, r.Name, r.Emoji, r.Capacity, pos)
		} else {
			err = tx.QueryRow(ctx, `INSERT INTO event_roles (event_id, name, emoji, capacity, position) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
				eventID, r.Name, r.Emoji, r.Capacity, pos).Scan(&id)
		}
		if err != nil {
			if db.IsUniqueViolation(err) {
				return nil, httpx.ErrConflict("two roles are called " + r.Name)
			}
			return nil, fmt.Errorf("store role: %w", err)
		}
		p, err := promoteRole(ctx, tx, eventID, roleRow{ID: id, Name: r.Name, Capacity: r.Capacity})
		if err != nil {
			return nil, err
		}
		promoted = append(promoted, p...)
	}
	return promoted, nil
}

// DeleteRole removes a role. Its sign-ups move to moveTo (keeping their
// place in line; confirmed members stay confirmed while there is room) or,
// without moveTo, the removal is refused when there are any.
func (s *Store) DeleteRole(ctx context.Context, eventID, roleID uuid.UUID, moveTo *uuid.UUID) ([]Promotion, error) {
	var promoted []Promotion
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		status, err := openEvent(ctx, tx, eventID, true)
		if err != nil {
			return err
		}
		if !isOpen(status) {
			return httpx.ErrConflict("this event is " + status + " and its roles can no longer change")
		}
		roles, err := lockRoles(ctx, tx, eventID)
		if err != nil {
			return err
		}
		role, target, err := pickRoles(roles, roleID, moveTo)
		if err != nil {
			return err
		}
		if promoted, err = moveSignups(ctx, tx, eventID, role, target); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM event_roles WHERE id = $1`, roleID)
		return err
	})
	return promoted, err
}

func pickRoles(roles []roleRow, roleID uuid.UUID, moveTo *uuid.UUID) (role roleRow, target *roleRow, err error) {
	found := false
	for i, r := range roles {
		if r.ID == roleID {
			role, found = r, true
		}
		if moveTo != nil && r.ID == *moveTo {
			target = &roles[i]
		}
	}
	switch {
	case !found:
		return role, nil, httpx.ErrNotFound("role not found")
	case moveTo != nil && (target == nil || *moveTo == roleID):
		return role, nil, httpx.ErrInvalidInput("move_to must be another role of this event")
	}
	return role, target, nil
}

// moveSignups hands the sign-ups of role over to target, or refuses when there
// is no target and the role is not empty. It returns the members whose standing
// changed: those who were waitlisted and are confirmed by the move, and those who
// were confirmed and end up on the waitlist of a target that is full.
func moveSignups(ctx context.Context, tx pgx.Tx, eventID uuid.UUID, role roleRow, target *roleRow) ([]Promotion, error) {
	var n int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM event_signups WHERE role_id = $1`, role.ID).Scan(&n); err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, nil
	}
	if target == nil {
		return nil, httpx.ErrConflict(fmt.Sprintf("the role %s has %d sign-ups; pass move_to to hand them to another role", role.Name, n))
	}
	waiting, err := membersIn(ctx, tx, role.ID, SignupWaitlist)
	if err != nil {
		return nil, err
	}
	held, err := membersIn(ctx, tx, role.ID, SignupConfirmed)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE event_signups SET role_id = $2, state = 'waitlist' WHERE role_id = $1`, role.ID, target.ID); err != nil {
		return nil, fmt.Errorf("move sign-ups: %w", err)
	}
	// The oldest sign-up goes first, so a member who moves over takes the place
	// their own sign-up time gives them among the target's waitlist.
	all, err := promoteRole(ctx, tx, eventID, *target)
	if err != nil {
		return nil, err
	}
	var changed []Promotion
	confirmed := make(map[uuid.UUID]bool, len(all))
	for _, p := range all {
		confirmed[p.UserID] = true
		if waiting[p.UserID] {
			changed = append(changed, p)
		}
	}
	for id := range held {
		if !confirmed[id] {
			changed = append(changed, Promotion{EventID: eventID, UserID: id, RoleName: target.Name, Kind: SignupKindWaitlisted})
		}
	}
	return changed, nil
}

// membersIn returns the members of a role in one state.
func membersIn(ctx context.Context, tx pgx.Tx, roleID uuid.UUID, state string) (map[uuid.UUID]bool, error) {
	rows, err := tx.Query(ctx, `SELECT user_id FROM event_signups WHERE role_id = $1 AND state = $2`, roleID, state)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, err
	}
	set := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set, nil
}
