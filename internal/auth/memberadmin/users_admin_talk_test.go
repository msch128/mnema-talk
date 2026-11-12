package memberadmin

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// talkRow answers the dm_id lookup: a public Talk, a call Talk, or an error.
type talkRow struct {
	public bool
	err    error
}

func (r talkRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(*bool) = r.public
	return nil
}

type talkQuerier struct{ row talkRow }

func (q talkQuerier) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("unexpected Exec")
}

func (q talkQuerier) QueryRow(context.Context, string, ...any) pgx.Row { return q.row }

func (q talkQuerier) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected Query")
}

// A removal is audited only from a Talk that exists and is no conversation's:
// a call Talk deleted in the meantime (its row gone) may have been a
// conversation's, and a failed lookup may hide one (H-L6-03, doc/talks.md).
func TestOnlyAPublicTalkThatExistsIsAudited(t *testing.T) {
	for name, tc := range map[string]struct {
		row  talkRow
		want bool
	}{
		"public Talk":      {talkRow{public: true}, true},
		"call Talk":        {talkRow{public: false}, false},
		"Talk gone":        {talkRow{err: pgx.ErrNoRows}, false},
		"lookup failed":    {talkRow{err: errors.New("down")}, false},
		"context canceled": {talkRow{err: context.Canceled}, false},
	} {
		if got := auditableTalk(context.Background(), talkQuerier{tc.row}, uuid.New()); got != tc.want {
			t.Errorf("%s: audited %v, want %v", name, got, tc.want)
		}
	}
}
