package talks

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
)

type isDMRow struct{ err error }

func (r isDMRow) Scan(...any) error { return r.err }

type isDMQuerier struct {
	db.Querier
	err error
}

func (q isDMQuerier) QueryRow(context.Context, string, ...any) pgx.Row { return isDMRow{q.err} }

// A failed lookup must not read as "not a conversation's Talk": callers audit
// only on a definite no, so an error has to surface.
func TestIsDMTalkReportsDatabaseErrors(t *testing.T) {
	boom := errors.New("connection reset")
	if is, err := IsDMTalk(context.Background(), isDMQuerier{err: boom}, uuid.New()); !errors.Is(err, boom) || is {
		t.Fatalf("is=%v err=%v", is, err)
	}
}

func TestIsDMTalkTreatsAMissingChannelAsNoConversationTalk(t *testing.T) {
	is, err := IsDMTalk(context.Background(), isDMQuerier{err: pgx.ErrNoRows}, uuid.New())
	if is || err != nil {
		t.Fatalf("is=%v err=%v", is, err)
	}
}
