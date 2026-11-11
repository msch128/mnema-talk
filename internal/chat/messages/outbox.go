package messages

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/events/wire"
)

// RecordMessageEvent stores message_create or message_update for the channel's
// outgoing webhooks in tx, with the message as tx leaves it. Call it last in
// the transaction, so the payload is the one published after the commit.
// nonce is the client's id of a send, echoed in message_create.
func RecordMessageEvent(ctx context.Context, p *db.Pool, tx pgx.Tx, eventType string, channel, id uuid.UUID, nonce *uuid.UUID) error {
	_, err := recordMessageEvent(ctx, p, tx, eventType, channel, id, nonce)
	return err
}

// recordMessageEvent is RecordMessageEvent. It returns the message it loaded
// for the payload, or nil when no webhook receives the event.
func recordMessageEvent(ctx context.Context, p *db.Pool, tx pgx.Tx, eventType string, channel, id uuid.UUID, nonce *uuid.UUID) (*core.Message, error) {
	var msg *core.Message
	err := core.RecordEvent(ctx, p, tx, channel, eventType, func() (events.Event, error) {
		var err error
		if msg, err = GetMessage(ctx, tx, id); err != nil {
			return nil, err
		}
		msg.Nonce = nonce
		if eventType == events.MessageUpdate {
			return (*core.MessageUpdate)(msg), nil
		}
		return (*core.MessageCreate)(msg), nil
	})
	return msg, err
}

// RecordMessageDelete stores message_delete for the channel's outgoing
// webhooks in the transaction that deletes the message.
func RecordMessageDelete(ctx context.Context, p *db.Pool, tx pgx.Tx, channel, id uuid.UUID, parent *uuid.UUID) error {
	return core.RecordEvent(ctx, p, tx, channel, events.MessageDelete, func() (events.Event, error) {
		return wire.MessageDelete{ID: id, ChannelID: channel, ParentID: parent}, nil
	})
}

// recordReactions stores message_reaction with the reactions as tx leaves them.
func recordReactions(ctx context.Context, p *db.Pool, tx pgx.Tx, channel, id uuid.UUID) error {
	return core.RecordEvent(ctx, p, tx, channel, events.MessageReaction, func() (events.Event, error) {
		reactions, err := ReactionSummary(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		return ReactionResult{MessageID: id, ChannelID: channel, Reactions: reactions}, nil
	})
}
