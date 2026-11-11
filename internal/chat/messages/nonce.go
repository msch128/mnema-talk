package messages

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// The client nonce makes a send idempotent (migration 0100): a member's second
// send of the same nonce returns the first message, or, when that message was
// deleted meanwhile, says so instead of posting the text again.

const (
	nonceKey = "message_nonces_pkey"
	// nonceLifetime is how long a retry can still find its nonce.
	nonceLifetime = 24 * time.Hour
)

var errNonceMessageDeleted = httpx.NewAPIError(http.StatusGone, httpx.CodeMessageDeleted, "this message was deleted and is not sent again")

// IsNonceConflict reports whether err is the primary key refusing a second
// send with the same author and nonce.
func IsNonceConflict(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == nonceKey
}

// recordNonce remembers the nonce of a new message in the transaction that
// stores it.
func recordNonce(ctx context.Context, tx pgx.Tx, nm NewMessage, id uuid.UUID) error {
	if nm.Nonce == nil {
		return nil
	}
	_, err := tx.Exec(ctx, `INSERT INTO message_nonces (user_id, nonce, channel_id, message_id) VALUES ($1, $2, $3, $4)`,
		nm.UserID, *nm.Nonce, nm.ChannelID, id)
	return err
}

// PruneNonces forgets nonces older than a day: no client retries that late.
func PruneNonces(ctx context.Context, q db.Querier) (int64, error) {
	tag, err := q.Exec(ctx, `DELETE FROM message_nonces WHERE created_at < now() - make_interval(secs => $1)`, nonceLifetime.Seconds())
	return tag.RowsAffected(), err
}
