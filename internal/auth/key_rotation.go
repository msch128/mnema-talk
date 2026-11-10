package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
)

// reseal stores secret under the sealing key in place of the blob it was read
// from. It reports false when the row changed in the meantime (a new
// enrolment, a reset): then there is nothing left to move.
func (s *Sessions) reseal(ctx context.Context, q db.Querier, id uuid.UUID, old, secret []byte) (bool, error) {
	sealed, err := s.sealSecret(id, secret)
	if err != nil {
		return false, err
	}
	tag, err := q.Exec(ctx, `UPDATE user_totp SET secret_enc = $3 WHERE user_id = $1 AND secret_enc = $2`, id, old, sealed)
	return err == nil && tag.RowsAffected() == 1, err
}

// ErrUnreadableSecrets is ResealSecrets refusing to move anything because a
// stored secret opens with no configured key; KeyRotation.Unreadable names
// the accounts.
var ErrUnreadableSecrets = errors.New("stored authenticator secrets cannot be read with any configured key")

// KeyRotation is what ResealSecrets found.
type KeyRotation struct {
	// Moved secrets were under a previous key and are now under the current one.
	Moved int
	// Current secrets were under the current key already.
	Current int
	// Unreadable names the accounts whose secret no configured key opens.
	Unreadable []string
	// RecoveryAccounts is how many accounts hold unused recovery codes. Their
	// hashes cannot be moved; codes made before a rotation need the previous key.
	RecoveryAccounts int
}

// ResealSecrets moves every stored authenticator secret that is under a
// previous key to the current one, in one transaction: either all of them
// move or, on any error or unreadable secret, none does. It is safe while the
// server runs (the rows are locked for the moment it takes) and a second run
// finds nothing to do.
func (s *Sessions) ResealSecrets(ctx context.Context, p *db.Pool) (KeyRotation, error) {
	ctx = db.WithStatementTimeout(ctx, db.BulkStatementTimeout)
	var res KeyRotation
	err := pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		var err error
		res, err = s.resealAll(ctx, tx)
		return err
	})
	if err != nil {
		res.Moved = 0 // rolled back
	}
	return res, err
}

func (s *Sessions) resealAll(ctx context.Context, tx pgx.Tx) (KeyRotation, error) {
	type row struct {
		ID     uuid.UUID
		Name   string
		Sealed []byte
	}
	var res KeyRotation
	rows, err := tx.Query(ctx, `SELECT t.user_id, u.username, t.secret_enc FROM user_totp t JOIN users u ON u.id = t.user_id
		ORDER BY u.username FOR UPDATE OF t`)
	if err != nil {
		return res, err
	}
	all, err := pgx.CollectRows(rows, pgx.RowToStructByPos[row])
	if err != nil {
		return res, err
	}
	type pending struct {
		row
		secret []byte
	}
	var stale []pending
	for _, r := range all {
		secret, old, err := s.openSecret(r.ID, r.Sealed)
		switch {
		case err != nil:
			res.Unreadable = append(res.Unreadable, r.Name)
		case old:
			stale = append(stale, pending{r, secret})
		default:
			res.Current++
		}
	}
	if len(res.Unreadable) > 0 {
		return res, ErrUnreadableSecrets
	}
	for _, r := range stale {
		moved, err := s.reseal(ctx, tx, r.ID, r.Sealed, r.secret)
		if err != nil {
			return res, err
		}
		if !moved {
			return res, fmt.Errorf("the authenticator secret of %s changed while it was locked", r.Name)
		}
		res.Moved++
	}
	err = tx.QueryRow(ctx, `SELECT COUNT(DISTINCT user_id) FROM recovery_codes WHERE used_at IS NULL`).Scan(&res.RecoveryAccounts)
	return res, err
}
