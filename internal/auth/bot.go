package auth

import (
	"context"

	"github.com/google/uuid"
)

// BotInfo describes the bot behind a request that authenticated with a bot
// token (Authorization: Bot ...). The request's user is the bot's account.
type BotInfo struct {
	BotID   uuid.UUID
	TokenID uuid.UUID
	Name    string
	// Intents are the gateway intents the admin allowed the bot.
	Intents int64
}

type botKey struct{}

// WithBot attaches the bot to the context.
func WithBot(ctx context.Context, b *BotInfo) context.Context {
	return context.WithValue(ctx, botKey{}, b)
}

// BotFrom returns the bot of the request, or nil for a member's request.
func BotFrom(ctx context.Context) *BotInfo {
	b, _ := ctx.Value(botKey{}).(*BotInfo)
	return b
}
