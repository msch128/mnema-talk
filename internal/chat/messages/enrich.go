package messages

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/mnema-xyz/talk/internal/chat/core"
	"github.com/mnema-xyz/talk/internal/chat/mentions"
	"github.com/mnema-xyz/talk/internal/db"
)

// Enrich loads attachments, reactions, mentions, polls and components for msgs.
// Its queries go to the database together, in one round trip (a pipelined
// batch): a post reads its new message this way while it holds its channel's
// row (insertMessageTx), and every round trip there is one more wait for the
// next post in the channel.
func Enrich(ctx context.Context, p db.Batcher, msgs []core.Message) error {
	if len(msgs) == 0 {
		return nil
	}
	idx := make(map[uuid.UUID]int, len(msgs))
	ids := make([]uuid.UUID, len(msgs))
	for i, m := range msgs {
		idx[m.ID] = i
		ids[i] = m.ID
	}
	b := &pgx.Batch{}
	for _, queue := range []func(context.Context, *pgx.Batch, []core.Message, map[uuid.UUID]int, []uuid.UUID){
		queueAttachments, queueReactions, queueMentions, mentions.QueueMentionRoles, queuePolls, queueForumPostDetails, core.QueueComponents,
	} {
		queue(ctx, b, msgs, idx, ids)
	}
	if err := p.SendBatch(ctx, b).Close(); err != nil {
		return fmt.Errorf("load message details: %w", err)
	}
	return nil
}

const reactionsSQL = `
		SELECT message_id, MIN(emoji), array_agg(user_id ORDER BY created_at)
		FROM message_reactions WHERE message_id = ANY($1)
		GROUP BY message_id, ` + reactionKeySQL + ` ORDER BY MIN(created_at)`

func loadReactions(ctx context.Context, p db.Querier, msgs []core.Message, idx map[uuid.UUID]int, ids []uuid.UUID) error {
	rows, err := p.Query(ctx, reactionsSQL, ids)
	if err != nil {
		return fmt.Errorf("load reactions: %w", err)
	}
	defer rows.Close()
	return scanReactions(rows, msgs, idx)
}

func queueReactions(_ context.Context, b *pgx.Batch, msgs []core.Message, idx map[uuid.UUID]int, ids []uuid.UUID) {
	b.Queue(reactionsSQL, ids).Query(labeled("load reactions", func(rows pgx.Rows) error { return scanReactions(rows, msgs, idx) }))
}

// labeled names the loader in the errors of a queued query of Enrich. A query
// the database refuses outright fails the whole batch before any of them runs
// (Enrich's "load message details").
func labeled(what string, scan func(pgx.Rows) error) func(pgx.Rows) error {
	return func(rows pgx.Rows) error {
		if err := scan(rows); err != nil {
			return fmt.Errorf("%s: %w", what, err)
		}
		return nil
	}
}

func scanReactions(rows pgx.Rows, msgs []core.Message, idx map[uuid.UUID]int) error {
	for rows.Next() {
		var r core.ReactionSummary
		var msgID uuid.UUID
		if err := rows.Scan(&msgID, &r.Emoji, &r.Users); err != nil {
			return err
		}
		msgs[idx[msgID]].Reactions = append(msgs[idx[msgID]].Reactions, oneReaction(r))
	}
	return rows.Err()
}

func queueMentions(_ context.Context, b *pgx.Batch, msgs []core.Message, idx map[uuid.UUID]int, ids []uuid.UUID) {
	b.Queue(`SELECT message_id, user_id, kind <> $2 FROM message_mentions WHERE message_id = ANY($1)`, ids, core.MentionKindGroup).
		Query(labeled("load mentions", func(rows pgx.Rows) error { return scanMentions(rows, msgs, idx) }))
}

func scanMentions(rows pgx.Rows, msgs []core.Message, idx map[uuid.UUID]int) error {
	for rows.Next() {
		var msgID, userID uuid.UUID
		var direct bool
		if err := rows.Scan(&msgID, &userID, &direct); err != nil {
			return err
		}
		m := &msgs[idx[msgID]]
		m.Mentions = append(m.Mentions, userID)
		if direct {
			m.DirectMentions = append(m.DirectMentions, userID)
		}
	}
	return rows.Err()
}

// queueForumPostDetails queues the queries that enrich messages with forum
// post titles, lock states, tags, and last activity timestamps. The tags
// come second: a post's title makes its tag list non-nil.
func queueForumPostDetails(_ context.Context, b *pgx.Batch, msgs []core.Message, idx map[uuid.UUID]int, ids []uuid.UUID) {
	b.Queue(`
		SELECT m.id, m.title, m.is_locked, m.is_resolved,
		       (SELECT MAX(r.created_at) FROM messages r WHERE r.parent_id = m.id AND (r.expires_at IS NULL OR r.expires_at > now())) AS last_reply_at
		FROM messages m
		WHERE m.id = ANY($1) AND m.parent_id IS NULL AND (m.title IS NOT NULL OR m.is_locked OR m.is_resolved)`, ids).
		Query(labeled("load forum post details", func(rows pgx.Rows) error { return scanPostTitlesAndLocks(rows, msgs, idx) }))
	b.Queue(`
		SELECT pt.message_id, ft.id, ft.channel_id, ft.name, ft.emoji, ft.color, ft.position, ft.moderated, ft.created_at
		FROM forum_post_tags pt
		JOIN forum_tags ft ON ft.id = pt.tag_id
		WHERE pt.message_id = ANY($1)
		ORDER BY ft.position, ft.name`, ids).
		Query(labeled("load forum post tags", func(rows pgx.Rows) error { return scanPostTags(rows, msgs, idx) }))
}

func scanPostTitlesAndLocks(rows pgx.Rows, msgs []core.Message, idx map[uuid.UUID]int) error {
	for rows.Next() {
		var id uuid.UUID
		var title *string
		var isLocked, isResolved bool
		var lastReplyAt *time.Time
		if err := rows.Scan(&id, &title, &isLocked, &isResolved, &lastReplyAt); err != nil {
			return err
		}
		i, ok := idx[id]
		if !ok {
			continue
		}
		msgs[i].Title = title
		msgs[i].IsLocked = isLocked
		msgs[i].IsResolved = isResolved
		if lastReplyAt != nil {
			msgs[i].LastActivityAt = lastReplyAt
		} else {
			msgs[i].LastActivityAt = &msgs[i].CreatedAt
		}
		if msgs[i].Title != nil && msgs[i].Tags == nil {
			msgs[i].Tags = make([]core.ForumTag, 0)
		}
	}
	return rows.Err()
}

func scanPostTags(rows pgx.Rows, msgs []core.Message, idx map[uuid.UUID]int) error {
	for rows.Next() {
		var msgID uuid.UUID
		var tag core.ForumTag
		if err := rows.Scan(&msgID, &tag.ID, &tag.ChannelID, &tag.Name, &tag.Emoji, &tag.Color, &tag.Position, &tag.Moderated, &tag.CreatedAt); err != nil {
			return err
		}
		if i, ok := idx[msgID]; ok {
			if msgs[i].Tags == nil {
				msgs[i].Tags = make([]core.ForumTag, 0)
			}
			msgs[i].Tags = append(msgs[i].Tags, tag)
		}
	}
	return rows.Err()
}
