package chat

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"io"
	"time"
)

var (
	ErrNativeContentInput       = errors.New("invalid native ciphertext request")
	ErrNativeContentConflict    = errors.New("native event identity already used")
	ErrNativeContentUnavailable = errors.New("native ciphertext store unavailable")
	ErrNativeContentChannel     = errors.New("native ciphertext channel unavailable")
)

// OpaqueCiphertextRequest contains routing metadata, not authenticated content.
// Clients must bind these values in their encrypted inner envelope and verify
// its approved channel/group mapping after MLS decryption. The server cannot.
type OpaqueCiphertextRequest struct {
	ChannelID     uuid.UUID
	ClientEventID uuid.UUID
	GroupID       []byte
	Ciphertext    []byte
}

// NativeCiphertextRecord exposes explicit wire copies only. Double indirection
// prevents fmt reflective fallback from walking bytes inside private wrappers.
// This prevents accidental formatting, not deliberate in-process inspection.
type NativeCiphertextRecord struct{ value **nativeCiphertextData }

type nativeCiphertextData struct {
	id            uuid.UUID
	number        int64
	channelID     uuid.UUID
	userID        uuid.UUID
	clientEventID uuid.UUID
	groupID       []byte
	ciphertext    []byte
	createdAt     time.Time
}

func (NativeCiphertextRecord) Format(s fmt.State, _ rune) {
	_, _ = io.WriteString(s, "[native ciphertext record redacted]")
}

type NativeCiphertextWire struct {
	ID            uuid.UUID `json:"id"`
	Number        int64     `json:"number"`
	ChannelID     uuid.UUID `json:"channel_id"`
	UserID        uuid.UUID `json:"user_id"`
	ClientEventID uuid.UUID `json:"client_event_id"`
	GroupID       []byte    `json:"group_id"`
	Ciphertext    []byte    `json:"ciphertext"`
	CreatedAt     time.Time `json:"created_at"`
}

func (r NativeCiphertextRecord) data() nativeCiphertextData {
	if r.value == nil || *r.value == nil {
		return nativeCiphertextData{}
	}
	return **r.value
}

func (r NativeCiphertextRecord) Wire() NativeCiphertextWire {
	v := r.data()
	return NativeCiphertextWire{v.id, v.number, v.channelID, v.userID, v.clientEventID, bytes.Clone(v.groupID), bytes.Clone(v.ciphertext), v.createdAt}
}

type NativeCiphertextStore struct{ sessions *auth.NativeSessions }

func NewNativeCiphertextStore(sessions *auth.NativeSessions) (*NativeCiphertextStore, error) {
	if sessions == nil {
		return nil, ErrNativeContentInput
	}
	return &NativeCiphertextStore{sessions: sessions}, nil
}

const nativeCiphertextColumns = `id,number,channel_id,user_id,client_event_id,group_id,ciphertext,created_at`

func scanNativeCiphertext(row pgx.Row) (NativeCiphertextRecord, error) {
	v := new(nativeCiphertextData)
	err := row.Scan(&v.id, &v.number, &v.channelID, &v.userID, &v.clientEventID, &v.groupID, &v.ciphertext, &v.createdAt)
	if err != nil {
		return NativeCiphertextRecord{}, err
	}
	return NativeCiphertextRecord{value: &v}, nil
}

// Publish commits identical stored bytes on an idempotent retry; it never
// reencrypts or silently replaces an existing event. The boolean means created.
// Writers serialize on the channel before allocating sequence numbers, so a
// concurrent reader cannot advance past an uncommitted lower-number event.
// No events are broadcast by this persistence-only foundation.
func (s *NativeCiphertextStore) Publish(ctx context.Context, principal auth.NativePrincipal, request OpaqueCiphertextRequest) (NativeCiphertextRecord, bool, error) {
	if request.ChannelID == uuid.Nil || request.ClientEventID == uuid.Nil || len(request.GroupID) < 1 || len(request.GroupID) > 128 || len(request.Ciphertext) < 1 || len(request.Ciphertext) > 65536 {
		return NativeCiphertextRecord{}, false, ErrNativeContentInput
	}
	request.GroupID = bytes.Clone(request.GroupID)
	request.Ciphertext = bytes.Clone(request.Ciphertext)
	var record NativeCiphertextRecord
	created := false
	err := s.sessions.WithAuthorizedTransaction(ctx, principal, func(ctx context.Context, tx pgx.Tx, user auth.User) error {
		var channel uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM channels WHERE id=$1 FOR UPDATE`, request.ChannelID).Scan(&channel); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNativeContentChannel
			}
			return ErrNativeContentUnavailable
		}
		var err error
		record, err = scanNativeCiphertext(tx.QueryRow(ctx, `INSERT INTO native_ciphertext_events
   (id,channel_id,user_id,client_event_id,group_id,ciphertext) VALUES($1,$2,$3,$4,$5,$6)
   ON CONFLICT(user_id,client_event_id) DO NOTHING RETURNING `+nativeCiphertextColumns,
			uuid.New(), request.ChannelID, user.ID, request.ClientEventID, request.GroupID, request.Ciphertext))
		if errors.Is(err, pgx.ErrNoRows) {
			record, err = scanNativeCiphertext(tx.QueryRow(ctx, `SELECT `+nativeCiphertextColumns+` FROM native_ciphertext_events WHERE user_id=$1 AND client_event_id=$2`, user.ID, request.ClientEventID))
			if err != nil {
				return ErrNativeContentUnavailable
			}
			if stored := record.data(); stored.channelID != request.ChannelID || !bytes.Equal(stored.groupID, request.GroupID) || !bytes.Equal(stored.ciphertext, request.Ciphertext) {
				return ErrNativeContentConflict
			}
			return nil
		}
		if err != nil {
			return ErrNativeContentUnavailable
		}
		created = true
		return nil
	})
	if err != nil {
		return NativeCiphertextRecord{}, false, err
	}
	return record, created, nil
}

// List paginates opaque events without claiming that any group is authorized.
// Channel visibility follows the current single-server text/voice model.
func (s *NativeCiphertextStore) List(ctx context.Context, principal auth.NativePrincipal, channelID uuid.UUID, after int64, limit int) ([]NativeCiphertextRecord, error) {
	if channelID == uuid.Nil || after < 0 || limit < 1 || limit > 100 {
		return nil, ErrNativeContentInput
	}
	records := make([]NativeCiphertextRecord, 0)
	err := s.sessions.WithAuthorizedTransaction(ctx, principal, func(ctx context.Context, tx pgx.Tx, _ auth.User) error {
		var channel uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM channels WHERE id=$1 FOR SHARE`, channelID).Scan(&channel); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNativeContentChannel
			}
			return ErrNativeContentUnavailable
		}
		rows, err := tx.Query(ctx, `SELECT `+nativeCiphertextColumns+` FROM native_ciphertext_events WHERE channel_id=$1 AND number>$2 ORDER BY number LIMIT $3`, channelID, after, limit)
		if err != nil {
			return ErrNativeContentUnavailable
		}
		defer rows.Close()
		for rows.Next() {
			record, err := scanNativeCiphertext(rows)
			if err != nil {
				return ErrNativeContentUnavailable
			}
			records = append(records, record)
		}
		if rows.Err() != nil {
			return ErrNativeContentUnavailable
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return records, nil
}
