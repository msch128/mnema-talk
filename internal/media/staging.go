package media

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// Staged attachments: the composer uploads each file while the member is
// still writing, then sends the message with the ids. A staged file is a
// media row with staged = TRUE and no message; only its uploader can fetch or
// remove it, and the sweep deletes it when no message claimed it in time.
const (
	// stagedTimeout is how long an unsent file is kept.
	stagedTimeout = 6 * time.Hour
	// stagedSweepInterval is how often the sweep looks for expired files.
	stagedSweepInterval = 15 * time.Minute
	// maxStagedPerMember bounds the unsent files one member holds at once
	// (three full messages), so staging cannot be used as free storage.
	maxStagedPerMember = 3 * chat.MaxAttachments
)

// stageAttachment handles POST /api/channels/{channelID}/attachments.
//
// @Summary Upload an attachment for a message
// @Description multipart/form-data with the file in field 'file'. Stores the file as a staged attachment without posting a message; send the returned id in attachment_ids of POST /api/channels/{channelID}/messages (up to 10 per message). Needs the same permissions as an upload. A staged file is visible only to its uploader and is deleted automatically after six hours if no message claims it. A member holds at most 30 unsent files. Same stricter per-member upload rate limit and size limits as the upload endpoint.
// @ID stageAttachment
// @Tags Media
// @Accept mpfd
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param file formData file true "At most MAX_UPLOAD_MB (config); type is sniffed and checked against an allowlist."
// @Success 201 {object} chat.MediaAttachment "The staged attachment."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): no file, an empty file or too many unsent files."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Send messages or Attach files permission in the channel, or cross-origin request rejected. RULES_NOT_ACCEPTED: the member has not accepted the community rules yet."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 413 {object} httpx.ErrorResponse "PAYLOAD_TOO_LARGE: body or file exceeds the limit (1 MiB for JSON)."
// @Failure 415 {object} httpx.ErrorResponse "UNSUPPORTED_MEDIA_TYPE: the file type is not allowed."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Failure 503 {object} httpx.ErrorResponse "UNAVAILABLE: a dependency (database, file storage) is not available."
// @Router /api/channels/{channelID}/attachments [post]
func (h *Handler) stageAttachment(w http.ResponseWriter, r *http.Request) error {
	user := auth.UserFrom(r.Context())
	chID, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return err
	}
	ctx, err := chat.UploadContext(h.Perms, r, chID)
	if err != nil {
		return err
	}
	if _, err := chat.ChannelForMessages(ctx, h.DB, chID); err != nil {
		return err
	}
	req, err := h.prepareUploadRequest(w, r, user)
	if err != nil {
		return err
	}
	release, err := h.ReserveUpload(req.Context(), user.ID, AnnouncedSize(req, bodyLimit(h.limit(req.Context()))))
	if err != nil {
		return err
	}
	defer release()
	in, _, err := h.readUpload(req, chID)
	if err != nil {
		return err
	}
	if err := h.insertStaged(ctx, chID, user.ID, in); err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, chat.MediaAttachment{
		ID: in.id, OriginalFilename: in.filename, MimeType: in.mime, SizeBytes: in.size, URL: "/api/media/" + in.id.String(),
	})
	return nil
}

// insertStaged records the file, unless the member already holds
// maxStagedPerMember unsent ones. The count and the insert run under a lock
// of the member's, so parallel uploads cannot slip past the cap together.
func (h *Handler) insertStaged(ctx context.Context, chID, userID uuid.UUID, in *storedUpload) error {
	err := pgx.BeginFunc(ctx, h.DB, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "staged:"+userID.String()); err != nil {
			return fmt.Errorf("lock staged uploads: %w", err)
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM media WHERE uploader_id = $1 AND staged`, userID).Scan(&n); err != nil {
			return fmt.Errorf("count staged attachments: %w", err)
		}
		if n >= maxStagedPerMember {
			return httpx.ErrInvalidInput(fmt.Sprintf("too many unsent files (at most %d); send or remove some first", maxStagedPerMember))
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO media (id, uploader_id, channel_id, s3_bucket, s3_key, original_filename, mime_type, size_bytes, staged)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, TRUE)`,
			in.id, userID, chID, h.Bucket, in.key, in.filename, in.mime, in.size)
		return err
	})
	if err != nil {
		// The object must not outlive a failed DB write or a refused file.
		h.discard(ctx, in.key)
		if _, refused := httpx.AsAPIError(err); refused {
			return err
		}
		return fmt.Errorf("record staged upload: %w", err)
	}
	return nil
}

// unstageAttachment handles DELETE /api/channels/{channelID}/attachments/{id}.
//
// @Summary Remove an unsent attachment
// @Description Deletes a staged attachment (one not yet sent in a message) and its file. Only the uploader can. 404 when it does not exist, is not staged any more or belongs to someone else.
// @ID unstageAttachment
// @Tags Media
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param id path string true "Attachment ID." Format(uuid)
// @Success 204 "Removed."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: cross-origin request rejected by the CSRF check."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Failure 503 {object} httpx.ErrorResponse "UNAVAILABLE: a dependency (database, file storage) is not available."
// @Router /api/channels/{channelID}/attachments/{id} [delete]
func (h *Handler) unstageAttachment(w http.ResponseWriter, r *http.Request) error {
	user := auth.UserFrom(r.Context())
	chID, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return err
	}
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	var key string
	err = h.DB.QueryRow(r.Context(), `
		DELETE FROM media WHERE id = $1 AND channel_id = $2 AND uploader_id = $3 AND staged RETURNING s3_key`,
		id, chID, user.ID).Scan(&key)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.ErrNotFound("attachment not found")
	}
	if err != nil {
		return fmt.Errorf("remove staged attachment: %w", err)
	}
	h.discard(r.Context(), key)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// SweepStaged deletes staged files older than olderThan that no message
// claimed, and returns how many it removed. The rows go first: a file whose
// row is gone can no longer be claimed, and a failed object delete leaves
// an orphan the orphan scan reports.
func SweepStaged(ctx context.Context, p *db.Pool, store Store, olderThan time.Duration) (int, error) {
	rows, err := p.Query(ctx, `
		DELETE FROM media WHERE staged AND created_at < NOW() - make_interval(secs => $1)
		RETURNING s3_key`, olderThan.Seconds())
	if err != nil {
		return 0, fmt.Errorf("sweep staged attachments: %w", err)
	}
	keys, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return 0, fmt.Errorf("sweep staged attachments: %w", err)
	}
	if len(keys) == 0 {
		return 0, nil
	}
	if err := store.DeleteBatch(ctx, keys); err != nil {
		return len(keys), fmt.Errorf("delete staged objects: %w", err)
	}
	return len(keys), nil
}

// StartStagedSweep removes unsent staged attachments after stagedTimeout.
func StartStagedSweep(ctx context.Context, p *db.Pool, store Store) {
	if store == nil {
		return
	}
	go runPeriodically(ctx, time.Minute, stagedSweepInterval, stagedSweepTask(p, store))
}

// stagedSweepTask is one sweep, logged, apart from its timer.
func stagedSweepTask(p *db.Pool, store Store) func(context.Context) {
	return func(ctx context.Context) {
		n, err := SweepStaged(ctx, p, store, stagedTimeout)
		if err != nil {
			slog.Warn("staged attachment sweep failed", "err", err)
			return
		}
		if n > 0 {
			slog.Info("removed unsent attachments", "count", n)
		}
	}
}
