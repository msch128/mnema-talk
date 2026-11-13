package channels

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/chat/readstate"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/events/wire"
	"github.com/msch128/mnema-talk/internal/worker"
)

const (
	// autoDeleteBatch is how many messages (or files) one transaction removes.
	autoDeleteBatch = 200
	// autoDeleteBatchesPerRun bounds one channel's work per sweep, so a huge
	// backlog is worked off over several sweeps without hogging the database.
	autoDeleteBatchesPerRun = 25
)

// AutoDeleter removes the messages, or only the files, that outlived their
// channel's auto-delete period.
type AutoDeleter struct {
	DB     *db.Pool
	Events events.Publisher
	// Objects deletes the attachments' stored files; nil leaves them.
	Objects core.ObjectDeleter
	// Audit records one entry per channel and run; nil records nothing.
	Audit *audit.Log
	// Recount tells the members who had not read the removed messages their
	// counts again, once per channel and run; nil tells nobody.
	Recount *readstate.Recounter
	// Next is when the run after now is; nil runs at NextAutoDeleteRun.
	Next func(now time.Time) time.Time
}

func (a *AutoDeleter) next(now time.Time) time.Time {
	if a.Next != nil {
		return a.Next(now)
	}
	return NextAutoDeleteRun(now)
}

type autoDeleteChannel struct {
	id    uuid.UUID
	name  string
	hours int
	scope string
	// followsDefault: the period is the community default's.
	followsDefault bool
}

// Run sweeps at every multiple of AutoDeleteEvery (or as Next says) until
// ctx ends.
func (a *AutoDeleter) Run(ctx context.Context) {
	t := time.NewTimer(time.Until(a.next(time.Now())))
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			worker.Tick(worker.JobAutoDelete, func() error {
				// An online backup freezes the storage: wait for it to end.
				err := a.DB.OutsideBackup(ctx, func() error { return a.Sweep(ctx) })
				if err != nil && ctx.Err() == nil {
					slog.Error("auto-delete sweep failed", "err", err)
				}
				return worker.IgnoreDone(ctx, err)
			})
			t.Reset(time.Until(a.next(time.Now())))
		}
	}
}

// Sweep runs one pass over all channels that have auto-delete on, by their
// own setting or the community default.
func (a *AutoDeleter) Sweep(ctx context.Context) error {
	ctx = db.WithStatementTimeout(ctx, db.BulkStatementTimeout)
	rows, err := a.DB.Query(ctx, `
		SELECT id, name, hours, scope, follows
		FROM (SELECT id, name, `+core.AutoDeleteSQL("channels")+` FROM channels) c(id, name, hours, scope, follows)
		WHERE hours > 0`)
	if err != nil {
		return fmt.Errorf("list auto-delete channels: %w", err)
	}
	chans, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (autoDeleteChannel, error) {
		var c autoDeleteChannel
		return c, r.Scan(&c.id, &c.name, &c.hours, &c.scope, &c.followsDefault)
	})
	if err != nil {
		return fmt.Errorf("list auto-delete channels: %w", err)
	}
	// A channel that fails does not stop the others, but the run counts as failed.
	var failed []error
	for _, c := range chans {
		if err := a.sweepChannel(ctx, c); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			slog.Error("auto-delete channel failed", "channel", c.id, "err", err)
			failed = append(failed, fmt.Errorf("channel %s: %w", c.id, err))
		}
	}
	return errors.Join(failed...)
}

func (a *AutoDeleter) sweepChannel(ctx context.Context, c autoDeleteChannel) error {
	cutoff := time.Now().Add(-time.Duration(c.hours) * time.Hour)
	batch := a.sweepBatch
	if c.scope == core.AutoDeleteScopeAttachments {
		batch = a.purgeFilesBatch
	}
	var messages, attachments int
	var failure error
	for i := 0; i < autoDeleteBatchesPerRun; i++ {
		n, files, err := batch(ctx, c, cutoff)
		messages += n
		attachments += files
		if err != nil {
			failure = err
			break
		}
		if max(n, files) < autoDeleteBatch {
			break
		}
	}
	if messages == 0 && attachments == 0 {
		return failure
	}
	if c.scope == core.AutoDeleteScopeAll {
		// Every removed message and every reply that went with it is older than the cutoff.
		a.Recount.Tell(c.id, &cutoff, nil)
	}
	a.Audit.RecordOrLog(ctx, audit.Entry{
		Action: "channel.auto_delete_run", TargetType: "channel", TargetID: c.id.String(), TargetName: c.name, ChannelID: &c.id,
		Details: map[string]any{
			"hours": c.hours, "scope": c.scope, "default": c.followsDefault, "days": core.AutoDeleteDays(c.hours, core.AutoDeleteScopeAll),
			"messages": messages, "attachments": attachments, "complete": failure == nil,
		},
	})
	return failure
}

// sweepBatch removes up to autoDeleteBatch expired messages of the channel.
// Pinned messages stay, and so does a thread root that still has a reply
// younger than the cutoff or a pinned one. The channel's setting is checked
// again inside the transaction, so turning auto-delete off or changing it
// stops a run at once.
func (a *AutoDeleter) sweepBatch(ctx context.Context, c autoDeleteChannel, cutoff time.Time) (int, int, error) {
	var removed []expiredMessage
	var keys []string
	err := pgx.BeginFunc(ctx, a.DB, func(tx pgx.Tx) error {
		if set, err := stillSet(ctx, tx, c); err != nil || !set {
			return err
		}
		var err error
		removed, err = selectExpired(ctx, tx, c, cutoff)
		if err != nil || len(removed) == 0 {
			return err
		}
		if err := deleteExpired(ctx, tx, removed, &keys); err != nil {
			return err
		}
		return a.recordExpired(ctx, tx, c.id, removed)
	})
	if err != nil {
		return 0, 0, err
	}
	core.DeleteObjects(ctx, a.Objects, keys)
	if a.Events != nil {
		for _, g := range removed {
			a.Events.BroadcastChannel(c.id, wire.MessageDelete{ID: g.id, ChannelID: c.id, ParentID: g.parent})
		}
	}
	return len(removed), len(keys), nil
}

