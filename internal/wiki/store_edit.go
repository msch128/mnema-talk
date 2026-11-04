package wiki

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
)

// Edited says what a change did, for the audit log.
type Edited struct {
	Page *Page
	// Revised: a new revision was written (title or text changed).
	Revised bool
	// Renamed, Moved: the address, or the place in the tree, changed.
	Renamed, Moved bool
	// OldSlug is the address before a rename.
	OldSlug string
}

// insertRevision writes revision rev of a page.
func insertRevision(ctx context.Context, q db.Querier, page uuid.UUID, rev int, editor uuid.UUID, t text, restoredFrom *int) error {
	_, err := q.Exec(ctx, `
		INSERT INTO wiki_revisions (page_id, rev, editor_id, title, content, summary, restored_from)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, page, rev, editor, t.title, t.content, t.summary, restoredFrom)
	if err != nil {
		return fmt.Errorf("write wiki revision: %w", err)
	}
	return nil
}

// pruneRevisions thins the history of a page: the latest KeepRevisions stay,
// and so does, for KeepRevisionDays, the last revision of every editor and
// day, so a flood of saves by one account cannot push everyone else's good
// revisions out. A revision a kept restore points to stays too.
func pruneRevisions(ctx context.Context, q db.Querier, page uuid.UUID, latest int) error {
	cutoff := latest - KeepRevisions // revisions up to this one are older than the latest KeepRevisions
	if cutoff < 1 {
		return nil
	}
	since := time.Now().AddDate(0, 0, -KeepRevisionDays)
	_, err := q.Exec(ctx, `
		DELETE FROM wiki_revisions r
		WHERE r.page_id = $1 AND r.rev <= $2
		  AND NOT EXISTS (SELECT 1 FROM wiki_revisions k
		                  WHERE k.page_id = r.page_id AND k.restored_from = r.rev AND k.rev > $2)
		  AND NOT (r.created_at >= $3 AND r.rev = (
		        SELECT max(d.rev) FROM wiki_revisions d
		        WHERE d.page_id = r.page_id AND d.editor_id IS NOT DISTINCT FROM r.editor_id
		          AND d.created_at::date = r.created_at::date))`, page, cutoff, since)
	if err != nil {
		return fmt.Errorf("prune wiki revisions: %w", err)
	}
	return nil
}

// save writes the new state of a page as the next revision.
func save(ctx context.Context, tx pgx.Tx, cur *Page, user uuid.UUID, t text, restoredFrom *int) error {
	rev := cur.Rev + 1
	if t.title != cur.Title {
		if err := checkTitle(ctx, tx, t.title, cur.ID); err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, `
		UPDATE wiki_pages SET title = $2, content = $3, rev = $4, updated_by = $5, updated_at = NOW() WHERE id = $1`,
		cur.ID, t.title, t.content, rev, user)
	if err != nil {
		return fmt.Errorf("save wiki page: %w", err)
	}
	if err := insertRevision(ctx, tx, cur.ID, rev, user, t, restoredFrom); err != nil {
		return err
	}
	return pruneRevisions(ctx, tx, cur.ID, rev)
}

// Change applies a PATCH. A change of the title or text writes a revision and
// needs BaseRev to be the page's latest, else it is refused (errStale); it also
// needs the page unlocked, unless manage. The tree fields (address, parent,
// position) only apply with manage and write no revision.
func (s *Store) Change(ctx context.Context, user uuid.UUID, slug string, ch PageChange, manage bool) (*Edited, error) {
	t, err := cleanText(ch.Title, ch.Content, ch.Summary)
	if err != nil {
		return nil, err
	}
	if err := checkChange(ch); err != nil {
		return nil, err
	}
	var out Edited
	err = pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		if err := lockTree(ctx, tx); err != nil {
			return err
		}
		cur, err := getPage(ctx, tx, slug, false, true)
		if err != nil {
			return err
		}
		out.OldSlug = cur.Slug
		if err := reviseText(ctx, tx, cur, user, t, ch, manage, &out); err != nil {
			return err
		}
		if err := moveInTree(ctx, tx, cur, ch, &out); err != nil {
			return err
		}
		out.Page, err = getPage(ctx, tx, cur.Slug, false, false)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// checkChange refuses a change that is wrong whatever the page is.
func checkChange(ch PageChange) error {
	if ch.Position != nil && (*ch.Position < 0 || *ch.Position > MaxPosition) {
		return errPosition
	}
	if (ch.Title != nil || ch.Content != nil) && ch.BaseRev < 1 {
		return errBaseRev
	}
	return nil
}

// reviseText writes the revision of a changed title or text.
func reviseText(ctx context.Context, tx pgx.Tx, cur *Page, user uuid.UUID, t text, ch PageChange, manage bool, out *Edited) error {
	next := text{title: cur.Title, content: cur.Content, summary: t.summary}
	if ch.Title != nil {
		next.title = t.title
	}
	if ch.Content != nil {
		next.content = t.content
	}
	if next.title == cur.Title && next.content == cur.Content {
		return nil
	}
	if cur.Locked && !manage {
		return errLocked
	}
	if ch.BaseRev != cur.Rev {
		return errStale
	}
	if err := save(ctx, tx, cur, user, next, nil); err != nil {
		return err
	}
	cur.Rev++
	out.Revised = true
	return nil
}

// moveInTree applies the address, parent and position of a change.
func moveInTree(ctx context.Context, tx pgx.Tx, cur *Page, ch PageChange, out *Edited) error {
	if ch.Slug != nil && *ch.Slug != cur.Slug {
		if err := rename(ctx, tx, cur, ch.Slug); err != nil {
			return err
		}
		out.Renamed = true
	}
	if ch.ParentID != nil {
		moved, err := reparent(ctx, tx, cur, *ch.ParentID)
		if err != nil {
			return err
		}
		out.Moved = out.Moved || moved
	}
	if ch.Position != nil && *ch.Position != cur.Position {
		if _, err := tx.Exec(ctx, `UPDATE wiki_pages SET position = $2 WHERE id = $1`, cur.ID, *ch.Position); err != nil {
			return err
		}
		out.Moved = true
	}
	return nil
}

// rename gives the page a new address and leaves a redirect from the old one.
func rename(ctx context.Context, tx pgx.Tx, cur *Page, to *string) error {
	slug := *to
	if !ValidSlug(slug) {
		return errSlugInvalid
	}
	// An address of the page's own old redirects is free again.
	if _, err := tx.Exec(ctx, `DELETE FROM wiki_redirects WHERE slug = $1 AND page_id = $2`, slug, cur.ID); err != nil {
		return err
	}
	if taken, err := slugTaken(ctx, tx, slug); err != nil || taken {
		if err != nil {
			return err
		}
		return errSlugTaken
	}
	if _, err := tx.Exec(ctx, `UPDATE wiki_pages SET slug = $2 WHERE id = $1`, cur.ID, slug); err != nil {
		return fmt.Errorf("rename wiki page: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO wiki_redirects (slug, page_id) VALUES ($1, $2)`, cur.Slug, cur.ID); err != nil {
		return fmt.Errorf("leave wiki redirect: %w", err)
	}
	cur.Slug = slug
	return nil
}

