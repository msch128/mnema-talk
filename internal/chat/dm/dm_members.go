package dm

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// lockGroupDM locks a conversation the user is in and says whether it is a
// group and how many members it has. Anybody else gets errDMNotFound.
func lockGroupDM(ctx context.Context, tx pgx.Tx, id, user uuid.UUID) (group bool, members int, err error) {
	err = tx.QueryRow(ctx, `SELECT dm_key IS NULL FROM channels WHERE id = $1 AND type = 'dm' FOR UPDATE`, id).Scan(&group)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, 0, errDMNotFound
	}
	if err != nil {
		return false, 0, err
	}
	// Membership and size are read by a statement of their own: a statement
	// that waited for the lock above re-checks only the locked row, and a
	// subquery of it would see the members from before the wait (an add or
	// a leave that held the lock meanwhile).
	var member bool
	err = tx.QueryRow(ctx, `
		SELECT COALESCE(bool_or(user_id = $2), FALSE), COUNT(*)
		FROM dm_members WHERE channel_id = $1`, id, user).Scan(&member, &members)
	if err == nil && !member {
		return false, 0, errDMNotFound
	}
	return group, members, err
}

// checkAddable requires target to be new to the conversation, which has n
// members, and eligible for adder; an existing member is not an error (false).
func checkAddable(ctx context.Context, tx pgx.Tx, id, adder, target uuid.UUID, n int) (bool, error) {
	var already bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM dm_members WHERE channel_id = $1 AND user_id = $2)`, id, target).Scan(&already); err != nil || already {
		return false, err
	}
	if n >= MaxDMMembers {
		return false, errDMFull
	}
	var eligible int
	if err := tx.QueryRow(ctx, eligibleSQL, []uuid.UUID{target}, adder, MaxGroupDMsPerMember, true).Scan(&eligible); err != nil {
		return false, err
	}
	if eligible != 1 {
		return false, ErrDMStart
	}
	return true, nil
}

// AddDMMember adds target to a group conversation on behalf of adder, who is
// in it. Refusals about the target are errDMStart.
func AddDMMember(ctx context.Context, p *db.Pool, id, adder, target uuid.UUID) (added bool, err error) {
	err = pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		group, n, err := lockGroupDM(ctx, tx, id, adder)
		if err != nil {
			return err
		}
		if !group {
			return errDMNotGroup
		}
		// The target's group count is checked below; hold their row so a
		// second add cannot count the same number meanwhile.
		if err := LockUsers(ctx, tx, []uuid.UUID{target}); err != nil {
			return err
		}
		if added, err = checkAddable(ctx, tx, id, adder, target, n); err != nil || !added {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO dm_members (channel_id, user_id, added_by) VALUES ($1, $2, $3)`, id, target, adder)
		return err
	})
	return added, err
}

// ServeLeaveDM takes user out of a group conversation. When nobody is left the
// conversation and its messages go, and the storage keys of its attachments
// are returned for the caller to delete. A conversation with one other person
// cannot be left.
func LeaveDM(ctx context.Context, p *db.Pool, id, user uuid.UUID) (remaining []uuid.UUID, keys []string, err error) {
	err = pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		group, _, err := lockGroupDM(ctx, tx, id, user)
		if err != nil {
			return err
		}
		if !group {
			return httpx.ErrInvalidInput("a direct message with one person cannot be left; block them to stop seeing their messages")
		}
		if remaining, err = removeDMMember(ctx, tx, id, user); err != nil || len(remaining) > 0 {
			return err
		}
		keys, err = deleteEmptyDM(ctx, tx, id)
		return err
	})
	return remaining, keys, err
}

// removeDMMember takes user and their read marker out and lists who remains.
func removeDMMember(ctx context.Context, tx pgx.Tx, id, user uuid.UUID) ([]uuid.UUID, error) {
	if _, err := tx.Exec(ctx, `DELETE FROM dm_members WHERE channel_id = $1 AND user_id = $2`, id, user); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM channel_reads WHERE channel_id = $1 AND user_id = $2`, id, user); err != nil {
		return nil, err
	}
	return core.DMMemberIDs(ctx, tx, id)
}

// deleteEmptyDM removes a conversation without members with its messages and
// returns the storage keys of their attachments.
func deleteEmptyDM(ctx context.Context, tx pgx.Tx, id uuid.UUID) ([]string, error) {
	// The conversation's Talks go with it (dm_id cascades): their attachments too.
	ids, err := dmAndTalkIDs(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	keys, err := messages.ChannelMediaKeys(ctx, tx, ids)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM channels WHERE id = $1 AND type = 'dm'`, id); err != nil {
		return nil, fmt.Errorf("delete direct message: %w", err)
	}
	return keys, nil
}

// dmAndTalkIDs is the conversation and the Talks that belong to it.
func dmAndTalkIDs(ctx context.Context, q db.Querier, dm uuid.UUID) ([]uuid.UUID, error) {
	rows, err := q.Query(ctx, `SELECT id FROM channels WHERE id = $1 OR dm_id = $1`, dm)
	if err != nil {
		return nil, fmt.Errorf("list conversation Talks: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// DMPolicy values.
const (
	DMPolicyEveryone = "everyone"
	DMPolicyNobody   = "nobody"
)

// GetDMPolicy returns who may start direct messages with the member.
func GetDMPolicy(ctx context.Context, q db.Querier, user uuid.UUID) (string, error) {
	var policy string
	err := q.QueryRow(ctx, `SELECT dm_policy FROM users WHERE id = $1`, user).Scan(&policy)
	return policy, err
}

// SetDMPolicy stores who may start direct messages with the member.
func SetDMPolicy(ctx context.Context, q db.Querier, user uuid.UUID, policy string) error {
	if policy != DMPolicyEveryone && policy != DMPolicyNobody {
		return httpx.ErrInvalidInput("dm_policy must be everyone or nobody")
	}
	_, err := q.Exec(ctx, `UPDATE users SET dm_policy = $2 WHERE id = $1`, user, policy)
	return err
}
