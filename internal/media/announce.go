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
// reload. A file of no message (an avatar, a cover) or of a message that is
// gone sends nothing.
func (h *Handler) announceRemoved(ctx context.Context, mediaID uuid.UUID) {
	if h.Events == nil {
		return
	}
	var messageID *uuid.UUID
	if err := h.DB.QueryRow(ctx, `SELECT message_id FROM media WHERE id = $1`, mediaID).Scan(&messageID); err != nil || messageID == nil {
		return
	}
	msg, err := messages.GetMessage(ctx, h.DB, *messageID)
	if err != nil {
		slog.Debug("media: reload message of a removed file", "message", *messageID, "err", err)
		return
	}
	h.Events.BroadcastChannel(msg.ChannelID, (*core.MessageUpdate)(msg))
}
