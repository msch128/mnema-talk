package communityevents

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/httpx"
)

const commentSelect = `
	SELECT c.id, c.event_id, u.id, u.username, u.display_name, u.avatar_s3_key, c.content, c.created_at, c.edited_at
	FROM event_comments c JOIN users u ON u.id = c.user_id`

func scanComment(row pgx.Row) (EventComment, error) {
	var c EventComment
	var avatarKey *string
	err := row.Scan(&c.ID, &c.EventID, &c.User.ID, &c.User.Username, &c.User.DisplayName, &avatarKey, &c.Content, &c.CreatedAt, &c.EditedAt)
	c.User = newUserRef(c.User.ID, c.User.Username, c.User.DisplayName, avatarKey)
	return c, err
}

// ListComments returns a page of an event's comments, oldest first: the
// newest page by default, or the page before the comment `before`.
func (s *Store) ListComments(ctx context.Context, eventID uuid.UUID, before *uuid.UUID, limit int, callerID uuid.UUID) (CommentList, error) {
	var cursor *time.Time
	if before != nil {
		var at time.Time
		err := s.DB.QueryRow(ctx, `SELECT created_at FROM event_comments WHERE id = $1 AND event_id = $2`, *before, eventID).Scan(&at)
		if errors.Is(err, pgx.ErrNoRows) {
			return CommentList{}, httpx.ErrNotFound("comment not found")
		}
		if err != nil {
			return CommentList{}, err
		}
		cursor = &at
	}
	rows, err := s.DB.Query(ctx, commentSelect+`
		WHERE c.event_id = $1 AND ($2::timestamptz IS NULL OR (c.created_at, c.id) < ($2, $3::uuid))
		ORDER BY c.created_at DESC, c.id DESC LIMIT $4`, eventID, cursor, before, limit+1)
	if err != nil {
		return CommentList{}, err
	}
	defer rows.Close()
	list := CommentList{Comments: []EventComment{}}
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			return list, err
		}
		list.Comments = append(list.Comments, c)
	}
	if err := rows.Err(); err != nil {
		return list, err
	}
	if len(list.Comments) > limit {
		list.HasMore = true
		list.Comments = list.Comments[:limit]
	}
	slices.Reverse(list.Comments)
	err = s.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM event_comment_mutes WHERE event_id = $1 AND user_id = $2)`, eventID, callerID).Scan(&list.Muted)
	return list, err
}

// CreateComment stores a comment unless the event already holds the most it keeps.
func (s *Store) CreateComment(ctx context.Context, eventID, userID uuid.UUID, content string) (EventComment, error) {
	text, err := httpx.CleanText("content", content, MaxCommentLen, true)
	if err != nil {
		return EventComment{}, err
	}
	var id uuid.UUID
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := openEvent(ctx, tx, eventID, true); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM event_comments WHERE event_id = $1`, eventID).Scan(&n); err != nil {
			return err
		}
		if n >= MaxEventComments {
			return httpx.ErrConflict(fmt.Sprintf("an event keeps at most %d comments", MaxEventComments))
		}
		return tx.QueryRow(ctx, `INSERT INTO event_comments (event_id, user_id, content) VALUES ($1, $2, $3) RETURNING id`, eventID, userID, text).Scan(&id)
	})
	if err != nil {
		return EventComment{}, err
	}
	return s.GetComment(ctx, eventID, id)
}

// GetComment returns one comment of an event.
func (s *Store) GetComment(ctx context.Context, eventID, id uuid.UUID) (EventComment, error) {
	c, err := scanComment(s.DB.QueryRow(ctx, commentSelect+` WHERE c.id = $1 AND c.event_id = $2`, id, eventID))
	if errors.Is(err, pgx.ErrNoRows) {
		return c, httpx.ErrNotFound("comment not found")
	}
	return c, err
}

// UpdateComment changes the text of a comment its author wrote.
func (s *Store) UpdateComment(ctx context.Context, eventID, id, userID uuid.UUID, content string) (EventComment, error) {
	text, err := httpx.CleanText("content", content, MaxCommentLen, true)
	if err != nil {
		return EventComment{}, err
	}
	cur, err := s.GetComment(ctx, eventID, id)
	if err != nil {
		return cur, err
	}
	if cur.User.ID != userID {
		return cur, httpx.ErrForbidden("only the author edits a comment")
	}
	if _, err := s.DB.Exec(ctx, `UPDATE event_comments SET content = $2, edited_at = NOW() WHERE id = $1`, id, text); err != nil {
		return cur, err
	}
	return s.GetComment(ctx, eventID, id)
}

// DeleteComment removes a comment and returns its author.
func (s *Store) DeleteComment(ctx context.Context, eventID, id uuid.UUID) (author uuid.UUID, err error) {
	err = s.DB.QueryRow(ctx, `DELETE FROM event_comments WHERE id = $1 AND event_id = $2 RETURNING user_id`, id, eventID).Scan(&author)
	if errors.Is(err, pgx.ErrNoRows) {
		return author, httpx.ErrNotFound("comment not found")
	}
	return author, err
}

// SetCommentMute switches the notices of an event's comments off or on for a member.
func (s *Store) SetCommentMute(ctx context.Context, eventID, userID uuid.UUID, muted bool) error {
	q := `DELETE FROM event_comment_mutes WHERE event_id = $1 AND user_id = $2`
	if muted {
		q = `INSERT INTO event_comment_mutes (event_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`
	}
	_, err := s.DB.Exec(ctx, q, eventID, userID)
	return err
}

// CommentRecipients are the members a new comment is announced to: the event's
// creator and everyone who responded going or interested or holds a role,
// except the author and those who muted the event's comments.
func (s *Store) CommentRecipients(ctx context.Context, eventID, authorID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT DISTINCT u FROM (
		    SELECT created_by AS u FROM events WHERE id = $1 AND created_by IS NOT NULL
		    UNION SELECT user_id FROM event_rsvps WHERE event_id = $1 AND response IN ('going', 'interested')
		    UNION SELECT user_id FROM event_signups WHERE event_id = $1
		) people
		WHERE u <> $2 AND NOT EXISTS (SELECT 1 FROM event_comment_mutes m WHERE m.event_id = $1 AND m.user_id = people.u)
		LIMIT 1000`, eventID, authorID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}
