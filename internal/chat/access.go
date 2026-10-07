package chat

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// ChannelInfo is what request handling needs to know about a channel. All
// channels belong to the one community and are open to every member.
type ChannelInfo struct {
	ID   uuid.UUID
	Type ChannelType
	// UserLimit caps a voice room's members at once (0 = no limit).
	UserLimit int
}

var errChannelNotFound = httpx.ErrNotFound("channel not found")

// LoadChannel fetches a channel's type; a missing channel is a 404.
func LoadChannel(ctx context.Context, p *db.Pool, id uuid.UUID) (*ChannelInfo, error) {
	ch := &ChannelInfo{ID: id}
	err := p.QueryRow(ctx, `SELECT type, user_limit FROM channels WHERE id = $1`, id).Scan(&ch.Type, &ch.UserLimit)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errChannelNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load channel: %w", err)
	}
	return ch, nil
}

// messageRef is the minimal context of an existing message.
type messageRef struct {
	ChannelID uuid.UUID
	AuthorID  uuid.UUID
	ParentID  *uuid.UUID
}

var errMessageNotFound = httpx.ErrNotFound("message not found")

func loadMessageRef(ctx context.Context, p *db.Pool, messageID uuid.UUID) (*messageRef, error) {
	var ref messageRef
	err := p.QueryRow(ctx, `SELECT channel_id, user_id, parent_id FROM messages WHERE id = $1`, messageID).
		Scan(&ref.ChannelID, &ref.AuthorID, &ref.ParentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errMessageNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load message: %w", err)
	}
	return &ref, nil
}

// ValidateParent checks that parentID is a root message of the same channel, so
// thread replies cannot be attached across channels or nested.
func ValidateParent(ctx context.Context, p *db.Pool, channelID uuid.UUID, parentID *uuid.UUID) error {
	if parentID == nil {
		return nil
	}
	ref, err := loadMessageRef(ctx, p, *parentID)
	if err != nil {
		return httpx.ErrInvalidInput("parent message not found")
	}
	if ref.ChannelID != channelID || ref.ParentID != nil {
		return httpx.ErrInvalidInput("parent must be a root message in the same channel")
	}
	return nil
}
