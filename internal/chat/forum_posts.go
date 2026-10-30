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

// PostFilter narrows and orders the post list of a forum.
type PostFilter struct {
	// Sort is "latest_activity" (default) or "newest".
	Sort string
	// TagID keeps the posts that carry this tag.
	TagID *uuid.UUID
	// Search keeps the posts whose title, first message or a reply contains the text.
	Search string
}

// MaxPostSearchLen is the longest search text a list accepts.
const MaxPostSearchLen = 100

// ListForumPosts returns a page of root posts in channelID, ordered by latest activity or creation.
func ListForumPosts(ctx context.Context, p *db.Pool, channelID uuid.UUID, sortMode string, tagID *uuid.UUID, q HistoryQuery) ([]Message, error) {
	return ListForumPostsFiltered(ctx, p, channelID, PostFilter{Sort: sortMode, TagID: tagID}, q)
}

// ListForumPostsFiltered is ListForumPosts with a search text. Pinned posts
// come first and only on the first page (no Before); the pages behind it hold
// the unpinned posts, so the cursor of the order never has to know about pins.
func ListForumPostsFiltered(ctx context.Context, p *db.Pool, channelID uuid.UUID, f PostFilter, q HistoryQuery) ([]Message, error) {
	newest, err := forumSortNewest(f.Sort)
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
	whereClause, args, err := postFilterWhere(channelID, f)
	if err != nil {
		return nil, err
	}

	var pinned []Message
	if q.Before == nil {
		if pinned, err = listPinnedPosts(ctx, p, whereClause, args); err != nil {
			return nil, err
		}
	}
	whereClause += ` AND NOT m.is_pinned`

	var page []Message
	if newest {
		page, err = listPostsByCreation(ctx, p, whereClause, args, q.Before, limit)
	} else {
		page, err = listPostsByActivity(ctx, p, whereClause, args, q, limit)
	}
	if err != nil {
		return nil, err
	}
	return append(pinned, page...), nil
}

// postFilterWhere is the WHERE clause (and its arguments) of the root posts of
// channelID that match f.
func postFilterWhere(channelID uuid.UUID, f PostFilter) (string, []any, error) {
	where := `WHERE m.channel_id = $1 AND m.parent_id IS NULL`
	args := []any{channelID}
	if f.TagID != nil {
		args = append(args, *f.TagID)
		where += fmt.Sprintf(` AND EXISTS (SELECT 1 FROM forum_post_tags pt WHERE pt.message_id = m.id AND pt.tag_id = $%d)`, len(args))
	}
	if text := strings.TrimSpace(f.Search); text != "" {
		clean, err := httpx.CleanText("q", text, MaxPostSearchLen, false)
		if err != nil {
			return "", nil, err
		}
		args = append(args, "%"+likeEscaper.Replace(clean)+"%")
		n := len(args)
		where += fmt.Sprintf(` AND (m.title ILIKE $%[1]d OR m.content ILIKE $%[1]d OR EXISTS (SELECT 1 FROM messages r WHERE r.parent_id = m.id AND r.content ILIKE $%[1]d))`, n)
	}
	return where, args, nil
}

