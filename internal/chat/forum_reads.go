package chat

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
)

// maxUnreadPosts bounds the list of unread posts a client loads at once.
const maxUnreadPosts = 200

// UnreadPostIDs lists the posts of channelID with something the member has not
// read: the post itself or a reply of someone else (not someone they blocked)
// newer than their last read of the post. A post they never opened counts from
// the day they joined. Newest activity first.
func UnreadPostIDs(ctx context.Context, p *db.Pool, channelID, userID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := p.Query(ctx, `
		SELECT m.id
		FROM messages m
		JOIN users me ON me.id = $2
		LEFT JOIN forum_post_reads r ON r.post_id = m.id AND r.user_id = $2
		CROSS JOIN LATERAL (
		    SELECT MAX(x.created_at) AS at FROM (
		        SELECT m.created_at, m.user_id
		        UNION ALL SELECT c.created_at, c.user_id FROM messages c WHERE c.parent_id = m.id
		    ) x
		    WHERE x.user_id <> $2
		      AND NOT EXISTS (SELECT 1 FROM user_blocks ub WHERE ub.blocker_id = $2 AND ub.blocked_id = x.user_id)
		      AND x.created_at > COALESCE(r.last_read_at, me.created_at)
		) news
		WHERE m.channel_id = $1 AND m.parent_id IS NULL AND news.at IS NOT NULL
		ORDER BY news.at DESC, m.id DESC
		LIMIT $3`, channelID, userID, maxUnreadPosts)
	if err != nil {
		return nil, fmt.Errorf("unread posts: %w", err)
	}
	defer rows.Close()
	ids := make([]uuid.UUID, 0)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// MarkPostRead sets the member's read marker of a post to now and returns the
// marker it had before (the day they joined when the post was never opened),
// which is where "jump to the first unread reply" starts. The marker never
// moves backwards.
func MarkPostRead(ctx context.Context, p *db.Pool, userID, postID uuid.UUID) (previous time.Time, err error) {
	err = p.QueryRow(ctx, `
		WITH old AS (SELECT last_read_at FROM forum_post_reads WHERE user_id = $1 AND post_id = $2),
		written AS (
		    INSERT INTO forum_post_reads (user_id, post_id, last_read_at) VALUES ($1, $2, NOW())
		    ON CONFLICT (user_id, post_id) DO UPDATE SET last_read_at = GREATEST(forum_post_reads.last_read_at, EXCLUDED.last_read_at)
		)
		SELECT COALESCE((SELECT last_read_at FROM old), (SELECT created_at FROM users WHERE id = $1))`, userID, postID).Scan(&previous)
	if err != nil {
		return time.Time{}, fmt.Errorf("mark post read: %w", err)
	}
	return previous, nil
}
