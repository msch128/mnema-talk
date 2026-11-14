package push

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// brokenQuerier fails every query.
type brokenQuerier struct{}

func (brokenQuerier) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("down")
}
func (brokenQuerier) QueryRow(context.Context, string, ...any) pgx.Row { return nil }
func (brokenQuerier) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("down")
}

func TestMentionLabelsLeaveTheUsernameWhenNothingCanBeRead(t *testing.T) {
	if got := mentionLabels(context.Background(), brokenQuerier{}, "hi @carol"); got != nil {
		t.Fatalf("labels after a failed read: %v", got)
	}
	// Without a mention the database is not asked at all.
	if got := mentionLabels(context.Background(), brokenQuerier{}, "no mention here"); got != nil {
		t.Fatalf("labels without a mention: %v", got)
	}
}