// reparent moves the page (and its subpages) below parent; uuid.Nil is the top.
func reparent(ctx context.Context, tx pgx.Tx, cur *Page, parent uuid.UUID) (bool, error) {
	var target *uuid.UUID
	if parent != uuid.Nil {
		target = &parent
	}
	if (target == nil && cur.ParentID == nil) || (target != nil && cur.ParentID != nil && *target == *cur.ParentID) {
		return false, nil
	}
	if target != nil {
		if under, err := isUnder(ctx, tx, *target, cur.ID); err != nil || under {
			if err != nil {
				return false, err
			}
			return false, errMoveIntoSelf
		}
	}
	height, err := heightOf(ctx, tx, cur.ID)
	if err != nil {
		return false, err
	}
	if err := checkRoom(ctx, tx, target, height); err != nil {
		return false, err
	}
	pos, err := nextPosition(ctx, tx, target)
	if err != nil {
		return false, err
	}
	_, err = tx.Exec(ctx, `UPDATE wiki_pages SET parent_id = $2, position = $3 WHERE id = $1`, cur.ID, target, pos)
	return true, err
}

// RestoreRevision makes revision rev the page's text again, as a new revision.
// in.BaseRev is the latest revision the editor saw; the restore is refused when
// the page moved on, is locked (without manage), or already is that revision.
func (s *Store) RestoreRevision(ctx context.Context, user uuid.UUID, slug string, rev int, in RestoreInput, manage bool) (*Page, error) {
	summary, err := cleanText(nil, nil, in.Summary)
	if err != nil {
		return nil, err
	}
	if in.BaseRev < 1 {
		return nil, errBaseRev
	}
	var out *Page
	err = pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		if err := lockTree(ctx, tx); err != nil {
			return err
		}
		cur, err := getPage(ctx, tx, slug, false, true)
		if err != nil {
			return err
		}
		target, err := restorable(ctx, tx, cur, rev, in.BaseRev, manage)
		if err != nil {
			return err
		}
		if err := save(ctx, tx, cur, user, text{title: target.Title, content: *target.Content, summary: summary.summary}, &rev); err != nil {
			return err
		}
		out, err = getPage(ctx, tx, cur.Slug, false, false)
		return err
	})
	return out, err
}

// restorable loads the revision to restore and refuses a restore that must not happen.
func restorable(ctx context.Context, tx pgx.Tx, cur *Page, rev, baseRev int, manage bool) (Revision, error) {
	if cur.Locked && !manage {
		return Revision{}, errLocked
	}
	if baseRev != cur.Rev {
		return Revision{}, errStale
	}
	target, err := loadRevision(ctx, tx, cur.ID, rev)
	if err != nil {
		return target, err
	}
	if target.Title == cur.Title && *target.Content == cur.Content {
		return target, errNothingNew
	}
	return target, nil
}
