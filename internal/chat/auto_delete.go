package chat

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// AutoDeleteChoices are the retention periods a channel can be set to, in
// days. 0 turns auto-delete off.
var AutoDeleteChoices = []int{0, 1, 7, 30, 90}

const (
	// autoDeleteBatch is how many messages one transaction removes.
	autoDeleteBatch = 200
	// autoDeleteBatchesPerRun bounds one channel's work per sweep, so a huge
	// backlog is worked off over several sweeps without hogging the database.
	autoDeleteBatchesPerRun = 25
	// AutoDeleteEvery is how often the sweeper runs.
	AutoDeleteEvery = 10 * time.Minute
)

func validAutoDeleteDays(days int) bool {
	for _, c := range AutoDeleteChoices {
		if c == days {
			return true
		}
	}
	return false
}

// SetAutoDelete changes how long a channel keeps its messages (0 = forever).
// Direct messages never take part. It returns the channel as it is now.
func SetAutoDelete(ctx context.Context, p *db.Pool, id uuid.UUID, days int) (*Channel, error) {
	if !validAutoDeleteDays(days) {
		return nil, httpx.ErrInvalidInput("days must be 0 (off), 1, 7, 30 or 90")
	}
	var ch Channel
	err := p.QueryRow(ctx, `
		UPDATE channels SET auto_delete_days = $2 WHERE id = $1 AND type <> 'dm'
		RETURNING id, number, category_id, name, type, topic, sort_order, created_at, user_limit, slow_mode_seconds, auto_delete_days`, id, days).
		Scan(&ch.ID, &ch.Number, &ch.CategoryID, &ch.Name, &ch.Type, &ch.Topic, &ch.SortOrder, &ch.CreatedAt, &ch.UserLimit, &ch.SlowModeSeconds, &ch.AutoDeleteDays)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errChannelNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("set auto-delete: %w", err)
	}
	if err := FillTalkFields(ctx, p, &ch); err != nil {
		return nil, err
	}
	if err := FillSlugs(ctx, p, &ch); err != nil {
		return nil, err
	}
	return &ch, nil
}

// AutoDeleter removes the messages that outlived their channel's retention.
type AutoDeleter struct {
	DB     *db.Pool
	Events events.Publisher
	// Objects deletes the attachments' stored files; nil leaves them.
	Objects ObjectDeleter
	// Audit records one entry per channel and run; nil records nothing.
	Audit *audit.Log
}

type autoDeleteChannel struct {
	id   uuid.UUID
	name string
	days int
}

// Run sweeps every interval until ctx ends.
func (a *AutoDeleter) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := a.Sweep(ctx); err != nil && ctx.Err() == nil {
				slog.Error("auto-delete sweep failed", "err", err)
			}
		}
	}
}

// Sweep runs one pass over all channels that have auto-delete on.
func (a *AutoDeleter) Sweep(ctx context.Context) error {
	rows, err := a.DB.Query(ctx, `SELECT id, name, auto_delete_days FROM channels WHERE auto_delete_days > 0 AND type <> 'dm'`)
	if err != nil {
		return fmt.Errorf("list auto-delete channels: %w", err)
	}
	chans, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (autoDeleteChannel, error) {
		var c autoDeleteChannel
		return c, r.Scan(&c.id, &c.name, &c.days)
	})
	if err != nil {
		return fmt.Errorf("list auto-delete channels: %w", err)
	}
	for _, c := range chans {
		if err := a.sweepChannel(ctx, c); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			slog.Error("auto-delete channel failed", "channel", c.id, "err", err)
		}
	}
	return nil
}

func (a *AutoDeleter) sweepChannel(ctx context.Context, c autoDeleteChannel) error {
	cutoff := time.Now().Add(-time.Duration(c.days) * 24 * time.Hour)
	var messages, attachments int
	var failure error
	for i := 0; i < autoDeleteBatchesPerRun; i++ {
		n, files, err := a.sweepBatch(ctx, c, cutoff)
		messages += n
		attachments += files
		if err != nil {
			failure = err
			break
		}
		if n < autoDeleteBatch {
			break
		}
	}
	if messages > 0 {
		a.Audit.RecordOrLog(ctx, audit.Entry{
			Action: "channel.auto_delete_run", TargetType: "channel", TargetID: c.id.String(), TargetName: c.name, ChannelID: &c.id,
			Details: map[string]any{"days": c.days, "messages": messages, "attachments": attachments, "complete": failure == nil},
		})
	}
	return failure
}

// sweepBatch removes up to autoDeleteBatch expired messages of the channel.
// Pinned messages stay, and so does a thread root that still has a reply
// younger than the cutoff or a pinned one. The channel's setting is checked
// again inside the transaction, so turning auto-delete off stops a run at once.
func (a *AutoDeleter) sweepBatch(ctx context.Context, c autoDeleteChannel, cutoff time.Time) (int, int, error) {
	var removed []expiredMessage
	var keys []string
	err := pgx.BeginFunc(ctx, a.DB, func(tx pgx.Tx) error {
		var err error
		removed, err = selectExpired(ctx, tx, c, cutoff)
		if err != nil || len(removed) == 0 {
			return err
		}
		return deleteExpired(ctx, tx, removed, &keys)
	})
	if err != nil {
		return 0, 0, err
	}
	if a.Objects != nil && len(keys) > 0 {
		if err := a.Objects.DeleteBatch(ctx, keys); err != nil {
			slog.Warn("auto-delete: delete media objects", "count", len(keys), "err", err)
		}
	}
	if a.Events != nil {
		for _, g := range removed {
			a.Events.BroadcastChannel(c.id, events.MessageDelete, map[string]any{"id": g.id, "channel_id": c.id, "parent_id": g.parent})
		}
	}
	return len(removed), len(keys), nil
}

type expiredMessage struct {
	id     uuid.UUID
	parent *uuid.UUID
}

// selectExpired locks the next batch of expired messages. Locking them blocks
// a concurrent reply or upload (their foreign keys need a share lock), so
// every attachment the delete removes is in the key list.
func selectExpired(ctx context.Context, tx pgx.Tx, c autoDeleteChannel, cutoff time.Time) ([]expiredMessage, error) {
	rows, err := tx.Query(ctx, `
		SELECT m.id, m.parent_id FROM messages m
		WHERE m.channel_id = $1 AND m.created_at < $2 AND NOT m.is_pinned
		  AND NOT EXISTS (SELECT 1 FROM messages r WHERE r.parent_id = m.id AND (r.created_at >= $2 OR r.is_pinned))
		  AND EXISTS (SELECT 1 FROM channels c WHERE c.id = m.channel_id AND c.auto_delete_days = $3 AND c.type <> 'dm')
		ORDER BY m.created_at
		LIMIT $4
		FOR UPDATE OF m SKIP LOCKED`, c.id, cutoff, c.days, autoDeleteBatch)
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
	return out, nil
}

func deleteExpired(ctx context.Context, tx pgx.Tx, removed []expiredMessage, keys *[]string) error {
	ids := make([]uuid.UUID, len(removed))
	for i, g := range removed {
		ids[i] = g.id
	}
	if err := collectKeys(ctx, tx, keys, `
		SELECT s3_key FROM media WHERE message_id IN
			(SELECT id FROM messages WHERE id = ANY($1) OR parent_id = ANY($1))`, ids); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM messages WHERE id = ANY($1)`, ids); err != nil {
		return fmt.Errorf("delete expired messages: %w", err)
	}
	return nil
}
