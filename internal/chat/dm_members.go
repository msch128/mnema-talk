package chat

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// DMMemberIDs lists the members of a conversation (empty if there is none).
func DMMemberIDs(ctx context.Context, q db.Querier, id uuid.UUID) ([]uuid.UUID, error) {
	rows, err := q.Query(ctx, `SELECT user_id FROM dm_members WHERE channel_id = $1 ORDER BY joined_at, user_id`, id)
	if err != nil {
		return nil, fmt.Errorf("direct message members: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// lockGroupDM locks a conversation the user is in and says whether it is a
// group and how many members it has. Anybody else gets errDMNotFound.
func lockGroupDM(ctx context.Context, tx pgx.Tx, id, user uuid.UUID) (group bool, members int, err error) {
	var member bool
	err = tx.QueryRow(ctx, `
		SELECT c.dm_key IS NULL,
		       EXISTS (SELECT 1 FROM dm_members WHERE channel_id = c.id AND user_id = $2),
		       (SELECT COUNT(*) FROM dm_members WHERE channel_id = c.id)
		FROM channels c WHERE c.id = $1 AND c.type = 'dm' FOR UPDATE OF c`, id, user).Scan(&group, &member, &members)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !member) {
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
		return false, errDMStart
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
		if added, err = checkAddable(ctx, tx, id, adder, target, n); err != nil || !added {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO dm_members (channel_id, user_id, added_by) VALUES ($1, $2, $3)`, id, target, adder)
		return err
	})
	return added, err
}

// LeaveDM takes user out of a group conversation. When nobody is left the
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
	return DMMemberIDs(ctx, tx, id)
}

// deleteEmptyDM removes a conversation without members with its messages and
// returns the storage keys of their attachments.
func deleteEmptyDM(ctx context.Context, tx pgx.Tx, id uuid.UUID) ([]string, error) {
	keys, err := channelMediaKeys(ctx, tx, []uuid.UUID{id})
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM channels WHERE id = $1 AND type = 'dm'`, id); err != nil {
		return nil, fmt.Errorf("delete direct message: %w", err)
	}
	return keys, nil
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
