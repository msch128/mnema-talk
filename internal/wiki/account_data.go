package wiki

import (
	"context"
	"time"

	"github.com/google/uuid"
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
	type revisionRow struct {
		Page      string    `json:"page" db:"page"`
		Rev       int       `json:"rev" db:"rev"`
		Title     string    `json:"title" db:"title"`
		Content   string    `json:"content" db:"content"`
		Summary   string    `json:"summary" db:"summary"`
		CreatedAt time.Time `json:"created_at" db:"created_at"`
	}
	return []userdata.File{{
		Name: "wiki_revisions.json", Order: userdata.FileWikiRevisions,
		Build: func() (any, bool, error) {
			rows, cut, err := userdata.FetchRows[revisionRow](ctx, q, limits.MaxRows, `SELECT p.slug AS page, r.rev, r.title, r.content, r.summary, r.created_at
		FROM wiki_revisions r JOIN wiki_pages p ON p.id = r.page_id
		WHERE r.editor_id = $1 ORDER BY r.created_at LIMIT $2`, user)
			if err != nil {
				return nil, false, err
			}
			return userdata.AsList(rows), cut, nil
		},
	}}, nil
}

func (accountData) Erase() []userdata.Step {
	return []userdata.Step{
		{Order: userdata.OrderWikiRevisions, SQL: `UPDATE wiki_revisions SET editor_id = NULL WHERE editor_id = $1`},
		{Order: userdata.OrderWikiCreated, SQL: `UPDATE wiki_pages SET created_by = NULL WHERE created_by = $1`},
		{Order: userdata.OrderWikiUpdated, SQL: `UPDATE wiki_pages SET updated_by = NULL WHERE updated_by = $1`},
		{Order: userdata.OrderWikiDeleted, SQL: `UPDATE wiki_pages SET deleted_by = NULL WHERE deleted_by = $1`},
	}
}
