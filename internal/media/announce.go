package media

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/messages"
)

// announceRemoved sends message_update for the message a removed file was
// attached to, so open clients show "File removed" at once instead of after a
// reload. A file of no message (an avatar, a cover), of a message that is
// gone, or of one whose lifetime has ended sends nothing: an ended message is
// never shown again.
func (h *Handler) announceRemoved(ctx context.Context, mediaID uuid.UUID) {
	// MessageSelect reads only live messages: an ended one is left out.
	msgs, err := messages.QueryMessages(ctx, h.DB, messages.MessageSelect+` WHERE m.id = (SELECT message_id FROM media WHERE id = $1)`, mediaID)
	if err != nil {
		slog.Warn("media: reload the message of a removed file", "media", mediaID, "err", err)
		return
	}
	for i := range msgs {
		h.Events.BroadcastChannel(msgs[i].ChannelID, (*core.MessageUpdate)(&msgs[i]))
	}
}
