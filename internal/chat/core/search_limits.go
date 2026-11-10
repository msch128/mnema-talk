package core

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// searchStatementTimeout bounds one search query inside the database.
const searchStatementTimeout = "5s"

// ErrSearchSlow answers a search the database cancelled for running too
// long. It is 429 like the other search limits; searches are expensive.
var ErrSearchSlow = httpx.NewAPIError(http.StatusTooManyRequests, httpx.CodeRateLimited, "the search took too long, narrow it down")

// SearchTx runs fn in a read-only transaction whose statements the database
// cancels after searchStatementTimeout. The caller maps that cancellation
// (db.IsStatementTimeout) to ErrSearchSlow.
func SearchTx(ctx context.Context, p *db.Pool, fn func(tx pgx.Tx) error) error {
	return pgx.BeginTxFunc(ctx, p, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SET LOCAL statement_timeout = '"+searchStatementTimeout+"'"); err != nil {
			return err
		}
		return fn(tx)
	})
}
