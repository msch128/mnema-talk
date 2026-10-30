package chat

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/httpx"
)

const (
	// MaxAttachments is how many files one message carries.
	MaxAttachments = 10
	// MaxAttachmentBytes is how much one message may carry in total. A single
	// file larger than this (the operator allows it) still goes alone.
	MaxAttachmentBytes int64 = 100 << 20
)

// validateAttachmentIDs checks the staged-attachment ids of a new message:
// at most MaxAttachments, no repeats.
func validateAttachmentIDs(ids []uuid.UUID) error {
	if len(ids) > MaxAttachments {
		return httpx.ErrInvalidInput(fmt.Sprintf("a message carries at most %d attachments", MaxAttachments))
	}
	seen := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			return httpx.ErrInvalidInput("attachment_ids contains a duplicate")
		}
		seen[id] = true
	}
	return nil
}

// claimAttachments attaches the staged files ids, in that order, to a new
// message inside its transaction. A file counts only if its caller uploaded
// it to this channel and nobody has sent it yet; otherwise the message is
// refused and nothing is attached. The files may weigh at most
// MaxAttachmentBytes together (or the largest single one).
func claimAttachments(ctx context.Context, tx pgx.Tx, messageID, userID, channelID uuid.UUID, ids []uuid.UUID) error {
	rows, err := tx.Query(ctx, `
		UPDATE media m SET message_id = $1, staged = FALSE, position = a.pos
		FROM unnest($2::uuid[]) WITH ORDINALITY AS a(id, pos)
		WHERE m.id = a.id AND m.staged AND NOT m.is_deleted AND m.uploader_id = $3 AND m.channel_id = $4
		RETURNING m.size_bytes`, messageID, ids, userID, channelID)
	if err != nil {
		return fmt.Errorf("attach files: %w", err)
	}
	sizes, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return fmt.Errorf("attach files: %w", err)
	}
	if len(sizes) != len(ids) {
		return httpx.ErrInvalidInput("an attachment is unknown, already sent or has expired; upload it again")
	}
	var total, largest int64
	for _, s := range sizes {
		total += s
		largest = max(largest, s)
	}
	if total > max(MaxAttachmentBytes, largest) {
		return httpx.ErrPayloadTooLarge(fmt.Sprintf("a message carries at most %d MB of attachments", MaxAttachmentBytes>>20))
	}
	return nil
}
