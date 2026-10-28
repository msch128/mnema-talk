package server

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
)

type failingRow struct{}

func (failingRow) Scan(...any) error { return errors.New("context deadline exceeded") }

// failingQuerier answers every single-row query with an error, like a
// database that is too slow for the health context.
type failingQuerier struct{ db.Querier }

func (failingQuerier) QueryRow(context.Context, string, ...any) pgx.Row { return failingRow{} }

func TestMigrationStatusErrorIsNotAllPending(t *testing.T) {
	m, err := migrationStatus(context.Background(), failingQuerier{})
	if err == nil {
		t.Fatal("expected an error")
	}
	if m.pending != 0 || m.applied != 0 {
		t.Fatalf("a failed query must not report migrations as pending: %+v", m)
	}
}

func TestMediaTotalsError(t *testing.T) {
	if _, err := mediaTotals(context.Background(), failingQuerier{}); err == nil {
		t.Fatal("expected an error")
	}
}
