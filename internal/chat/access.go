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
	// SlowMode is the least number of seconds between two messages of one
	// member (0 = off).
	SlowMode int
	// IsLobby marks a "Create a Talk" lobby (see talks.go).
	IsLobby bool
	// IsStage marks a Stage (see stage.go).
	IsStage bool
}

var errChannelNotFound = httpx.ErrNotFound("channel not found")

// LoadChannel fetches a channel's type; a missing channel is a 404.
func LoadChannel(ctx context.Context, p db.Querier, id uuid.UUID) (*ChannelInfo, error) {
	ch := &ChannelInfo{ID: id}
	err := p.QueryRow(ctx, `SELECT type, user_limit, slow_mode_seconds, is_lobby, is_stage FROM channels WHERE id = $1`, id).Scan(&ch.Type, &ch.UserLimit, &ch.SlowMode, &ch.IsLobby, &ch.IsStage)
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

// conversationReadableSQL is the condition "message m is in a conversation
// that the member user (a SQL parameter or expression) belongs to". A
// conversation is readable by its members only, whatever the permission state
// says, so every read of conversation messages (search, saved list) uses this
// one rule.
func conversationReadableSQL(user string) string {
	return "m.in_dm AND EXISTS (SELECT 1 FROM dm_members dm WHERE dm.channel_id = m.channel_id AND dm.user_id = " + user + ")"
}

// errCursorGone answers a page request whose cursor row has been deleted since
// the previous page: its position is unknown, so the list must be reloaded.
var errCursorGone = httpx.ErrNotFound("the page cursor no longer exists; reload the list")

// cursorExists checks that the row a page cursor points at still exists
// (query selects it). Without that check a keyset comparison against a deleted
// row is NULL and the page comes back empty with has_more=false, as if the
// list had ended.
func cursorExists(ctx context.Context, q db.Querier, query string, args ...any) error {
	var ok bool
	if err := q.QueryRow(ctx, "SELECT EXISTS ("+query+")", args...).Scan(&ok); err != nil {
		return fmt.Errorf("check page cursor: %w", err)
	}
	if !ok {
		return errCursorGone
	}
	return nil
}

// messageCursorExists is cursorExists for a cursor that is a message id (nil:
// the first page).
func messageCursorExists(ctx context.Context, q db.Querier, before *uuid.UUID) error {
	if before == nil {
		return nil
	}
	return cursorExists(ctx, q, `SELECT 1 FROM messages WHERE id = $1`, *before)
}

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

// loadReferencedMessage loads the message a request refers to. A missing one
// is the caller's mistake (400 with notFound); any other failure is ours.
func loadReferencedMessage(ctx context.Context, p *db.Pool, id uuid.UUID, notFound string) (*messageRef, error) {
	ref, err := loadMessageRef(ctx, p, id)
	if errors.Is(err, errMessageNotFound) {
		return nil, httpx.ErrInvalidInput(notFound)
	}
	return ref, err
}

// ValidateParent checks that parentID is a root message of the same channel, so
// thread replies cannot be attached across channels or nested.
func ValidateParent(ctx context.Context, p *db.Pool, channelID uuid.UUID, parentID *uuid.UUID) error {
	if parentID == nil {
		return nil
	}
	ref, err := loadReferencedMessage(ctx, p, *parentID, "parent message not found")
	if err != nil {
		return err
	}
	if ref.ChannelID != channelID || ref.ParentID != nil {
		return httpx.ErrInvalidInput("parent must be a root message in the same channel")
	}
	return nil
}
