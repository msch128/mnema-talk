package messages

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// Deleting a message: by its author, by a moderator, and at the end of its
// lifetime. All three remove it in removeLocked.

// DeleteMessageAs removes a message (and its thread replies) if user is its
// author or canManage says they may delete other members' messages (the Manage
// messages permission in the message's channel, or being the Owner). It returns
// the storage keys of the media that went with it; their rows are removed by
// cascade, the caller deletes the objects.
func DeleteMessageAs(ctx context.Context, p *db.Pool, messageID uuid.UUID, user *auth.User, canManage bool) ([]string, error) {
	keys, _, err := deleteMessageAs(ctx, p, messageID, user, canManage, nil)
	return keys, err
}

// deleteMessageAs is DeleteMessageAs with audited, if set, run in the
// deletion's transaction once the message is locked and the deletion allowed,
// while the message can still be read (the audit entry). It also returns when
// the newest of the removed messages (the message or one of its replies) was
// written: members who had read the channel up to then were not counting any.
func deleteMessageAs(ctx context.Context, p *db.Pool, messageID uuid.UUID, user *auth.User, canManage bool, audited func(pgx.Tx) error) ([]string, time.Time, error) {
	var keys []string
	var newest time.Time
	err := pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		// Locking the message first blocks a concurrent thread reply or
		// upload (their foreign keys need a share lock on it) until we are
		// done, so every attachment the cascade removes is in keys.
		m, err := lockMessageForDelete(ctx, tx, messageID)
		if err != nil {
			return err
		}
		newest = m.newest
		if err := checkDeleteAuthorized(user, m.author, m.ownerContent, canManage); err != nil {
			return err
		}
		if audited != nil {
			if err := audited(tx); err != nil {
				return err
			}
		}
		return removeLocked(ctx, p, tx, messageID, m, &keys)
	})
	if err != nil {
		return nil, newest, err
	}
	return keys, newest, nil
}

// removeLocked deletes a message whose row lock tx holds, with its thread
// replies, attachments, reactions and saved entries (by cascade), collects the
// storage keys of the files and records message_delete for the outgoing
// webhooks. An author's delete, a moderator's and the end of a lifetime all
// remove a message here.
func removeLocked(ctx context.Context, p *db.Pool, tx pgx.Tx, messageID uuid.UUID, m lockedMessage, keys *[]string) error {
	if err := CollectKeys(ctx, tx, keys, `
		SELECT s3_key FROM media WHERE message_id IN
			(SELECT id FROM messages WHERE id = $1 OR parent_id = $1)`, messageID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM messages WHERE id = $1`, messageID); err != nil {
		return fmt.Errorf("delete message: %w", err)
	}
	return RecordMessageDelete(ctx, p, tx, m.channel, messageID, m.parent)
}

// lockedMessage is what deleting a message needs to know about it.
type lockedMessage struct {
	author, channel uuid.UUID
	parent          *uuid.UUID
	ownerContent    bool
	// newest is when the newest of the message and its replies was written,
	// read once the lock is held (no reply can be added after that).
	newest time.Time
}

// lockMessageForDelete takes the author's row (FOR KEY SHARE) before the
// message's. An erasure of the author holds that row FOR UPDATE while it
// removes the person's reactions, mentions and files and then empties their
// messages; a delete that held the message first would wait for those rows
// and the erasure for the message, a cycle PostgreSQL breaks by failing one of
// them. In this order the delete waits for the erasure, or the erasure for
// the delete. The author of a message never changes, so it is read unlocked.
func lockMessageForDelete(ctx context.Context, tx pgx.Tx, messageID uuid.UUID) (lockedMessage, error) {
	var m lockedMessage
	if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id = (SELECT user_id FROM messages WHERE id = $1) FOR KEY SHARE`, messageID); err != nil {
		return m, fmt.Errorf("lock author: %w", err)
	}
	err := tx.QueryRow(ctx, `SELECT user_id, channel_id, parent_id FROM messages WHERE id = $1 FOR UPDATE`, messageID).
		Scan(&m.author, &m.channel, &m.parent)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, core.ErrMessageNotFound
	}
	if err != nil {
		return m, fmt.Errorf("lock message: %w", err)
	}
	// The Owner's words, read after the lock in a statement of its own
	// (ownerWords), then the newest message the delete removes.
	if m.ownerContent, err = ownerWords(ctx, tx, messageID, m.author); err != nil {
		return m, err
	}
	m.newest, err = newestOfThread(ctx, tx, messageID)
	return m, err
}

func checkDeleteAuthorized(user *auth.User, author uuid.UUID, ownerContent, canManage bool) error {
	if author != user.ID && !canManage {
		return httpx.ErrForbidden("only the author or a moderator can delete this message")
	}
	if ownerContent && !user.IsAdmin() && author != user.ID {
		return httpx.ErrForbidden("the Owner's messages can only be deleted by the Owner")
	}
	return nil
}

// ExpiredDelete is what the deletion of an ended message removed.
type ExpiredDelete struct {
	Channel uuid.UUID
	Parent  *uuid.UUID
	// Newest is when the newest of the message and its replies was written.
	Newest time.Time
	// Keys are the storage keys of the files that went with it.
	Keys []string
}

// DeleteExpired removes the message if its lifetime has ended, as its
// author's own delete would (removeLocked): no audit entry, as for an own
// delete. It returns nil when the message is gone already or has not ended.
//
// The row is locked first (a reply, an upload or a pin waits for it), and
// the end is read in a statement of its own after the lock is held.
//
// The lock order of lockMessageForDelete keeps it out of a cycle with the
// erasure of the author; a deadlock with anything else is tried again a few
// times, as the other side has committed by then.
func DeleteExpired(ctx context.Context, p *db.Pool, id uuid.UUID) (*ExpiredDelete, error) {
	for attempt := 1; ; attempt++ {
		d, err := deleteExpiredOnce(ctx, p, id)
		if !IsDeadlock(err) || attempt == maxDeadlockRetries {
			return d, err
		}
	}
}

// maxDeadlockRetries bounds how often DeleteExpired tries after a deadlock.
const maxDeadlockRetries = 3

// IsDeadlock reports whether err is PostgreSQL aborting the transaction to
// break a lock cycle (SQLSTATE 40P01).
func IsDeadlock(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "40P01"
}

func deleteExpiredOnce(ctx context.Context, p *db.Pool, id uuid.UUID) (*ExpiredDelete, error) {
	var out *ExpiredDelete
	err := pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		m, err := lockMessageForDelete(ctx, tx, id)
		if errors.Is(err, core.ErrMessageNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		var ended bool
		if err := tx.QueryRow(ctx, `SELECT COALESCE(expires_at <= clock_timestamp(), FALSE) FROM messages WHERE id = $1`, id).Scan(&ended); err != nil {
			return fmt.Errorf("read lifetime: %w", err)
		}
		if !ended {
			return nil
		}
		d := &ExpiredDelete{Channel: m.channel, Parent: m.parent, Newest: m.newest}
		if err := removeLocked(ctx, p, tx, id, m, &d.Keys); err != nil {
			return err
		}
		out = d
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