// likeEscaper makes user text literal inside a LIKE pattern.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// listPinnedPosts returns the pinned posts matching where, most recently pinned first.
func listPinnedPosts(ctx context.Context, p *db.Pool, where string, args []any) ([]Message, error) {
	sql := messageSelect + " " + where + fmt.Sprintf(` AND m.is_pinned ORDER BY m.pinned_at DESC, m.id DESC LIMIT %d`, MaxPinsPerChannel)
	return queryMessages(ctx, p, sql, args...)
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

// PreparedPost is a forum post whose title and tags passed the forum's rules,
// ready to be stored with its first message. A post made together with a file
// (media package) is stored the same way as one made from text.
type PreparedPost struct {
	Title  string
	TagIDs []uuid.UUID
}

// PreparePost validates a new post's title and tags in channelID. canManage says
// whether the author holds Manage messages, which moderated tags need.
func PreparePost(ctx context.Context, q db.Querier, channelID uuid.UUID, canManage bool, title string, tagIDs []uuid.UUID) (*PreparedPost, error) {
	cleanTitle, err := CleanPostTitle(title)
	if err != nil {
		return nil, err
	}
	if err := ValidatePostTags(ctx, q, channelID, tagIDs); err != nil {
		return nil, err
	}
	if err := checkTagRules(ctx, q, channelID, nil, tagIDs, canManage); err != nil {
		return nil, err
	}
	return &PreparedPost{Title: cleanTitle, TagIDs: tagIDs}, nil
}

// Store is the callback of InsertMessage that attaches the tags and makes the
// author follow the new post.
func (pp *PreparedPost) Store(ctx context.Context, userID uuid.UUID) func(tx pgx.Tx, messageID uuid.UUID) error {
	return func(tx pgx.Tx, messageID uuid.UUID) error {
		for _, tagID := range pp.TagIDs {
			if _, err := tx.Exec(ctx, `INSERT INTO forum_post_tags (message_id, tag_id) VALUES ($1, $2)`, messageID, tagID); err != nil {
				return postTagError(err)
			}
		}
		return SetThreadFollow(ctx, tx, userID, messageID, true)
	}
}

// CreateForumPost creates a root post in channelID with title, message content, and tags.
// canManage says whether the author holds Manage messages, which moderated tags need.
func CreateForumPost(ctx context.Context, p *db.Pool, online OnlineSource, channelID, userID uuid.UUID, canManage bool, title, content string, tagIDs []uuid.UUID) (uuid.UUID, error) {
	validContent, err := ValidateContent(content, false)
	if err != nil {
		return uuid.Nil, err
	}
	post, err := PreparePost(ctx, p, channelID, canManage, title, tagIDs)
	if err != nil {
		return uuid.Nil, err
	}
	return InsertMessage(ctx, p, online, NewMessage{
		ChannelID: channelID,
		UserID:    userID,
		Content:   validContent,
		Title:     &post.Title,
	}, post.Store(ctx, userID))
}

// PostPatch is the set of changes an update of a post asks for; nil fields stay as they are.
type PostPatch struct {
	Title      *string
	TagIDs     *[]uuid.UUID
	IsLocked   *bool
	IsResolved *bool
}

func (p PostPatch) empty() bool {
	return p.Title == nil && p.TagIDs == nil && p.IsLocked == nil && p.IsResolved == nil
}

// editsPost is true when the patch changes what the post's author owns: title, tags or the solved mark.
func (p PostPatch) editsPost() bool {
	return p.Title != nil || p.TagIDs != nil || p.IsResolved != nil
}

// PostActor is who changes a post.
type PostActor struct {
	ID uuid.UUID
	// CanManage: the actor holds Manage messages in the channel.
	CanManage bool
}

// UpdateForumPost updates a post's title, tags, lock or solved status.
func UpdateForumPost(ctx context.Context, p *db.Pool, channelID, messageID uuid.UUID, actor PostActor, patch PostPatch) (*Message, error) {
	if patch.empty() {
		return nil, httpx.ErrInvalidInput("no fields to update")
	}
	// A first check before the screening and tag lookups; the one under the
	// row lock below is the one that counts.
	authorID, locked, err := getForumPostAuthor(ctx, p, channelID, messageID, false)
	if err != nil {
		return nil, err
	}
	if err := checkPostUpdatePerms(actor, authorID, locked, patch); err != nil {
		return nil, err
	}
	cleanTitle, err := validatePostUpdates(ctx, p, channelID, patch)
	if err != nil {
		return nil, err
	}
	var pending *automod.Pending
	if cleanTitle != nil {
		if pending, err = screenText(ctx, p, channelID, actor.ID, messageID, *cleanTitle); err != nil {
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
		if err := checkPostUpdatePerms(actor, authorID, locked, patch); err != nil {
			return err
		}
		return applyPostUpdate(ctx, tx, channelID, messageID, actor, cleanTitle, patch)
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

func checkPostUpdatePerms(actor PostActor, authorID uuid.UUID, locked bool, patch PostPatch) error {
	if patch.IsLocked != nil && !actor.CanManage {
		return httpx.ErrForbidden("missing permission to lock or unlock posts")
	}
	if patch.editsPost() && actor.ID != authorID && !actor.CanManage {
		return httpx.ErrForbidden("missing permission to edit others' posts")
	}
	if patch.editsPost() && locked && !actor.CanManage {
		return httpx.ErrForbidden("the post is locked")
	}
	return nil
}

func validatePostUpdates(ctx context.Context, p *db.Pool, channelID uuid.UUID, patch PostPatch) (*string, error) {
	var cleanTitle *string
	if patch.Title != nil {
		c, err := CleanPostTitle(*patch.Title)
		if err != nil {
			return nil, err
		}
		cleanTitle = &c
	}
	if patch.TagIDs != nil {
		if err := ValidatePostTags(ctx, p, channelID, *patch.TagIDs); err != nil {
			return nil, err
		}
	}
	return cleanTitle, nil
}

func applyPostUpdate(ctx context.Context, tx pgx.Tx, channelID, messageID uuid.UUID, actor PostActor, cleanTitle *string, patch PostPatch) error {
	if cleanTitle != nil || patch.IsLocked != nil || patch.IsResolved != nil {
		if _, err := tx.Exec(ctx, `
			UPDATE messages SET
				title = COALESCE($2, title),
				is_locked = COALESCE($3, is_locked),
				is_resolved = COALESCE($4, is_resolved)
			WHERE id = $1`, messageID, cleanTitle, patch.IsLocked, patch.IsResolved); err != nil {
			return fmt.Errorf("update post: %w", err)
		}
	}
	if patch.TagIDs == nil {
		return nil
	}
	return replacePostTags(ctx, tx, channelID, messageID, actor, *patch.TagIDs)
}

// replacePostTags sets the tags of a post to tagIDs after the moderated-tag and
// required-tag rules, which see the tags the post has right now.
func replacePostTags(ctx context.Context, tx pgx.Tx, channelID, messageID uuid.UUID, actor PostActor, tagIDs []uuid.UUID) error {
	current, err := postTagIDs(ctx, tx, messageID)
	if err != nil {
		return err
	}
	if err := checkTagRules(ctx, tx, channelID, current, tagIDs, actor.CanManage); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM forum_post_tags WHERE message_id = $1`, messageID); err != nil {
		return fmt.Errorf("delete post tags: %w", err)
	}
	for _, tagID := range tagIDs {
		if _, err := tx.Exec(ctx, `INSERT INTO forum_post_tags (message_id, tag_id) VALUES ($1, $2)`, messageID, tagID); err != nil {
			return postTagError(err)
		}
	}
	return nil
}

func postTagIDs(ctx context.Context, q db.Querier, messageID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := q.Query(ctx, `SELECT tag_id FROM forum_post_tags WHERE message_id = $1`, messageID)
	if err != nil {
		return nil, fmt.Errorf("load post tags: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// postTagError maps a refused tag insert: a tag deleted after ValidatePostTags
// looked at it is no longer a tag of this channel.
func postTagError(err error) error {
	if isForeignKeyViolation(err) {
		return errTagWrongChannel
	}
	return fmt.Errorf("insert post tag: %w", err)
}
