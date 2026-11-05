package webhook

import (
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/messages"
)

// The embed rules live in package chat (the bot API shares them); these are
// the names this package uses.
type (
	inEmbed   = messages.InEmbed
	formError = messages.FormError
)

const maxEmbeds = messages.MaxEmbeds

func sanitizeEmbeds(in []inEmbed) ([]core.Embed, error) { return messages.SanitizeEmbeds(in) }
