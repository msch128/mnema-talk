package wiki

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// text is a page's title, text and summary after the checks.
type text struct{ title, content, summary string }

// cleanText checks and trims what an edit carries. A nil title or content is "not given".
func cleanText(title, content *string, summary string) (text, error) {
	var out text
	var err error
	if title != nil {
		if out.title, err = cleanTitle(*title); err != nil {
			return out, err
		}
	}
	if content != nil {
		if len(*content) > MaxContentBytes {
			return out, errTooLong
		}
		if !utf8.ValidString(*content) || strings.ContainsRune(*content, 0) {
			return out, httpx.ErrInvalidInput("the text must be valid text without NUL characters")
		}
		out.content = *content
	}
	out.summary, err = httpx.CleanText("summary", summary, MaxSummaryRunes, false)
	return out, err
}

// checkTitle refuses a title another live page has (titleTaken).
func checkTitle(ctx context.Context, q db.Querier, title string, except uuid.UUID) error {
	taken, err := titleTaken(ctx, q, title, except)
	if err != nil {
		return err
	}
	if taken {
		return errTitleTaken
	}
	return nil
}

// slugTaken reports whether a page or a redirect uses the address.
func slugTaken(ctx context.Context, q db.Querier, slug string) (bool, error) {
	var taken bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM wiki_pages WHERE slug = $1 UNION ALL SELECT 1 FROM wiki_redirects WHERE slug = $1)`, slug).Scan(&taken)
	return taken, err
}

// chooseSlug validates the address a page was given, or derives a free one from its title.
func chooseSlug(ctx context.Context, q db.Querier, given, title string) (string, error) {
	if given = strings.ToLower(strings.TrimSpace(given)); given != "" {
		return givenSlug(ctx, q, given)
	}
	base := DeriveSlug(title)
	for n := 1; n < 100; n++ {
		candidate := base
		if n > 1 {
			candidate = numbered(base, n)
		}
		free, err := slugFree(ctx, q, candidate)
		if err != nil {
			return "", err
		}
		if free {
			return candidate, nil
		}
	}
	return "", errSlugTaken
}

// givenSlug checks an address somebody chose.
func givenSlug(ctx context.Context, q db.Querier, slug string) (string, error) {
	if !ValidSlug(slug) {
		return "", errSlugInvalid
	}
	free, err := slugFree(ctx, q, slug)
	if err != nil {
		return "", err
	}
	if !free {
		return "", errSlugTaken
	}
	return slug, nil
}

// slugFree reports whether the address is valid and no page or redirect uses it.
func slugFree(ctx context.Context, q db.Querier, slug string) (bool, error) {
	if !ValidSlug(slug) {
		return false, nil
	}
	taken, err := slugTaken(ctx, q, slug)
	return !taken, err
}

// checkRoom refuses a page that would pass the limits: 500 pages, 5 levels below the parent.
func checkRoom(ctx context.Context, q db.Querier, parent *uuid.UUID, extraLevels int) error {
	if parent != nil {
		var live bool
		if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM wiki_pages WHERE id = $1 AND deleted_at IS NULL)`, *parent).Scan(&live); err != nil {
			return err
		}
		if !live {
			return errParentGone
		}
	}
	depth, err := depthOf(ctx, q, parent)
	if err != nil {
		return err
	}
	if depth+extraLevels > MaxDepth {
		return errTooDeep
	}
	return nil
}

func checkCount(ctx context.Context, q db.Querier) error {
	var n int
	if err := q.QueryRow(ctx, `SELECT count(*) FROM wiki_pages WHERE deleted_at IS NULL`).Scan(&n); err != nil {
		return err
	}
	if n >= MaxPages {
		return errTooMany
	}
	return nil
}

func nextPosition(ctx context.Context, q db.Querier, parent *uuid.UUID) (int, error) {
	var pos int
	err := q.QueryRow(ctx, `SELECT COALESCE(MAX(position) + 1, 0) FROM wiki_pages WHERE parent_id IS NOT DISTINCT FROM $1 AND deleted_at IS NULL`, parent).Scan(&pos)
	return pos, err
}

