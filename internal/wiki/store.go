package wiki

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// The refusals of the store. Their texts are part of the API (doc/wiki.md).
var (
	errPageNotFound = httpx.ErrNotFound("wiki page not found")
	errSlugTaken    = httpx.ErrConflict("that address is already used by a page or a redirect")
	errSlugInvalid  = httpx.ErrInvalidInput("the address needs lower-case letters, digits and single hyphens, and must not be a reserved word")
	errStale        = httpx.ErrConflict("the page changed since you opened it; open it again and apply your change to the new version")
	errLocked       = httpx.ErrForbidden("this page is locked; only holders of Manage wiki can change it")
	errTooLong      = httpx.ErrInvalidInput("a wiki page holds at most 50 KB of text")
	errTooMany      = httpx.ErrConflict("the wiki holds at most 500 pages")
	errTooDeep      = httpx.ErrConflict("wiki pages nest at most 5 levels")
	errHasChildren  = httpx.ErrConflict("move or delete the subpages first")
	errMoveIntoSelf = httpx.ErrConflict("a page cannot move under itself or one of its subpages")
	errParentGone   = httpx.ErrNotFound("parent page not found")
	errBaseRev      = httpx.ErrInvalidInput("base_rev, the revision you edited, is required")
	errNothingNew   = httpx.ErrInvalidInput("that is the page as it is")
	errNotDeleted   = httpx.ErrConflict("the page is not deleted")
	errRevGone      = httpx.ErrNotFound("revision not found")
	errTitleTaken   = httpx.ErrConflict("another wiki page already has that title")
	errPosition     = httpx.ErrInvalidInput("position must be between 0 and 1048576")
)

// Store keeps the wiki in the database.
type Store struct{ DB *db.Pool }

// NewStore returns the store of the wiki.
func NewStore(p *db.Pool) *Store { return &Store{DB: p} }

// pageColumns is what a page query selects, in the order scanPage reads it.
const pageColumns = `p.id, p.slug, p.title, p.parent_id, p.position, p.locked, p.updated_at, p.deleted_at,
	p.content, p.rev, p.created_at,
	cu.id, cu.username, cu.display_name, uu.id, uu.username, uu.display_name`

const pageFrom = ` FROM wiki_pages p
	LEFT JOIN users cu ON cu.id = p.created_by
	LEFT JOIN users uu ON uu.id = p.updated_by`

func scanPage(row pgx.Row) (*Page, error) {
	var p Page
	var cID, uID *uuid.UUID
	var cName, cDisplay, uName, uDisplay *string
	err := row.Scan(&p.ID, &p.Slug, &p.Title, &p.ParentID, &p.Position, &p.Locked, &p.UpdatedAt, &p.DeletedAt,
		&p.Content, &p.Rev, &p.CreatedAt, &cID, &cName, &cDisplay, &uID, &uName, &uDisplay)
	if err != nil {
		return nil, err
	}
	p.CreatedBy, p.UpdatedBy = userRef(cID, cName, cDisplay), userRef(uID, uName, uDisplay)
	return &p, nil
}

func userRef(id *uuid.UUID, name, display *string) *UserRef {
	if id == nil || name == nil || display == nil {
		return nil
	}
	return &UserRef{ID: *id, Username: *name, DisplayName: *display}
}

// Tree lists the pages without their text, in tree order (siblings by position, then title). Deleted pages only with deleted.
func (s *Store) Tree(ctx context.Context, deleted bool) ([]PageSummary, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT id, slug, title, parent_id, position, locked, updated_at, deleted_at FROM wiki_pages
		WHERE $1 OR deleted_at IS NULL ORDER BY position, lower(title), id`, deleted)
	if err != nil {
		return nil, fmt.Errorf("list wiki pages: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (PageSummary, error) {
		var p PageSummary
		err := r.Scan(&p.ID, &p.Slug, &p.Title, &p.ParentID, &p.Position, &p.Locked, &p.UpdatedAt, &p.DeletedAt)
		return p, err
	})
}

// Get returns the page at slug, following a redirect of a renamed page. A
// deleted page is only found with deleted (the holders of Manage wiki).
func (s *Store) Get(ctx context.Context, slug string, deleted bool) (*Page, error) {
	return getPage(ctx, s.DB, slug, deleted, false)
}

// getPage is Get inside a transaction or not; lock takes the row for update.
func getPage(ctx context.Context, q db.Querier, slug string, deleted, lock bool) (*Page, error) {
	suffix := ""
	if lock {
		suffix = " FOR UPDATE OF p"
	}
	p, err := scanPage(q.QueryRow(ctx, `SELECT `+pageColumns+pageFrom+` WHERE p.slug = $1`+suffix, slug))
	if errors.Is(err, pgx.ErrNoRows) {
		p, err = scanPage(q.QueryRow(ctx, `SELECT `+pageColumns+pageFrom+
			` JOIN wiki_redirects r ON r.page_id = p.id WHERE r.slug = $1`+suffix, slug))
		if err == nil {
			p.RedirectedFrom = slug
		}
	}
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && p.DeletedAt != nil && !deleted) {
		return nil, errPageNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load wiki page: %w", err)
	}
	return p, nil
}

// depthOf is the level of the page: 1 for a page at the top, 0 for no page.
func depthOf(ctx context.Context, q db.Querier, id *uuid.UUID) (int, error) {
	if id == nil {
		return 0, nil
	}
	var depth int
	err := q.QueryRow(ctx, `
		WITH RECURSIVE up AS (SELECT id, parent_id, 1 AS d FROM wiki_pages WHERE id = $1
			UNION ALL SELECT p.id, p.parent_id, up.d + 1 FROM wiki_pages p JOIN up ON p.id = up.parent_id WHERE up.d < 20)
		SELECT COALESCE(MAX(d), 0) FROM up`, *id).Scan(&depth)
	return depth, err
}

// heightOf is how many levels the page and its subpages take: 1 for a page without subpages.
func heightOf(ctx context.Context, q db.Querier, id uuid.UUID) (int, error) {
	var height int
	err := q.QueryRow(ctx, `
		WITH RECURSIVE down AS (SELECT id, 1 AS d FROM wiki_pages WHERE id = $1
			UNION ALL SELECT p.id, down.d + 1 FROM wiki_pages p JOIN down ON p.parent_id = down.id WHERE down.d < 20)
		SELECT COALESCE(MAX(d), 1) FROM down`, id).Scan(&height)
	return height, err
}

// isUnder reports whether the page is the ancestor itself or lies below it.
func isUnder(ctx context.Context, q db.Querier, page, ancestor uuid.UUID) (bool, error) {
	var found bool
	err := q.QueryRow(ctx, `
		WITH RECURSIVE down AS (SELECT id FROM wiki_pages WHERE id = $2
			UNION SELECT p.id FROM wiki_pages p JOIN down ON p.parent_id = down.id)
		SELECT EXISTS (SELECT 1 FROM down WHERE id = $1)`, page, ancestor).Scan(&found)
	return found, err
}

// lockTree serializes the writes that look at the whole tree (counts, depth,
// addresses) for the rest of the transaction.
func lockTree(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('mnema.wiki'))`)
	return err
}
