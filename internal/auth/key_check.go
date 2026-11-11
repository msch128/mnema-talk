package auth

import (
	"context"
	"encoding/binary"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
)

// fingerprint names a key without revealing it: four bytes of a keyed hash
// of the root, under a purpose nothing else uses. The server logs the sealing
// key's at start and `mnema-talk rotate-keys` prints it, so an operator can
// see whether both ran with the same settings; new recovery codes store it,
// so codes under a retired key can be told apart.
func (k dataKey) fingerprint() uint32 {
	return binary.BigEndian.Uint32(k.derive("key-fingerprint"))
}

// readableFingerprints are the fingerprints of every key stored data may
// still be under.
func (s *Sessions) readableFingerprints() []int64 {
	keys := s.readKeys()
	out := make([]int64, 0, len(keys))
	for _, k := range keys {
		out = append(out, int64(k.fingerprint()))
	}
	return out
}

// KeySetup names the keys a configuration uses for the second factor's data,
// without any key material.
type KeySetup struct {
	// Sealing is where the key new data is written under comes from.
	Sealing string
	// Fingerprint identifies the sealing key (eight hex digits, not secret).
	Fingerprint string
	// Previous are the configured variables that name a replaced key.
	Previous []string
}

// SessionDerivedKey is how the key without a DATA_KEY is called in output.
const SessionDerivedKey = "the key derived from JWT_SECRET"

// KeySetup describes the configured keys.
func (s *Sessions) KeySetup() KeySetup {
	k := KeySetup{Sealing: SessionDerivedKey, Fingerprint: fmt.Sprintf("%08x", s.sealingKey().fingerprint())}
	if s.DataKey != "" {
		k.Sealing = "DATA_KEY"
	}
	if s.PreviousSecret != "" {
		k.Previous = append(k.Previous, "JWT_SECRET_PREVIOUS")
	}
	if s.DataKeyPrevious != "" {
		k.Previous = append(k.Previous, "DATA_KEY_PREVIOUS")
	}
	return k
}

// StoredSecrets is what CheckStoredSecrets found.
type StoredSecrets struct {
	// Unreadable is how many accounts with two-factor sign-in on have an
	// authenticator secret that no configured key opens: their second step
	// fails until the key is configured again or the account is reset.
	Unreadable int
	// UnknownDataKey is how many of those name a data key that is not
	// configured: DATA_KEY was removed or replaced without DATA_KEY_PREVIOUS.
	UnknownDataKey int
}

// CheckStoredSecrets counts the authenticator secrets the configured keys
// cannot open. It only reads; unconfirmed enrolments are left out, because
// two-factor sign-in is not on for them.
func (s *Sessions) CheckStoredSecrets(ctx context.Context, q db.Querier) (StoredSecrets, error) {
	var res StoredSecrets
	rows, err := q.Query(ctx, `SELECT user_id, secret_enc FROM user_totp WHERE confirmed_at IS NOT NULL`)
	if err != nil {
		return res, err
	}
	var id uuid.UUID
	var sealed []byte
	_, err = pgx.ForEachRow(rows, []any{&id, &sealed}, func() error {
		if _, _, err := s.openSecret(id, sealed); err != nil {
			res.Unreadable++
			if s.namesUnknownDataKey(sealed) {
				res.UnknownDataKey++
			}
		}
		return nil
	})
	return res, err
}

// namesUnknownDataKey reports whether a blob carries the id of a data key
// that is not among the configured ones.
func (s *Sessions) namesUnknownDataKey(sealed []byte) bool {
	id, _, ok := parseSealed(sealed)
	if !ok || id == 0 {
		return false
	}
	for _, k := range s.readKeys() {
		if k.id == id {
			return false
		}
	}
	return true
}
