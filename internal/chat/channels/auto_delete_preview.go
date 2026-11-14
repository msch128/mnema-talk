package channels

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/db"
)

// AutoDeletePreview is what a setting would remove of what exists now, over
// its next runs (one run removes at most 5,000 messages or files per channel;
// a backlog takes several): the answer of the dry-run routes, so the settings
// can ask before content goes. Files removed before are not counted.
type AutoDeletePreview struct {
	// Messages is how many messages would be deleted, thread replies that go
	// with their root included; 0 for the scope "attachments".
	Messages int `json:"messages"`
	// Attachments is how many files would be removed.
	Attachments int `json:"attachments"`
	// NextRunAt is when the sweeper runs next.
	NextRunAt time.Time `json:"next_run_at" format:"date-time"`
}

// PreviewAutoDelete counts what a period of hours with scope would remove in
// the channels now. hours 0 removes nothing.
func PreviewAutoDelete(ctx context.Context, q db.Querier, chans []uuid.UUID, hours int, scope string) (AutoDeletePreview, error) {
	out := AutoDeletePreview{NextRunAt: NextAutoDeleteRun(time.Now())}
	if hours <= 0 || len(chans) == 0 {
		return out, nil
	}
	cutoff := time.Now().Add(-time.Duration(hours) * time.Hour)
	ctx = db.WithStatementTimeout(ctx, db.BulkStatementTimeout)
	var err error
	if scope == core.AutoDeleteScopeAttachments {
		err = q.QueryRow(ctx, `
			SELECT COUNT(*) FROM media md JOIN messages m ON m.id = md.message_id
			WHERE m.channel_id = ANY($1) AND m.created_at < $2 AND NOT m.is_pinned AND NOT md.is_deleted`, chans, cutoff).Scan(&out.Attachments)
	} else {
		err = q.QueryRow(ctx, `
			WITH gone AS (
				SELECT m.id FROM messages m
				WHERE m.channel_id = ANY($1) AND m.created_at < $2 AND NOT m.is_pinned
				  AND NOT EXISTS (SELECT 1 FROM messages r WHERE r.parent_id = m.id AND (r.created_at >= $2 OR r.is_pinned))
			), every AS (
				SELECT id FROM gone UNION SELECT r.id FROM messages r JOIN gone g ON r.parent_id = g.id
			)
			SELECT (SELECT COUNT(*) FROM every), (SELECT COUNT(*) FROM media WHERE NOT is_deleted AND message_id IN (SELECT id FROM every))`,
			chans, cutoff).Scan(&out.Messages, &out.Attachments)
	}
	if err != nil {
		return out, fmt.Errorf("preview auto-delete: %w", err)
	}
	return out, nil
}

// channelsFollowingDefault lists the community channels without an
// auto-delete setting of their own.
func channelsFollowingDefault(ctx context.Context, q db.Querier) ([]uuid.UUID, error) {
	rows, err := q.Query(ctx, `SELECT id FROM channels c WHERE auto_delete_hours IS NULL AND NOT `+core.PrivateChannelSQL("c"))
	if err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
