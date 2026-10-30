package chat

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

var (
	errForumTagNotFound  = httpx.ErrNotFound("forum tag not found")
	errTooManyForumTags  = httpx.ErrConflict("a forum channel can have at most 20 tags")
	errTooManyPostTags   = httpx.ErrInvalidInput("a post can have at most 5 tags")
	errTagWrongChannel   = httpx.ErrInvalidInput("tag does not belong to this channel")
	errTagNameTaken      = httpx.ErrConflict("a tag with this name already exists")
	errPostTitleRequired = httpx.ErrInvalidInput("title is required for forum posts")
	errTagColorInvalid   = httpx.ErrInvalidInput("color must be one of: gray, red, orange, yellow, green, teal, blue, purple, pink")
	errTagOrderMismatch  = httpx.ErrInvalidInput("tag_ids must list every tag of the forum exactly once")
	errTagModerated      = httpx.ErrForbidden("missing permission to apply or remove moderated tags")
)

// ForumTagColors are the colours a tag can have: names of the palette the
// client draws in its own calm tones, never raw CSS.
var ForumTagColors = []string{"gray", "red", "orange", "yellow", "green", "teal", "blue", "purple", "pink"}

// CleanTagColor validates an optional tag colour ("" is the plain look).
func CleanTagColor(color string) (string, error) {
	if color == "" || slices.Contains(ForumTagColors, color) {
		return color, nil
	}
	return "", errTagColorInvalid
}

// CleanTagName validates and sanitizes a forum tag name.
func CleanTagName(name string) (string, error) {
	return httpx.CleanText("name", name, MaxForumTagNameLen, true)
}

// CleanTagEmoji validates and sanitizes an optional tag emoji.
func CleanTagEmoji(emoji string) (string, error) {
	if emoji == "" {
		return "", nil
	}
	return httpx.CleanText("emoji", emoji, MaxForumTagEmojiLen, true)
}

// forumTagColumns is the column list every tag query returns, in ForumTag.scan order.
const forumTagColumns = `id, channel_id, name, emoji, color, position, moderated, created_at`

func (t *ForumTag) scan(row pgx.Row) error {
	return row.Scan(&t.ID, &t.ChannelID, &t.Name, &t.Emoji, &t.Color, &t.Position, &t.Moderated, &t.CreatedAt)
}

