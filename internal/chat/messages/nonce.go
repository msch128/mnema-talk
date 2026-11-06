package messages

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// The client nonce makes a send idempotent (migration 0100): a member's second
// send of the same nonce returns the first message.

const nonceIndex = "messages_client_nonce_idx"

// IsNonceConflict reports whether err is the unique index refusing a second
// message with the same author and nonce.
func IsNonceConflict(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == nonceIndex
}

// replayNonce answers a send whose nonce the author already used with the
// message it created (200, nothing broadcast again). It reports whether it
// answered. A nonce of the same author in another channel is refused.
func (a *API) replayNonce(w http.ResponseWriter, r *http.Request, userID, chID uuid.UUID, nonce *uuid.UUID) (bool, error) {
	if nonce == nil {
		return false, nil
	}
	var id, inChannel uuid.UUID
	err := a.DB.QueryRow(r.Context(), `SELECT id, channel_id FROM messages WHERE user_id = $1 AND client_nonce = $2`, userID, *nonce).Scan(&id, &inChannel)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if inChannel != chID {
		return true, httpx.ErrInvalidInput("nonce was already used in another conversation")
	}
	msg, err := GetMessage(r.Context(), a.DB, id)
	if err != nil {
		return true, err
	}
	msg.Nonce = nonce
	httpx.WriteJSON(w, http.StatusOK, msg)
	return true, nil
}
