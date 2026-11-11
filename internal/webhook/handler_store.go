package webhook

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/mentions"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/db"
)

// handlerStore is what the handlers need from the database to load a webhook
// and to store its message. It is an interface so that a test can fail one
// step by its name.
type handlerStore interface {
	// webhook loads a webhook (errNotFound when there is none).
	webhook(ctx context.Context, id uuid.UUID) (*record, error)
	// validateThread checks that thread is a thread root of channel.
	validateThread(ctx context.Context, channel uuid.UUID, thread *uuid.UUID) error
	// insertMessage stores a message and runs also in the same transaction.
	insertMessage(ctx context.Context, nm messages.NewMessage, also func(tx pgx.Tx, messageID uuid.UUID) error) (uuid.UUID, error)
	// message reads a stored message back.
	message(ctx context.Context, id uuid.UUID) (*core.Message, error)
}

// pgStore is the handlerStore of the database.
type pgStore struct {
	db     *db.Pool
	online mentions.OnlineSource
}

func (s pgStore) webhook(ctx context.Context, id uuid.UUID) (*record, error) {
	return get(ctx, s.db, id)
}

func (s pgStore) validateThread(ctx context.Context, channel uuid.UUID, thread *uuid.UUID) error {
	return core.ValidateParent(ctx, s.db, channel, thread)
}

func (s pgStore) insertMessage(ctx context.Context, nm messages.NewMessage, also func(tx pgx.Tx, messageID uuid.UUID) error) (uuid.UUID, error) {
	return messages.InsertMessage(ctx, s.db, s.online, nm, also)
}

func (s pgStore) message(ctx context.Context, id uuid.UUID) (*core.Message, error) {
	return messages.GetMessage(ctx, s.db, id)
}

// records is the store the handlers work on: the database unless a test put
// another one in.
func (h *Handler) records() handlerStore {
	if h.recordsOverride != nil {
		return h.recordsOverride
	}
	return pgStore{db: h.DB, online: h.Online}
}
