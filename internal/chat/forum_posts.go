package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/automod"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// CleanPostTitle validates and sanitizes a forum post title.
func CleanPostTitle(title string) (string, error) {
	if strings.TrimSpace(title) == "" {
		return "", errPostTitleRequired
	}
	return httpx.CleanText("title", title, MaxPostTitleLen, true)
}

// forumSortNewest maps the sort query value of the post list to its order:
// true for creation time, false for latest activity (the default).
func forumSortNewest(mode string) (bool, error) {
	switch mode {
	case "", "latest_activity", "activity":
		return false, nil
	case "newest", "creation":
		return true, nil
	}
	return false, httpx.ErrInvalidInput("sort must be latest_activity or newest")
}

// ListForumPosts returns a page of root posts in channelID, ordered by latest activity or creation.
func ListForumPosts(ctx context.Context, p *db.Pool, channelID uuid.UUID, sortMode string, tagID *uuid.UUID, q HistoryQuery) ([]Message, error) {
	newest, err := forumSortNewest(sortMode)
	if err != nil {
		return nil, err
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}

	if err := messageCursorExists(ctx, p, q.Before); err != nil {
		return nil, err
	}
	whereClause := `WHERE m.channel_id = $1 AND m.parent_id IS NULL`
	args := []any{channelID}

	if tagID != nil {
		args = append(args, *tagID)
		whereClause += fmt.Sprintf(` AND EXISTS (SELECT 1 FROM forum_post_tags pt WHERE pt.message_id = m.id AND pt.tag_id = $%d)`, len(args))
	}

	if newest {
		return listPostsByCreation(ctx, p, whereClause, args, q.Before, limit)
	}
	return listPostsByActivity(ctx, p, whereClause, args, q, limit)
}

func listPostsByCreation(ctx context.Context, p *db.Pool, where string, args []any, before *uuid.UUID, limit int) ([]Message, error) {
	if before != nil {
		args = append(args, *before)
		where += fmt.Sprintf(` AND (m.created_at, m.id) < (SELECT created_at, id FROM messages WHERE id = $%d)`, len(args))
	}
	args = append(args, limit)
	sql := messageSelect + " " + where + fmt.Sprintf(` ORDER BY m.created_at DESC, m.id DESC LIMIT $%d`, len(args))
	return queryMessages(ctx, p, sql, args...)
}

const activityExpr = `COALESCE((SELECT MAX(r.created_at) FROM messages r WHERE r.parent_id = m.id), m.created_at)`

// cursorActivityExpr is activityExpr evaluated for the cursor row (alias a).
const cursorActivityExpr = `COALESCE((SELECT MAX(r.created_at) FROM messages r WHERE r.parent_id = a.id), a.created_at)`

// listPostsByActivity pages by (activity, id). A client that sends the
// activity it saw for the cursor post (BeforeActivity) gets exactly the posts
// behind that position; without it the cursor's activity is read now, which
// moves when the cursor post has since got or lost a reply.
func listPostsByActivity(ctx context.Context, p *db.Pool, where string, args []any, q HistoryQuery, limit int) ([]Message, error) {
	switch {
	case q.Before != nil && q.BeforeActivity != nil:
		args = append(args, *q.BeforeActivity, *q.Before)
		where += fmt.Sprintf(` AND (`+activityExpr+`, m.id) < ($%d, $%d)`, len(args)-1, len(args))
	case q.Before != nil:
		args = append(args, *q.Before)
		where += fmt.Sprintf(` AND (`+activityExpr+`, m.id) < (SELECT `+cursorActivityExpr+`, a.id FROM messages a WHERE a.id = $%d)`, len(args))
	}
	args = append(args, limit)
	sql := messageSelect + " " + where + fmt.Sprintf(` ORDER BY `+activityExpr+` DESC, m.id DESC LIMIT $%d`, len(args))
	return queryMessages(ctx, p, sql, args...)
}

