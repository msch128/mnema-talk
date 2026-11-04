package stage

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// skipped is a database whose update touches no row and whose lookup of the
// channel afterwards fails with lookupErr.
type skipped struct{ lookupErr error }

func (skipped) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag("UPDATE 0"), nil
}

func (skipped) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unused")
}

func (s skipped) QueryRow(context.Context, string, ...any) pgx.Row { return failingRow{s.lookupErr} }

type failingRow struct{ err error }

func (r failingRow) Scan(...any) error { return r.err }

func TestAFailedLookupIsAServerErrorAndAMissingChannelIsNotFound(t *testing.T) {
	broken := errors.New("connection reset")
	err := SetStage(context.Background(), skipped{broken}, uuid.New(), true)
	var api *httpx.APIError
	if errors.As(err, &api) || !errors.Is(err, broken) {
		t.Fatalf("a failed lookup became %v, want the database error", err)
	}
	err = SetStage(context.Background(), skipped{pgx.ErrNoRows}, uuid.New(), true)
	if !errors.As(err, &api) || api.Status != http.StatusNotFound {
		t.Fatalf("a missing channel: %v, want 404", err)
	}
}
