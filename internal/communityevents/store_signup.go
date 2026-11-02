package communityevents

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// SignUp puts a member on a role: confirmed while the role has room, otherwise
// on its waitlist. A member who already holds another role of the event moves
// over (their old slot frees and its waitlist moves up); signing up again for
// the same role changes nothing. The capacity check runs under the role
// locks, so the last slot is never given twice. A sign-up counts as going.
func (s *Store) SignUp(ctx context.Context, eventID, userID, roleID uuid.UUID) (Signup, []Promotion, error) {
	var result Signup
	var promoted []Promotion
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		status, err := openEvent(ctx, tx, eventID, false)
		if err != nil {
			return err
		}
		if !isOpen(status) {
			return httpx.ErrConflict("this event is " + status + " and takes no more sign-ups")
		}
		roles, err := lockRoles(ctx, tx, eventID)
		if err != nil {
			return err
		}
		role, ok := findRole(roles, roleID)
		if !ok {
			return httpx.ErrNotFound("role not found")
		}
		if promoted, err = leaveOtherRole(ctx, tx, eventID, userID, role, roles); err != nil {
			return err
		}
		if result, err = joinRole(ctx, tx, eventID, userID, role); err != nil {
			return err
		}
		return markGoing(ctx, tx, eventID, userID)
	})
	return result, promoted, err
}

func findRole(roles []roleRow, id uuid.UUID) (roleRow, bool) {
	for _, r := range roles {
		if r.ID == id {
			return r, true
		}
	}
	return roleRow{}, false
}

// leaveOtherRole removes the member's sign-up for another role of the event
// and moves that role's waitlist up. Holding the wanted role already leaves it.
func leaveOtherRole(ctx context.Context, tx pgx.Tx, eventID, userID uuid.UUID, want roleRow, roles []roleRow) ([]Promotion, error) {
	var old uuid.UUID
	err := tx.QueryRow(ctx, `SELECT role_id FROM event_signups WHERE event_id = $1 AND user_id = $2 AND role_id <> $3`, eventID, userID, want.ID).Scan(&old)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM event_signups WHERE event_id = $1 AND user_id = $2`, eventID, userID); err != nil {
		return nil, err
	}
	prev, _ := findRole(roles, old)
	return promoteRole(ctx, tx, eventID, prev)
}

// joinRole adds the member to the role (or reports the sign-up they already have for it).
func joinRole(ctx context.Context, tx pgx.Tx, eventID, userID uuid.UUID, role roleRow) (Signup, error) {
	var state string
	err := tx.QueryRow(ctx, `SELECT state FROM event_signups WHERE event_id = $1 AND user_id = $2 AND role_id = $3`, eventID, userID, role.ID).Scan(&state)
	if err == nil {
		return signupWithPosition(ctx, tx, role.ID, userID, state)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Signup{}, err
	}
	var confirmed int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM event_signups WHERE role_id = $1 AND state = 'confirmed'`, role.ID).Scan(&confirmed); err != nil {
		return Signup{}, err
	}
	state = SignupConfirmed
	if role.Capacity != nil && confirmed >= *role.Capacity {
		state = SignupWaitlist
	}
	if _, err := tx.Exec(ctx, `INSERT INTO event_signups (event_id, user_id, role_id, state, created_at) VALUES ($1, $2, $3, $4, clock_timestamp())`, eventID, userID, role.ID, state); err != nil {
		return Signup{}, fmt.Errorf("sign up: %w", err)
	}
	return signupWithPosition(ctx, tx, role.ID, userID, state)
}

func signupWithPosition(ctx context.Context, tx pgx.Tx, roleID, userID uuid.UUID, state string) (Signup, error) {
	s := Signup{RoleID: roleID, State: state}
	if state != SignupWaitlist {
		return s, nil
	}
	err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM event_signups o, event_signups me
		WHERE me.role_id = $1 AND me.user_id = $2 AND o.role_id = $1 AND o.state = 'waitlist'
		  AND (o.created_at, o.user_id) <= (me.created_at, me.user_id)`, roleID, userID).Scan(&s.WaitlistPosition)
	return s, err
}

func markGoing(ctx context.Context, tx pgx.Tx, eventID, userID uuid.UUID) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO event_rsvps (event_id, user_id, response, created_at, updated_at) VALUES ($1, $2, 'going', NOW(), NOW())
		ON CONFLICT (event_id, user_id) DO UPDATE SET response = 'going', updated_at = NOW()
		WHERE event_rsvps.response <> 'going'`, eventID, userID)
	return err
}

// Leave removes a member's sign-up and moves the waitlist of the role up. It
// reports the role the member left (nil when they had no sign-up). Leaving is
// always allowed, whatever the state of the event; a finished event promotes nobody.
func (s *Store) Leave(ctx context.Context, eventID, userID uuid.UUID) (left *roleRow, promoted []Promotion, err error) {
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		left, promoted, err = removeSignup(ctx, tx, eventID, userID)
		return err
	})
	return left, promoted, err
}

// RemoveSignup is Leave on behalf of a moderator; a finished event is read-only.
func (s *Store) RemoveSignup(ctx context.Context, eventID, userID uuid.UUID) (left *roleRow, promoted []Promotion, err error) {
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		status, err := openEvent(ctx, tx, eventID, false)
		if err != nil {
			return err
		}
		if !isOpen(status) {
			return httpx.ErrConflict("this event is " + status + " and its roster can no longer change")
		}
		left, promoted, err = removeSignup(ctx, tx, eventID, userID)
		return err
	})
	return left, promoted, err
}

func removeSignup(ctx context.Context, tx pgx.Tx, eventID, userID uuid.UUID) (*roleRow, []Promotion, error) {
	status, err := openEvent(ctx, tx, eventID, false)
	if err != nil {
		return nil, nil, err
	}
	roles, err := lockRoles(ctx, tx, eventID)
	if err != nil {
		return nil, nil, err
	}
	var roleID uuid.UUID
	err = tx.QueryRow(ctx, `DELETE FROM event_signups WHERE event_id = $1 AND user_id = $2 RETURNING role_id`, eventID, userID).Scan(&roleID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	role, _ := findRole(roles, roleID)
	if !isOpen(status) {
		return &role, nil, nil
	}
	promoted, err := promoteRole(ctx, tx, eventID, role)
	return &role, promoted, err
}

// PromoteOpenSlots moves waitlisted members up wherever a role of a running or
// upcoming event has room, for example after a member was erased, and returns
// who moved. The sign-up routes promote on their own; this is the safety net.
func (s *Store) PromoteOpenSlots(ctx context.Context) ([]Promotion, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT DISTINCT r.event_id FROM event_roles r JOIN events e ON e.id = r.event_id
		WHERE e.status IN ('scheduled', 'active')
		  AND EXISTS (SELECT 1 FROM event_signups w WHERE w.role_id = r.id AND w.state = 'waitlist')
		  AND (r.capacity IS NULL OR r.capacity > (SELECT COUNT(*) FROM event_signups c WHERE c.role_id = r.id AND c.state = 'confirmed'))`)
	if err != nil {
		return nil, err
	}
	eventIDs, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, err
	}
	var promoted []Promotion
	for _, id := range eventIDs {
		err := s.inTx(ctx, func(tx pgx.Tx) error {
			roles, err := lockRoles(ctx, tx, id)
			if err != nil {
				return err
			}
			for _, r := range roles {
				p, err := promoteRole(ctx, tx, id, r)
				if err != nil {
					return err
				}
				promoted = append(promoted, p...)
			}
			return nil
		})
		if err != nil {
			return promoted, err
		}
	}
	return promoted, nil
}
