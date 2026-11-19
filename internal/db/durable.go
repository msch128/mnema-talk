package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// AsyncCommit makes the commit of tx return without waiting for its WAL to
// reach the disk (synchronous_commit = off for this transaction only). Its
// row locks are released and its rows become visible as soon as the commit
// record is in the WAL buffers. The caller must call AwaitDurable after the
// commit and before it tells anybody that the transaction happened.
//
// It is meant for a transaction that has to hold a hot lock until its commit:
// with a synchronous commit, the lock is held through the WAL flush, so every
// transaction queued on it pays one full flush after another and none can
// share a flush with another (group commit). Committed asynchronously and
// flushed afterwards, the waiters queue only for the work in memory, and the
// flushes of concurrent transactions are merged into one.
//
// Between the commit and the flush, other sessions can already read the rows.
// Should PostgreSQL itself crash in that moment (a few milliseconds), the
// transaction is gone after the restart although a reader saw it. Nothing
// durable can depend on it: any later synchronous commit flushes the WAL
// before its own record, this one's included.
func AsyncCommit(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `SET LOCAL synchronous_commit = off`); err != nil {
		return fmt.Errorf("asynchronous commit: %w", err)
	}
	return nil
}

// durableTimeout bounds AwaitDurable; durableAttempts is how often it tries.
const (
	durableTimeout  = 10 * time.Second
	durableAttempts = 3
)

// AwaitDurable returns once every transaction committed on this server before
// the call is on disk (and on the synchronous standbys, if any). The WAL is
// one sequence, so flushing it up to a record of our own covers every earlier
// commit record, among them those of AsyncCommit transactions. Concurrent
// calls share one flush.
//
// The record is a transactional logical message, committed on its own. A
// transaction id alone is not enough: PostgreSQL flushes at commit only when
// the transaction wrote WAL before its commit record. The message writes such
// WAL, needs no table and no special privilege, and is ignored unless a logical
// replication slot asks for messages with this prefix.
//
// It takes the pool, not a transaction: the statement must commit on its own.
// The caller's cancellation does not cut it short (the transaction it waits
// for is committed already; a client that went away must not turn that into
// a failure); durableTimeout bounds it instead. A failed attempt is tried
// again on another statement: an error here answers a request whose change
// is stored, so only a database that stays unreachable (as with a commit
// whose answer is lost) may give one. When the administrator turned
// synchronous_commit off for the whole server, it flushes nothing, like every
// other commit there.
func AwaitDurable(ctx context.Context, p *Pool) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), durableTimeout)
	defer cancel()
	var err error
	for attempt := range durableAttempts {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return fmt.Errorf("wait for durable commit: %w", err)
			case <-time.After(time.Duration(attempt) * 50 * time.Millisecond):
			}
		}
		if _, err = p.Exec(ctx, `SELECT pg_logical_emit_message(true, 'mnema-durable', '')`); err == nil {
			return nil
		}
	}
	return fmt.Errorf("wait for durable commit: %w", err)
}
