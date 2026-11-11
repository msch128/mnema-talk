package forum

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/automod"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/mentions"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// CleanPostTitle validates and sanitizes a forum post title.
func CleanPostTitle(title string) (string, error) {
	if strings.TrimSpace(title) == "" {
		return "", core.ErrPostTitleRequired
	}
	return httpx.CleanText("title", title, core.MaxPostTitleLen, true)
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
		return messages.SetThreadFollow(ctx, tx, userID, messageID, true)
	}
}

// ServeCreateForumPost creates a root post in channelID with title, message content, and tags.
// canManage says whether the author holds Manage messages, which moderated tags need.
func CreateForumPost(ctx context.Context, p *db.Pool, online mentions.OnlineSource, channelID, userID uuid.UUID, canManage bool, title, content string, tagIDs []uuid.UUID) (uuid.UUID, error) {
	validContent, err := core.ValidateContent(content, false)
	if err != nil {
		return uuid.Nil, err
	}
	post, err := PreparePost(ctx, p, channelID, canManage, title, tagIDs)
	if err != nil {
		return uuid.Nil, err
	}
	return messages.InsertMessage(ctx, p, online, messages.NewMessage{
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

// ServeUpdateForumPost updates a post's title, tags, lock or solved status.
func UpdateForumPost(ctx context.Context, p *db.Pool, channelID, messageID uuid.UUID, actor PostActor, patch PostPatch) (*core.Message, error) {
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
		if pending, err = messages.ScreenText(ctx, p, channelID, actor.ID, messageID, *cleanTitle); err != nil {
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
		if err := applyPostUpdate(ctx, tx, channelID, messageID, actor, cleanTitle, patch); err != nil {
			return err
		}
		return messages.RecordMessageEvent(ctx, p, tx, events.MessageUpdate, channelID, messageID, nil)
	})
	if err != nil {
		return nil, err
	}
	pending.Commit(ctx, messageID)

	return messages.GetMessage(ctx, p, messageID)
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
		return uuid.Nil, false, core.ErrMessageNotFound
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
	if messages.IsForeignKeyViolation(err) {
		return errTagWrongChannel
	}
	return fmt.Errorf("insert post tag: %w", err)
}
