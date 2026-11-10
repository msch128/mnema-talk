package db

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	// DefaultMaxConns is the pool size when DB_MAX_CONNS is not set.
	DefaultMaxConns = 25
	// MaxPoolConns is the largest pool size accepted.
	MaxPoolConns = 1000
	// DefaultStatementTimeout is the longest one statement may run when
	// DB_STATEMENT_TIMEOUT is not set.
	DefaultStatementTimeout = 15 * time.Second
	// BulkStatementTimeout is the limit for work that rightly takes longer
	// than a request: the data export, the sweeps, an import, deleting a
	// channel or an account. Pass it to WithStatementTimeout.
	BulkStatementTimeout = 5 * time.Minute
	// idleInTransactionTimeout ends a session that holds a transaction open
	// without running anything, so a stuck caller cannot keep locks and a
	// connection forever. The longest deliberate pause inside a transaction
	// is hashing a password (password reset), far below this.
	idleInTransactionTimeout = time.Minute
)

// Options are the limits of a pool.
type Options struct {
	// MaxConns is the most connections the pool opens.
	MaxConns int
	// StatementTimeout is the longest one statement may run on the pool's
	// connections; 0 sets no limit.
	StatementTimeout time.Duration
}

// DefaultOptions are the limits used when nothing is configured.
func DefaultOptions() Options {
	return Options{MaxConns: DefaultMaxConns, StatementTimeout: DefaultStatementTimeout}
}

type statementTimeoutKey struct{}

// WithStatementTimeout marks ctx so that statements run with it may take up
// to d instead of the pool's limit; 0 lifts the limit. It only ever raises the
// limit: a d below the pool's own is ignored. The mark applies to connections
// acquired with ctx (every pool call and every transaction begun with it).
func WithStatementTimeout(ctx context.Context, d time.Duration) context.Context {
	return context.WithValue(ctx, statementTimeoutKey{}, d)
}

// statementLimit keeps each pooled connection's statement_timeout at what
// the acquiring context asks for. The timeouts are session settings of the
// pool's own connections, never of the database or the role, so pg_dump and
// psql in the backup scripts stay unaffected.
type statementLimit struct {
	base time.Duration
}

// customDataKey is where a connection remembers a limit that differs from
// the one it was opened with.
const customDataKey = "mnema.statement_timeout"

// wanted is the limit a connection acquired with ctx must have.
func (l statementLimit) wanted(ctx context.Context) time.Duration {
	d, marked := ctx.Value(statementTimeoutKey{}).(time.Duration)
	if !marked || l.base == 0 {
		return l.base
	}
	if d == 0 || d > l.base {
		return d
	}
	return l.base
}

// prepare runs before a connection is handed out. Only a connection whose
// limit differs from the wanted one costs a round trip: the first use for
// bulk work and the first ordinary use after it.
func (l statementLimit) prepare(ctx context.Context, conn *pgx.Conn) (bool, error) {
	want := l.wanted(ctx)
	data := conn.PgConn().CustomData()
	current, changed := data[customDataKey].(time.Duration)
	if !changed {
		current = l.base
	}
	if current == want {
		return true, nil
	}
	if _, err := conn.Exec(ctx, "SET statement_timeout = "+milliseconds(want)); err != nil {
		return false, fmt.Errorf("set statement timeout: %w", err)
	}
	data[customDataKey] = want
	return true, nil
}

func milliseconds(d time.Duration) string {
	return strconv.FormatInt(d.Milliseconds(), 10)
}

// IsStatementTimeout reports whether err is the database cancelling a
// statement that ran into statement_timeout (SQLSTATE 57014).
func IsStatementTimeout(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "57014"
}
