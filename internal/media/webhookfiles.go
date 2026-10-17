package media

import (
	"context"
	"mime/multipart"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// StoredFile is an attachment already written to object storage whose media
// row is not recorded yet.
type StoredFile = storedUpload

// StoreFilePart sniffs and streams one multipart file part to object storage
// with the same limits and type checks as a member's upload. The caller
// records it with RecordFile, or drops it with DiscardFile.
func (h *Handler) StoreFilePart(ctx context.Context, part *multipart.Part) (*StoredFile, error) {
	if h.Store == nil {
		return nil, httpx.ErrUnavailable("file storage is not configured")
	}
	return h.storePart(ctx, part)
}

// DiscardFile deletes a stored file that will not be used.
func (h *Handler) DiscardFile(ctx context.Context, f *StoredFile) { h.discard(ctx, f.key) }

// RecordFile attaches a stored file to a message inside tx.
func (h *Handler) RecordFile(ctx context.Context, tx pgx.Tx, f *StoredFile, uploader, channel, message uuid.UUID) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO media (id, uploader_id, channel_id, message_id, s3_bucket, s3_key, original_filename, mime_type, size_bytes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		f.id, uploader, channel, message, h.Bucket, f.key, f.filename, f.mime, f.size)
	return err
}

// FormError maps a failure reading a request body to the error to answer with.
func (h *Handler) FormError(err error) error { return h.formError(err) }
