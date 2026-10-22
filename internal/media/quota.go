package media

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// quotaWindow is the rolling window of the per-user upload byte budget.
const quotaWindow = 24 * time.Hour

// Quota bounds how much storage uploads may consume. A zero field disables
// that check.
type Quota struct {
	// PerUserBytes is what one account (member, bot or webhook) may upload in
	// any rolling 24 hours, counted from the media table, deleted files
	// included, so deleting and re-uploading does not reset it.
	PerUserBytes int64
	// TotalBytes refuses uploads once the files still stored reach this size.
	TotalBytes int64
	// MinFreeBytes refuses uploads while the filesystem at DiskPath has less
	// than this free. It is only meaningful when the app can see the volume
	// that holds the objects; DiskPath empty disables it.
	MinFreeBytes int64
	DiskPath     string
}

// CheckUploadQuota refuses an upload of up to size bytes by userID when it
// would exceed the per-user budget, the total storage cap or the free-space
// floor. size is the announced request size (an upper bound for the file);
// the streamed file is still capped by the upload limit.
func (h *Handler) CheckUploadQuota(ctx context.Context, userID uuid.UUID, size int64) error {
	size = max(size, 0)
	q := h.Quota
	if err := q.checkFreeSpace(size); err != nil {
		return err
	}
	if q.PerUserBytes > 0 {
		var used int64
		if err := h.DB.QueryRow(ctx, `
			SELECT COALESCE(SUM(size_bytes), 0)::bigint FROM media
			WHERE uploader_id = $1 AND created_at > $2`,
			userID, time.Now().Add(-quotaWindow)).Scan(&used); err != nil {
			return fmt.Errorf("read upload usage: %w", err)
		}
		if used+size > q.PerUserBytes {
			return httpx.NewAPIError(http.StatusTooManyRequests, httpx.CodeRateLimited, "daily upload limit reached; try again later")
		}
	}
	if q.TotalBytes > 0 {
		var total int64
		if err := h.DB.QueryRow(ctx, `SELECT COALESCE(SUM(size_bytes), 0)::bigint FROM media WHERE NOT is_deleted`).Scan(&total); err != nil {
			return fmt.Errorf("read storage usage: %w", err)
		}
		if total+size > q.TotalBytes {
			return httpx.ErrUnavailable("file storage is full; uploads are paused")
		}
	}
	return nil
}

func (q Quota) checkFreeSpace(size int64) error {
	if q.MinFreeBytes <= 0 || q.DiskPath == "" {
		return nil
	}
	free, err := freeDiskBytes(q.DiskPath)
	if err != nil {
		slog.Warn("could not read free disk space", "err", err)
		return nil
	}
	if free < uint64(q.MinFreeBytes) || free-uint64(q.MinFreeBytes) < uint64(size) {
		return httpx.ErrUnavailable("file storage is almost full; uploads are paused")
	}
	return nil
}
