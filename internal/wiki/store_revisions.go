package wiki

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
)

// revisionColumns selects a revision; the list leaves the text out (an empty string in its place), only its size is read.
func revisionColumns(withText bool) string {
	content := `''`
	if withText {
		content = `r.content`
	}
	return `r.rev, r.summary, r.restored_from, octet_length(r.content), r.created_at, r.title, ` + content + `,
	e.id, e.username, e.display_name`
}

func scanRevision(row pgx.Row, withText bool) (Revision, error) {
	var r Revision
	var content string
	var eID *uuid.UUID
	var eName, eDisplay *string
	err := row.Scan(&r.Rev, &r.Summary, &r.RestoredFrom, &r.Size, &r.CreatedAt, &r.Title, &content, &eID, &eName, &eDisplay)
	if err != nil {
		return r, err
	}
	r.Editor = userRef(eID, eName, eDisplay)
	if withText {
		r.Content = &content
	} else {
		r.Title = ""
	}
	return r, nil
}

// loadRevision returns revision rev of a page with its text.
func loadRevision(ctx context.Context, q db.Querier, page uuid.UUID, rev int) (Revision, error) {
	r, err := scanRevision(q.QueryRow(ctx, `SELECT `+revisionColumns(true)+`
		FROM wiki_revisions r LEFT JOIN users e ON e.id = r.editor_id WHERE r.page_id = $1 AND r.rev = $2`, page, rev), true)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, errRevGone
	}
	if err != nil {
		return r, fmt.Errorf("load wiki revision: %w", err)
	}
	return r, nil
}

// Revision returns one revision of the page at slug, with its text.
func (s *Store) Revision(ctx context.Context, slug string, rev int, deleted bool) (Revision, error) {
	p, err := getPage(ctx, s.DB, slug, deleted, false)
	if err != nil {
		return Revision{}, err
	}
	return loadRevision(ctx, s.DB, p.ID, rev)
}

// Revisions lists the page's revisions, newest first, without their text. before (0: none) continues after a page.
func (s *Store) Revisions(ctx context.Context, slug string, before, limit int, deleted bool) ([]Revision, error) {
	p, err := getPage(ctx, s.DB, slug, deleted, false)
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.Query(ctx, `SELECT `+revisionColumns(false)+`
		FROM wiki_revisions r LEFT JOIN users e ON e.id = r.editor_id
		WHERE r.page_id = $1 AND ($2 = 0 OR r.rev < $2) ORDER BY r.rev DESC LIMIT $3`, p.ID, before, limit)
	if err != nil {
		return nil, fmt.Errorf("list wiki revisions: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Revision, error) { return scanRevision(r, false) })
}
