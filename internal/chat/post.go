package chat

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// Posting a message is shared by plain messages and uploads (media package).

// TextChannel loads chID and refuses voice channels, which have no text chat.
func TextChannel(ctx context.Context, p *db.Pool, chID uuid.UUID) (*ChannelInfo, error) {
	ch, err := LoadChannel(ctx, p, chID)
	if err != nil {
		return nil, err
	}
	if ch.Type == ChannelTypeVoice {
		return nil, httpx.ErrInvalidInput("voice channels have no text chat")
	}
	return ch, nil
}

// ValidateTarget checks a new message's thread parent and quoted reply target.
func ValidateTarget(ctx context.Context, p *db.Pool, chID uuid.UUID, parentID, replyToID *uuid.UUID) error {
	if err := ValidateParent(ctx, p, chID, parentID); err != nil {
		return err
	}
	return ValidateReplyTarget(ctx, p, chID, parentID, replyToID)
}

// InsertMessage stores a validated message and its mentions in one
// transaction. also, when set, runs in the same transaction (e.g. to record
// an attachment).
func InsertMessage(ctx context.Context, p *db.Pool, online OnlineSource, nm NewMessage, also func(tx pgx.Tx, messageID uuid.UUID) error) (uuid.UUID, error) {
	var id uuid.UUID
	err := pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		var err error
		if id, err = CreateMessage(ctx, tx, nm); err != nil {
			return err
		}
		if err := RecordMentions(ctx, tx, online, id, nm.UserID, nm.Content); err != nil {
			return err
		}
		if also != nil {
			return also(tx, id)
		}
		return nil
	})
	if err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

// PublishMessage loads a stored message, broadcasts message_create and
// writes it as the 201 response.
func PublishMessage(w http.ResponseWriter, r *http.Request, p *db.Pool, pub events.Publisher, id uuid.UUID) error {
	msg, err := GetMessage(r.Context(), p, id)
	if err != nil {
		return err
	}
	pub.Broadcast("message_create", msg)
	httpx.WriteJSON(w, http.StatusCreated, msg)
	return nil
}

// queryIDs parses the named optional UUID query parameters into their
// destinations and returns how many were given.
func queryIDs(r *http.Request, what string, dst map[string]**uuid.UUID) (int, error) {
	n := 0
	for name, d := range dst {
		raw := r.URL.Query().Get(name)
		if raw == "" {
			continue
		}
		id, err := uuid.Parse(raw)
		if err != nil {
			return 0, httpx.ErrInvalidInput(name + " must be " + what)
		}
		*d = &id
		n++
	}
	return n, nil
}

// firstID returns the first non-nil id.
func firstID(ids ...*uuid.UUID) *uuid.UUID {
	for _, id := range ids {
		if id != nil {
			return id
		}
	}
	return nil
}