// CreateForumPost creates a root post in channelID with title, message content, and tags.
func CreateForumPost(ctx context.Context, p *db.Pool, online OnlineSource, channelID, userID uuid.UUID, title, content string, tagIDs []uuid.UUID) (uuid.UUID, error) {
	cleanTitle, err := CleanPostTitle(title)
	if err != nil {
		return uuid.Nil, err
	}
	validContent, err := ValidateContent(content, false)
	if err != nil {
		return uuid.Nil, err
	}
	if err := ValidatePostTags(ctx, p, channelID, tagIDs); err != nil {
		return uuid.Nil, err
	}

	id, err := InsertMessage(ctx, p, online, NewMessage{
		ChannelID: channelID,
		UserID:    userID,
		Content:   validContent,
		Title:     &cleanTitle,
	}, func(tx pgx.Tx, messageID uuid.UUID) error {
		for _, tagID := range tagIDs {
			if _, err := tx.Exec(ctx, `INSERT INTO forum_post_tags (message_id, tag_id) VALUES ($1, $2)`, messageID, tagID); err != nil {
				return postTagError(err)
			}
		}
		return SetThreadFollow(ctx, tx, userID, messageID, true)
	})
	if err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

// UpdateForumPost updates a post's title, tags, or lock status.
func UpdateForumPost(ctx context.Context, p *db.Pool, channelID, messageID, actorID uuid.UUID, canManage bool, title *string, tagIDs *[]uuid.UUID, isLocked *bool) (*Message, error) {
	if title == nil && tagIDs == nil && isLocked == nil {
		return nil, httpx.ErrInvalidInput("no fields to update")
	}
	// A first check before the screening and tag lookups; the one under the
	// row lock below is the one that counts.
	authorID, locked, err := getForumPostAuthor(ctx, p, channelID, messageID, false)
	if err != nil {
		return nil, err
	}
	if err := checkPostUpdatePerms(actorID, authorID, canManage, locked, title, tagIDs, isLocked); err != nil {
		return nil, err
	}
	cleanTitle, err := validatePostUpdates(ctx, p, channelID, title, tagIDs)
	if err != nil {
		return nil, err
	}
	var pending *automod.Pending
	if cleanTitle != nil {
		if pending, err = screenText(ctx, p, channelID, actorID, messageID, *cleanTitle); err != nil {
			return nil, err
		}
	}

	err = pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		// A lock or an ownership change that raced with this request is seen
		// here, and the row stays put until the update is applied.
		authorID, locked, err := getForumPostAuthor(ctx, tx, channelID, messageID, true)
		if err != nil {
			return err
		}
		if err := checkPostUpdatePerms(actorID, authorID, canManage, locked, title, tagIDs, isLocked); err != nil {
			return err
		}
		return applyPostUpdate(ctx, tx, messageID, cleanTitle, isLocked, tagIDs)
	})
	if err != nil {
		return nil, err
	}
	pending.Commit(ctx, messageID)

	return GetMessage(ctx, p, messageID)
}

func getForumPostAuthor(ctx context.Context, q db.Querier, channelID, messageID uuid.UUID, lock bool) (authorID uuid.UUID, locked bool, err error) {
	var curChannelID uuid.UUID
	var parentID *uuid.UUID
	query := `SELECT channel_id, user_id, parent_id, is_locked FROM messages WHERE id = $1`
	if lock {
		query += ` FOR UPDATE`
	}
	err = q.QueryRow(ctx, query, messageID).Scan(&curChannelID, &authorID, &parentID, &locked)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (curChannelID != channelID || parentID != nil)) {
		return uuid.Nil, false, errMessageNotFound
	}
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("load post: %w", err)
	}
	return authorID, locked, nil
}

func checkPostUpdatePerms(actorID, authorID uuid.UUID, canManage, locked bool, title *string, tagIDs *[]uuid.UUID, isLocked *bool) error {
	if isLocked != nil && !canManage {
		return httpx.ErrForbidden("missing permission to lock or unlock posts")
	}
	if (title != nil || tagIDs != nil) && actorID != authorID && !canManage {
		return httpx.ErrForbidden("missing permission to edit others' posts")
	}
	if (title != nil || tagIDs != nil) && locked && !canManage {
		return httpx.ErrForbidden("the post is locked")
	}
	return nil
}

func validatePostUpdates(ctx context.Context, p *db.Pool, channelID uuid.UUID, title *string, tagIDs *[]uuid.UUID) (*string, error) {
	var cleanTitle *string
	if title != nil {
		c, err := CleanPostTitle(*title)
		if err != nil {
			return nil, err
		}
		cleanTitle = &c
	}
	if tagIDs != nil {
		if err := ValidatePostTags(ctx, p, channelID, *tagIDs); err != nil {
			return nil, err
		}
	}
	return cleanTitle, nil
}

func applyPostUpdate(ctx context.Context, tx pgx.Tx, messageID uuid.UUID, cleanTitle *string, isLocked *bool, tagIDs *[]uuid.UUID) error {
	if cleanTitle != nil || isLocked != nil {
		if _, err := tx.Exec(ctx, `
			UPDATE messages SET
				title = COALESCE($2, title),
				is_locked = COALESCE($3, is_locked)
			WHERE id = $1`, messageID, cleanTitle, isLocked); err != nil {
			return fmt.Errorf("update post: %w", err)
		}
	}
	if tagIDs == nil {
		return nil
	}
	if _, err := tx.Exec(ctx, `DELETE FROM forum_post_tags WHERE message_id = $1`, messageID); err != nil {
		return fmt.Errorf("delete post tags: %w", err)
	}
	for _, tagID := range *tagIDs {
		if _, err := tx.Exec(ctx, `INSERT INTO forum_post_tags (message_id, tag_id) VALUES ($1, $2)`, messageID, tagID); err != nil {
			return postTagError(err)
		}
	}
	return nil
}

// postTagError maps a refused tag insert: a tag deleted after ValidatePostTags
// looked at it is no longer a tag of this channel.
func postTagError(err error) error {
	if isForeignKeyViolation(err) {
		return errTagWrongChannel
	}
	return fmt.Errorf("insert post tag: %w", err)
}