// Create adds a page below parent (nil: at the top) as revision 1 by user.
func (s *Store) Create(ctx context.Context, user uuid.UUID, in PageInput) (*Page, error) {
	t, err := cleanText(&in.Title, &in.Content, in.Summary)
	if err != nil {
		return nil, err
	}
	var slug string
	err = pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		if err := lockTree(ctx, tx); err != nil {
			return err
		}
		slug, err = createIn(ctx, tx, user, in, t)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, slug, false)
}

// Delete takes the page out of the tree; it keeps its address and its revisions and can be restored.
func (s *Store) Delete(ctx context.Context, user uuid.UUID, slug string) (*Page, error) {
	return s.inTx(ctx, slug, false, func(tx pgx.Tx, p *Page) error {
		var children bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM wiki_pages WHERE parent_id = $1 AND deleted_at IS NULL)`, p.ID).Scan(&children); err != nil {
			return err
		}
		if children {
			return errHasChildren
		}
		_, err := tx.Exec(ctx, `UPDATE wiki_pages SET deleted_at = NOW(), deleted_by = $2 WHERE id = $1`, p.ID, user)
		return err
	})
}

// Undelete puts a deleted page back; below its old parent, or at the top when that one is gone too.
func (s *Store) Undelete(ctx context.Context, slug string) (*Page, error) {
	return s.inTx(ctx, slug, true, func(tx pgx.Tx, p *Page) error {
		if p.DeletedAt == nil {
			return errNotDeleted
		}
		if err := checkCount(ctx, tx); err != nil {
			return err
		}
		if err := checkTitle(ctx, tx, p.Title, p.ID); err != nil {
			return err
		}
		parent := p.ParentID
		if checkRoom(ctx, tx, parent, 1) != nil {
			parent = nil // the parent is gone, or the page no longer fits below it
		}
		_, err := tx.Exec(ctx, `UPDATE wiki_pages SET deleted_at = NULL, deleted_by = NULL, parent_id = $2 WHERE id = $1`, p.ID, parent)
		return err
	})
}

// SetLocked freezes or thaws a page: while locked, only holders of Manage wiki change it.
func (s *Store) SetLocked(ctx context.Context, slug string, locked bool) (*Page, error) {
	return s.inTx(ctx, slug, false, func(tx pgx.Tx, p *Page) error {
		_, err := tx.Exec(ctx, `UPDATE wiki_pages SET locked = $2 WHERE id = $1`, p.ID, locked)
		return err
	})
}

// inTx runs change on the locked page and returns the page as it is after.
func (s *Store) inTx(ctx context.Context, slug string, deleted bool, change func(pgx.Tx, *Page) error) (*Page, error) {
	var out *Page
	err := pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		if err := lockTree(ctx, tx); err != nil {
			return err
		}
		p, err := getPage(ctx, tx, slug, deleted, true)
		if err != nil {
			return err
		}
		if err := change(tx, p); err != nil {
			return err
		}
		out, err = getPage(ctx, tx, p.Slug, true, false)
		return err
	})
	return out, err
}

// createIn writes the page and its first revision in the locked tree and returns its address.
func createIn(ctx context.Context, tx pgx.Tx, user uuid.UUID, in PageInput, t text) (string, error) {
	if err := checkCount(ctx, tx); err != nil {
		return "", err
	}
	if err := checkRoom(ctx, tx, in.ParentID, 1); err != nil {
		return "", err
	}
	if err := checkTitle(ctx, tx, t.title, uuid.Nil); err != nil {
		return "", err
	}
	slug, err := chooseSlug(ctx, tx, in.Slug, t.title)
	if err != nil {
		return "", err
	}
	pos, err := nextPosition(ctx, tx, in.ParentID)
	if err != nil {
		return "", err
	}
	var id uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO wiki_pages (slug, title, parent_id, position, content, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $6) RETURNING id`, slug, t.title, in.ParentID, pos, t.content, user).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("create wiki page: %w", err)
	}
	return slug, insertRevision(ctx, tx, id, 1, user, t, nil)
}