// ListForumTags loads all tags defined in channelID in the forum's own order.
func ListForumTags(ctx context.Context, q db.Querier, channelID uuid.UUID) ([]ForumTag, error) {
	rows, err := q.Query(ctx, `
		SELECT `+forumTagColumns+`
		FROM forum_tags WHERE channel_id = $1 ORDER BY position ASC, name ASC`, channelID)
	if err != nil {
		return nil, fmt.Errorf("list forum tags: %w", err)
	}
	defer rows.Close()
	tags := make([]ForumTag, 0)
	for rows.Next() {
		var t ForumTag
		if err := t.scan(rows); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// TagSpec is the content of a new tag.
type TagSpec struct {
	Name      string
	Emoji     string
	Color     string
	Moderated bool
}

// CreateForumTag adds a plain tag (no colour, not moderated) to channelID.
func CreateForumTag(ctx context.Context, p *db.Pool, channelID uuid.UUID, name, emoji string) (*ForumTag, error) {
	return CreateForumTagSpec(ctx, p, channelID, TagSpec{Name: name, Emoji: emoji})
}

// CreateForumTagSpec adds a new tag at the end of channelID's order, enforcing the 20-tag cap.
func CreateForumTagSpec(ctx context.Context, p *db.Pool, channelID uuid.UUID, spec TagSpec) (*ForumTag, error) {
	cleanName, err := CleanTagName(spec.Name)
	if err != nil {
		return nil, err
	}
	cleanEmoji, err := CleanTagEmoji(spec.Emoji)
	if err != nil {
		return nil, err
	}
	cleanColor, err := CleanTagColor(spec.Color)
	if err != nil {
		return nil, err
	}

	var tag ForumTag
	err = pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		// Lock the channel row so concurrent creates cannot pass the cap together.
		if _, err := tx.Exec(ctx, `SELECT 1 FROM channels WHERE id = $1 FOR UPDATE`, channelID); err != nil {
			return err
		}
		var count, last int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*), COALESCE(MAX(position), 0) FROM forum_tags WHERE channel_id = $1`, channelID).Scan(&count, &last); err != nil {
			return err
		}
		if count >= MaxForumTags {
			return errTooManyForumTags
		}
		return tag.scan(tx.QueryRow(ctx, `
			INSERT INTO forum_tags (channel_id, name, emoji, color, moderated, position) VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING `+forumTagColumns,
			channelID, cleanName, cleanEmoji, cleanColor, spec.Moderated, last+1))
	})
	if db.IsUniqueViolation(err) {
		return nil, errTagNameTaken
	}
	if err != nil {
		return nil, err
	}
	return &tag, nil
}

// TagPatch is the set of fields an update changes; nil leaves a field as it is.
type TagPatch struct {
	Name      *string
	Emoji     *string
	Color     *string
	Moderated *bool
}

func (p TagPatch) empty() bool {
	return p.Name == nil && p.Emoji == nil && p.Color == nil && p.Moderated == nil
}

// clean validates the set fields; the result has the same shape.
func (p TagPatch) clean() (TagPatch, error) {
	out := TagPatch{Moderated: p.Moderated}
	var err error
	for _, f := range []struct {
		in    *string
		out   **string
		clean func(string) (string, error)
	}{{p.Name, &out.Name, CleanTagName}, {p.Emoji, &out.Emoji, CleanTagEmoji}, {p.Color, &out.Color, CleanTagColor}} {
		if f.in == nil {
			continue
		}
		var v string
		if v, err = f.clean(*f.in); err != nil {
			return TagPatch{}, err
		}
		*f.out = &v
	}
	return out, nil
}

// UpdateForumTag updates the set fields of a tag and returns the tag as it was
// before the change next to the new one. A single statement writes only the
// fields that are set, so two admins changing different fields of one tag
// cannot undo each other.
func UpdateForumTag(ctx context.Context, q db.Querier, channelID, tagID uuid.UUID, patch TagPatch) (updated, previous *ForumTag, err error) {
	if patch.empty() {
		return nil, nil, httpx.ErrInvalidInput("no fields to update")
	}
	c, err := patch.clean()
	if err != nil {
		return nil, nil, err
	}

	var tag, old ForumTag
	err = q.QueryRow(ctx, `
		UPDATE forum_tags t SET name = COALESCE($3, o.name), emoji = COALESCE($4, o.emoji),
		       color = COALESCE($5, o.color), moderated = COALESCE($6, o.moderated)
		FROM (SELECT id, name, emoji, color, moderated FROM forum_tags WHERE id = $1 AND channel_id = $2 FOR UPDATE) o
		WHERE t.id = o.id
		RETURNING t.id, t.channel_id, t.name, t.emoji, t.color, t.position, t.moderated, t.created_at,
		          o.name, o.emoji, o.color, o.moderated`,
		tagID, channelID, c.Name, c.Emoji, c.Color, c.Moderated).
		Scan(&tag.ID, &tag.ChannelID, &tag.Name, &tag.Emoji, &tag.Color, &tag.Position, &tag.Moderated, &tag.CreatedAt,
			&old.Name, &old.Emoji, &old.Color, &old.Moderated)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, nil, errForumTagNotFound
	case db.IsUniqueViolation(err):
		return nil, nil, errTagNameTaken
	case err != nil:
		return nil, nil, fmt.Errorf("update forum tag: %w", err)
	}
	old.ID, old.ChannelID, old.Position, old.CreatedAt = tag.ID, tag.ChannelID, tag.Position, tag.CreatedAt
	return &tag, &old, nil
}

// ReorderForumTags puts the tags of channelID in the order of tagIDs, which
// must name every tag of the forum exactly once.
func ReorderForumTags(ctx context.Context, p *db.Pool, channelID uuid.UUID, tagIDs []uuid.UUID) ([]ForumTag, error) {
	err := pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM channels WHERE id = $1 FOR UPDATE`, channelID); err != nil {
			return err
		}
		current, err := ListForumTags(ctx, tx, channelID)
		if err != nil {
			return err
		}
		if !sameTagSet(current, tagIDs) {
			return errTagOrderMismatch
		}
		_, err = tx.Exec(ctx, `
			UPDATE forum_tags t SET position = o.ord
			FROM unnest($2::uuid[]) WITH ORDINALITY AS o(id, ord)
			WHERE t.id = o.id AND t.channel_id = $1`, channelID, tagIDs)
		return err
	})
	if err != nil {
		return nil, err
	}
	return ListForumTags(ctx, p, channelID)
}

