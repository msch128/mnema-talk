package webhook

import (
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/chat/messages"
)

// The embed rules live in package chat (the bot API shares them); these are
// the names this package uses.
type (
	inEmbed   = chat.InEmbed
	formError = chat.FormError
)

const maxEmbeds = chat.MaxEmbeds

func sanitizeEmbeds(in []inEmbed) ([]chat.Embed, error) { return messages.SanitizeEmbeds(in) }