// recordExpired stores message_delete of the removed messages for the
// channel's outgoing webhooks, in the transaction that removed them.
func (a *AutoDeleter) recordExpired(ctx context.Context, tx pgx.Tx, channel uuid.UUID, removed []expiredMessage) error {
	deleted := make([]wire.MessageDelete, len(removed))
	for i, g := range removed {
		deleted[i] = wire.MessageDelete{ID: g.id, ChannelID: channel, ParentID: g.parent}
	}
	return messages.RecordMessageDeletes(ctx, a.DB, tx, channel, deleted)
}

type expiredMessage struct {
	id     uuid.UUID
	parent *uuid.UUID
}

// selectExpired locks the next batch of expired messages. Locking them blocks
// a concurrent reply or upload (their foreign keys need a share lock), so
// every attachment the delete removes is in the key list. The thread roots
// that must stay are then left out by dropKeptThreads.
func selectExpired(ctx context.Context, tx pgx.Tx, c autoDeleteChannel, cutoff time.Time) ([]expiredMessage, error) {
	rows, err := tx.Query(ctx, `
		SELECT m.id, m.parent_id FROM messages m
		WHERE m.channel_id = $1 AND m.created_at < $2 AND NOT m.is_pinned
		  AND NOT EXISTS (SELECT 1 FROM messages r WHERE r.parent_id = m.id AND (r.created_at >= $2 OR r.is_pinned))
		ORDER BY m.created_at
		LIMIT $3
		FOR UPDATE OF m SKIP LOCKED`, c.id, cutoff, autoDeleteBatch)
	if err != nil {
		return nil, fmt.Errorf("select expired messages: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (expiredMessage, error) {
		var g expiredMessage
		return g, r.Scan(&g.id, &g.parent)
	})
	if err != nil {
		return nil, fmt.Errorf("select expired messages: %w", err)
	}
	if len(out) == 0 {
		return out, nil
	}
	return dropKeptThreads(ctx, tx, out, cutoff)
}

// stillSet holds the channel's row and the community settings with a share
// lock for the batch (a change of the setting waits for it), then reads in a
// statement of its own whether the channel is still set to the run's period
// and scope: turning auto-delete off or changing it stops a run at once.
func stillSet(ctx context.Context, tx pgx.Tx, c autoDeleteChannel) (bool, error) {
	if _, err := tx.Exec(ctx, `SELECT 1 FROM channels WHERE id = $1 FOR SHARE`, c.id); err != nil {
		return false, fmt.Errorf("lock channel: %w", err)
	}
	// The settings row too: a change of the default waits for the batch.
	// No cycle: no transaction updates instance_settings and then a channel
	// or a message, and the batch takes this row after the channel's and
	// before any message's. A change of another setting waits for the short
	// batch as well (at most 200 rows).
	if _, err := tx.Exec(ctx, `SELECT 1 FROM instance_settings FOR SHARE`); err != nil {
		return false, fmt.Errorf("lock settings: %w", err)
	}
	var hours int
	var scope string
	var follows bool
	err := tx.QueryRow(ctx, `SELECT `+core.AutoDeleteSQL("c")+` FROM channels c WHERE c.id = $1`, c.id).Scan(&hours, &scope, &follows)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read auto-delete: %w", err)
	}
	return hours == c.hours && scope == c.scope, nil
}

// dropKeptThreads leaves out the locked roots that now have a reply younger
// than the cutoff or a pinned one. The NOT EXISTS of selectExpired reads the
// statement's snapshot, so it misses a reply or pin that committed after the
// snapshot and before the root's lock was taken; this statement starts after
// the locks are held. Once a root is locked, neither can be added: a reply's
// insert and a pin of a reply take a key-share lock on the root first and wait
// for the sweep.
func dropKeptThreads(ctx context.Context, tx pgx.Tx, removed []expiredMessage, cutoff time.Time) ([]expiredMessage, error) {
	ids := make([]uuid.UUID, len(removed))
	for i, g := range removed {
		ids[i] = g.id
	}
	rows, err := tx.Query(ctx, `
		SELECT DISTINCT parent_id FROM messages
		WHERE parent_id = ANY($1) AND (created_at >= $2 OR is_pinned)`, ids, cutoff)
	if err != nil {
		return nil, fmt.Errorf("check kept threads: %w", err)
	}
	kept, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("check kept threads: %w", err)
	}
	return slices.DeleteFunc(removed, func(g expiredMessage) bool { return slices.Contains(kept, g.id) }), nil
}

func deleteExpired(ctx context.Context, tx pgx.Tx, removed []expiredMessage, keys *[]string) error {
	ids := make([]uuid.UUID, len(removed))
	for i, g := range removed {
		ids[i] = g.id
	}
	// A file removed before (is_deleted) has no object left and is not counted.
	if err := messages.CollectKeys(ctx, tx, keys, `
		SELECT s3_key FROM media WHERE NOT is_deleted AND message_id IN
			(SELECT id FROM messages WHERE id = ANY($1) OR parent_id = ANY($1))`, ids); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM messages WHERE id = ANY($1)`, ids); err != nil {
		return fmt.Errorf("delete expired messages: %w", err)
	}
	return nil
}
