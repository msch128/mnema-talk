package communityevents

import (
	"context"

	"github.com/google/uuid"
)

// fillRoles adds the roles (with their counts) of the events and the caller's
// own sign-up to each of them.
func (s *Store) fillRoles(ctx context.Context, byID map[uuid.UUID]*CommunityEvent, ids []uuid.UUID, callerID *uuid.UUID) error {
	for _, ev := range byID {
		ev.Roles = []EventRole{}
	}
	rows, err := s.DB.Query(ctx, `
		SELECT r.event_id, r.id, r.name, r.emoji, r.capacity, r.position,
		       COUNT(s.user_id) FILTER (WHERE s.state = 'confirmed'),
		       COUNT(s.user_id) FILTER (WHERE s.state = 'waitlist')
		FROM event_roles r LEFT JOIN event_signups s ON s.role_id = r.id
		WHERE r.event_id = ANY($1)
		GROUP BY r.id
		ORDER BY r.event_id, r.position, r.name`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var eventID uuid.UUID
		var r EventRole
		if err := rows.Scan(&eventID, &r.ID, &r.Name, &r.Emoji, &r.Capacity, &r.Position, &r.Confirmed, &r.Waitlist); err != nil {
			return err
		}
		if ev := byID[eventID]; ev != nil {
			ev.Roles = append(ev.Roles, r)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return s.fillMySignups(ctx, byID, ids, callerID)
}

func (s *Store) fillMySignups(ctx context.Context, byID map[uuid.UUID]*CommunityEvent, ids []uuid.UUID, callerID *uuid.UUID) error {
	if callerID == nil {
		return nil
	}
	rows, err := s.DB.Query(ctx, `
		SELECT me.event_id, me.role_id, me.state,
		       CASE WHEN me.state = 'waitlist' THEN (
		           SELECT COUNT(*) FROM event_signups o
		           WHERE o.role_id = me.role_id AND o.state = 'waitlist' AND (o.created_at, o.user_id) <= (me.created_at, me.user_id)
		       ) ELSE 0 END
		FROM event_signups me WHERE me.event_id = ANY($1) AND me.user_id = $2`, ids, *callerID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var eventID uuid.UUID
		var sg Signup
		if err := rows.Scan(&eventID, &sg.RoleID, &sg.State, &sg.WaitlistPosition); err != nil {
			return err
		}
		if ev := byID[eventID]; ev != nil {
			ev.MySignup = &sg
		}
	}
	return rows.Err()
}

// fillSignups lists who holds or awaits which role of one event: confirmed
// members first, the waitlist in the order it moves up.
func (s *Store) fillSignups(ctx context.Context, ev *CommunityEvent) error {
	rows, err := s.DB.Query(ctx, `
		SELECT u.id, u.username, u.display_name, u.avatar_s3_key, s.role_id, s.state, s.created_at
		FROM event_signups s JOIN users u ON u.id = s.user_id
		WHERE s.event_id = $1
		ORDER BY s.state, s.created_at, u.id`, ev.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	ev.Signups = []SignupItem{}
	for rows.Next() {
		var it SignupItem
		var avatarKey *string
		if err := rows.Scan(&it.User.ID, &it.User.Username, &it.User.DisplayName, &avatarKey, &it.RoleID, &it.State, &it.CreatedAt); err != nil {
			return err
		}
		it.User = newUserRef(it.User.ID, it.User.Username, it.User.DisplayName, avatarKey)
		ev.Signups = append(ev.Signups, it)
	}
	return rows.Err()
}
