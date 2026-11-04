package wiki

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/userdata"
)

// accountData is the wiki in an export and an erasure. The pages belong to the
// community: erasing an account detaches the person from them and keeps the text.
type accountData struct{}

// RegisterAccountData adds the revisions a member saved and the erasure that
// detaches them from the wiki.
func RegisterAccountData(r *userdata.Registry) {
	var d accountData
	r.Register(d, d)
}

func (accountData) Export(ctx context.Context, q db.Querier, user uuid.UUID, _ userdata.Channels, limits userdata.Limits) ([]userdata.File, error) {
	return []userdata.File{{
		Name: "wiki_revisions.json", Order: userdata.FileWikiRevisions,
		Build: func() (any, bool, error) { return exportRevisions(ctx, q, user, limits) },
	}}, nil
}

type exportedRevision struct {
	Page      string    `json:"page" db:"page"`
	PageID    uuid.UUID `json:"-" db:"page_id"`
	Rev       int       `json:"rev" db:"rev"`
	Title     string    `json:"title" db:"title"`
	Content   string    `json:"content" db:"content"`
	Summary   string    `json:"summary" db:"summary"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// exportRevisions lists the revisions the member saved, oldest first, a page of
// rows at a time. A revision holds up to 50 KB of text, so the list stops at the
// same byte bound as the member's messages (and at the row bound) and says it
// was cut; it never holds every text of a busy editor at once.
func exportRevisions(ctx context.Context, q db.Querier, user uuid.UUID, limits userdata.Limits) (any, bool, error) {
	size := max(limits.Page, 1)
	var out []exportedRevision
	var bytes int
	var lastAt time.Time
	lastPage, lastRev := uuid.Nil, 0
	for {
		page, err := userdata.Collect(ctx, q, pgx.RowToStructByName[exportedRevision], `
			SELECT p.slug AS page, r.page_id, r.rev, r.title, r.content, r.summary, r.created_at
			FROM wiki_revisions r JOIN wiki_pages p ON p.id = r.page_id
			WHERE r.editor_id = $1 AND (r.created_at, r.page_id, r.rev) > ($2, $3, $4)
			ORDER BY r.created_at, r.page_id, r.rev LIMIT $5`, user, lastAt, lastPage, lastRev, size)
		if err != nil {
			return nil, false, err
		}
		for _, r := range page {
			if len(out) >= limits.MaxRows || bytes >= limits.MessageBytes {
				return userdata.AsList(out), true, nil
			}
			out = append(out, r)
			bytes += len(r.Content)
			lastAt, lastPage, lastRev = r.CreatedAt, r.PageID, r.Rev
		}
		if len(page) < size {
			return userdata.AsList(out), false, nil
		}
	}
}

func (accountData) Erase() []userdata.Step {
	return []userdata.Step{
		{Order: userdata.OrderWikiRevisions, SQL: `UPDATE wiki_revisions SET editor_id = NULL WHERE editor_id = $1`},
		{Order: userdata.OrderWikiCreated, SQL: `UPDATE wiki_pages SET created_by = NULL WHERE created_by = $1`},
		{Order: userdata.OrderWikiUpdated, SQL: `UPDATE wiki_pages SET updated_by = NULL WHERE updated_by = $1`},
		{Order: userdata.OrderWikiDeleted, SQL: `UPDATE wiki_pages SET deleted_by = NULL WHERE deleted_by = $1`},
	}
}