func sameTagSet(current []ForumTag, ids []uuid.UUID) bool {
	if len(current) != len(ids) {
		return false
	}
	have := make(map[uuid.UUID]bool, len(current))
	for _, t := range current {
		have[t.ID] = true
	}
	for _, id := range ids {
		if !have[id] {
			return false
		}
		delete(have, id)
	}
	return true
}

// DeleteForumTag removes a tag from the channel and returns what it was.
func DeleteForumTag(ctx context.Context, q db.Querier, channelID, tagID uuid.UUID) (*ForumTag, error) {
	var tag ForumTag
	err := tag.scan(q.QueryRow(ctx, `
		DELETE FROM forum_tags WHERE id = $1 AND channel_id = $2
		RETURNING `+forumTagColumns, tagID, channelID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errForumTagNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("delete forum tag: %w", err)
	}
	return &tag, nil
}

// ValidatePostTags verifies that tagIDs are at most 5 and all belong to channelID.
func ValidatePostTags(ctx context.Context, q db.Querier, channelID uuid.UUID, tagIDs []uuid.UUID) error {
	if len(tagIDs) == 0 {
		return nil
	}
	if len(tagIDs) > MaxPostTags {
		return errTooManyPostTags
	}
	// Ensure all tag IDs belong to this channel
	var count int
	err := q.QueryRow(ctx, `SELECT COUNT(*) FROM forum_tags WHERE channel_id = $1 AND id = ANY($2)`, channelID, tagIDs).Scan(&count)
	if err != nil {
		return fmt.Errorf("validate post tags: %w", err)
	}
	if count != len(tagIDs) {
		return errTagWrongChannel
	}
	return nil
}

// checkTagRules enforces the forum's rules for a post whose tags change from
// current to next: a moderated tag is applied or removed only by someone who
// holds Manage messages, and a forum that requires a tag does not let a post
// end up without one (unless the actor may apply no tag at all, which would
// close the forum for them).
func checkTagRules(ctx context.Context, q db.Querier, channelID uuid.UUID, current, next []uuid.UUID, canManage bool) error {
	tags, err := ListForumTags(ctx, q, channelID)
	if err != nil {
		return err
	}
	moderated := make(map[uuid.UUID]bool, len(tags))
	usable := 0
	for _, t := range tags {
		moderated[t.ID] = t.Moderated
		if canManage || !t.Moderated {
			usable++
		}
	}
	if !canManage {
		for _, id := range symmetricDifference(current, next) {
			if moderated[id] {
				return errTagModerated
			}
		}
	}
	if len(next) > 0 || usable == 0 {
		return nil
	}
	settings, err := GetForumSettings(ctx, q, channelID)
	if err != nil {
		return err
	}
	if settings.RequireTag {
		return errTagRequired
	}
	return nil
}

// symmetricDifference is the ids that are in exactly one of a and b.
func symmetricDifference(a, b []uuid.UUID) []uuid.UUID {
	inA := make(map[uuid.UUID]bool, len(a))
	for _, id := range a {
		inA[id] = true
	}
	var out []uuid.UUID
	for _, id := range b {
		if inA[id] {
			delete(inA, id)
		} else {
			out = append(out, id)
		}
	}
	for id := range inA {
		out = append(out, id)
	}
	return out
}
