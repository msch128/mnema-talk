package chat

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
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
	if _, err := io.WriteString(s, "[native ciphertext record redacted]"); err != nil {
		return
	}
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

type NativeCiphertextStore struct {
	sessions *auth.NativeSessions
	perms    *perm.Service
}

// WithPermissions makes the store enforce channel permissions: publishing
// needs to view the channel and send messages in it, listing needs to view it.
// A channel the caller may not use is reported as unavailable, like a missing
// one. Without it the store follows the single-server model of every member
// seeing every channel.
func (s *NativeCiphertextStore) WithPermissions(p *perm.Service) *NativeCiphertextStore {
	s.perms = p
	return s
}

// authorize checks need in channel for user.
func (s *NativeCiphertextStore) authorize(ctx context.Context, user auth.User, channel uuid.UUID, need perm.Set) error {
	if s.perms == nil {
		return nil
	}
	err := s.perms.RequireChannel(ctx, user.Subject(), channel, need)
	if err == nil {
		return nil
	}
	if _, denied := httpx.AsAPIError(err); denied {
		return ErrNativeContentChannel
	}
	return ErrNativeContentUnavailable
}

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
func ciphertextInputRejected(request OpaqueCiphertextRequest) bool {
	if request.ChannelID == uuid.Nil || request.ClientEventID == uuid.Nil {
		return true
	}
	if len(request.GroupID) < 1 || len(request.GroupID) > 128 {
		return true
	}
	return len(request.Ciphertext) < 1 || len(request.Ciphertext) > 65536
}

func (s *NativeCiphertextStore) publishInChannel(ctx context.Context, tx pgx.Tx, user auth.User, request OpaqueCiphertextRequest) (NativeCiphertextRecord, bool, error) {
	if err := lockNativeChannel(ctx, tx, nativeChannelLockWrite, request.ChannelID); err != nil {
		return NativeCiphertextRecord{}, false, err
	}
	if err := s.authorize(ctx, user, request.ChannelID, perm.SendMessages); err != nil {
		return NativeCiphertextRecord{}, false, err
	}
	return insertOrReplay(ctx, tx, user.ID, request)
}

func (s *NativeCiphertextStore) Publish(ctx context.Context, principal auth.NativePrincipal, request OpaqueCiphertextRequest) (NativeCiphertextRecord, bool, error) {
	if ciphertextInputRejected(request) {
		return NativeCiphertextRecord{}, false, ErrNativeContentInput
	}
	request.GroupID = bytes.Clone(request.GroupID)
	request.Ciphertext = bytes.Clone(request.Ciphertext)
	var record NativeCiphertextRecord
	created := false
	err := s.sessions.WithAuthorizedTransaction(ctx, principal, func(ctx context.Context, tx pgx.Tx, user auth.User) error {
		var err error
		record, created, err = s.publishInChannel(ctx, tx, user, request)
		return err
	})
	if err != nil {
		return NativeCiphertextRecord{}, false, err
	}
	return record, created, nil
}

// Channel row locks: writers lock exclusively, readers share.
const (
	nativeChannelLockWrite = `SELECT id FROM channels WHERE id=$1 FOR UPDATE`
	nativeChannelLockRead  = `SELECT id FROM channels WHERE id=$1 FOR SHARE`
)

// lockNativeChannel locks the channel row with the given lock query and
// reports a missing channel as ErrNativeContentChannel.
func lockNativeChannel(ctx context.Context, tx pgx.Tx, query string, id uuid.UUID) error {
	var channel uuid.UUID
	if err := tx.QueryRow(ctx, query, id).Scan(&channel); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNativeContentChannel
		}
		return ErrNativeContentUnavailable
	}
	return nil
}

// insertOrReplay stores the event, or on a retry with the same client event
// ID returns the stored one when its bytes are identical (created is false)
// and ErrNativeContentConflict when they differ.
func insertOrReplay(ctx context.Context, tx pgx.Tx, userID uuid.UUID, request OpaqueCiphertextRequest) (record NativeCiphertextRecord, created bool, err error) {
	record, err = scanNativeCiphertext(tx.QueryRow(ctx, `INSERT INTO native_ciphertext_events
   (id,channel_id,user_id,client_event_id,group_id,ciphertext) VALUES($1,$2,$3,$4,$5,$6)
   ON CONFLICT(user_id,client_event_id) DO NOTHING RETURNING `+nativeCiphertextColumns,
		uuid.New(), request.ChannelID, userID, request.ClientEventID, request.GroupID, request.Ciphertext))
	if err == nil {
		return record, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return record, false, ErrNativeContentUnavailable
	}
	record, err = scanNativeCiphertext(tx.QueryRow(ctx, `SELECT `+nativeCiphertextColumns+` FROM native_ciphertext_events WHERE user_id=$1 AND client_event_id=$2`, userID, request.ClientEventID))
	if err != nil {
		return record, false, ErrNativeContentUnavailable
	}
	if stored := record.data(); stored.channelID != request.ChannelID || !bytes.Equal(stored.groupID, request.GroupID) || !bytes.Equal(stored.ciphertext, request.Ciphertext) {
		return record, false, ErrNativeContentConflict
	}
	return record, false, nil
}

func listInputRejected(channelID uuid.UUID, after int64, limit int) bool {
	if channelID == uuid.Nil || after < 0 {
		return true
	}
	if limit < 1 {
		return true
	}
	return limit > 100
}

// List paginates opaque events without claiming that any group is authorized.
// Channel visibility follows the current single-server text/voice model.
func (s *NativeCiphertextStore) List(ctx context.Context, principal auth.NativePrincipal, channelID uuid.UUID, after int64, limit int) ([]NativeCiphertextRecord, error) {
	if listInputRejected(channelID, after, limit) {
		return nil, ErrNativeContentInput
	}
	var records []NativeCiphertextRecord
	err := s.sessions.WithAuthorizedTransaction(ctx, principal, func(ctx context.Context, tx pgx.Tx, user auth.User) error {
		if err := lockNativeChannel(ctx, tx, nativeChannelLockRead, channelID); err != nil {
			return err
		}
		if err := s.authorize(ctx, user, channelID, perm.ViewChannel); err != nil {
			return err
		}
		var err error
		records, err = queryNativeCiphertext(ctx, tx, channelID, after, limit)
		return err
	})
	if err != nil {
		return nil, err
	}
	return records, nil
}

func queryNativeCiphertext(ctx context.Context, tx pgx.Tx, channelID uuid.UUID, after int64, limit int) ([]NativeCiphertextRecord, error) {
	rows, err := tx.Query(ctx, `SELECT `+nativeCiphertextColumns+` FROM native_ciphertext_events WHERE channel_id=$1 AND number>$2 ORDER BY number LIMIT $3`, channelID, after, limit)
	if err != nil {
		return nil, ErrNativeContentUnavailable
	}
	defer rows.Close()
	records := make([]NativeCiphertextRecord, 0)
	for rows.Next() {
		record, err := scanNativeCiphertext(rows)
		if err != nil {
			return nil, ErrNativeContentUnavailable
		}
		records = append(records, record)
	}
	if rows.Err() != nil {
		return nil, ErrNativeContentUnavailable
	}
	return records, nil
}
