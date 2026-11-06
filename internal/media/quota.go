package media

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// quotaWindow is the rolling window of the per-user upload byte budget.
const quotaWindow = 24 * time.Hour

// Quota bounds how much storage uploads may consume. A zero field disables
// that check.
type Quota struct {
	// PerUserBytes is what one account (member, bot or webhook) may upload in
	// any rolling 24 hours, counted from upload_usage, which keeps a row
	// per upload for the window, so deleting a file or its message and
	// re-uploading does not reset it.
	PerUserBytes int64
	// TotalBytes refuses uploads once the files still stored reach this size.
	TotalBytes int64
	// MinFreeBytes refuses uploads while the filesystem at DiskPath has less
	// than this free. It is only meaningful when the app can see the volume
	// that holds the objects; DiskPath empty disables it.
	MinFreeBytes int64
	DiskPath     string
}

// reservations are the bytes of uploads in flight. The app is one process, so
// a mutex makes check-and-reserve atomic: parallel uploads of one account
// cannot all pass the same budget. The rolling window is read from upload_usage
// (rows are written when an upload completes), in-flight uploads are
// added on top, and a reservation is released only after the row is written
// (or the upload failed), so nothing is ever counted less than once.
type reservations struct {
	mu    sync.Mutex
	user  map[uuid.UUID]int64
	total int64
}

// ReserveUpload checks an upload of up to size bytes by userID against the
// budgets and reserves the bytes until the returned release is called. size
// is an upper bound: the request body is capped at it (an unknown length must
// be passed as the largest body the route accepts).
func (h *Handler) ReserveUpload(ctx context.Context, userID uuid.UUID, size int64) (func(), error) {
	size = max(size, 0)
	q := h.Quota
	if err := q.checkFreeSpace(size); err != nil {
		return nil, err
	}
	if q.PerUserBytes <= 0 && q.TotalBytes <= 0 {
		return func() {}, nil
	}
	r := &h.reserved
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := h.checkBudgets(ctx, userID, size); err != nil {
		return nil, err
	}
	if r.user == nil {
		r.user = map[uuid.UUID]int64{}
	}
	r.user[userID] += size
	r.total += size
	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			if r.user[userID] -= size; r.user[userID] <= 0 {
				delete(r.user, userID)
			}
			r.total -= size
		})
	}, nil
}

// checkBudgets runs with h.reserved.mu held.
func (h *Handler) checkBudgets(ctx context.Context, userID uuid.UUID, size int64) error {
	q, r := h.Quota, &h.reserved
	if q.PerUserBytes > 0 {
		var used int64
		if err := h.DB.QueryRow(ctx, `
			SELECT COALESCE(SUM(bytes), 0)::bigint FROM upload_usage
			WHERE user_id = $1 AND created_at > $2`,
			userID, time.Now().Add(-quotaWindow)).Scan(&used); err != nil {
			return fmt.Errorf("read upload usage: %w", err)
		}
		if used+r.user[userID]+size > q.PerUserBytes {
			return httpx.NewAPIError(http.StatusTooManyRequests, httpx.CodeRateLimited, "daily upload limit reached; try again later")
		}
	}
	if q.TotalBytes > 0 {
		var total int64
		if err := h.DB.QueryRow(ctx, `SELECT COALESCE(SUM(size_bytes), 0)::bigint FROM media WHERE NOT is_deleted`).Scan(&total); err != nil {
			return fmt.Errorf("read storage usage: %w", err)
		}
		if total+r.total+size > q.TotalBytes {
			return httpx.ErrUnavailable("file storage is full; uploads are paused")
		}
	}
	return nil
}

// AnnouncedSize is the size to reserve for a request: its Content-Length, or
// the largest body the route accepts when the length is not known.
func AnnouncedSize(r *http.Request, worstCase int64) int64 {
	if r.ContentLength < 0 {
		return worstCase
	}
	return r.ContentLength
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

// RecordUploadUsage books an upload against the account's daily budget, in the
// transaction that writes its media row. The row outlives the file and its
// message: deleting either must not refund the budget.
func RecordUploadUsage(ctx context.Context, tx pgx.Tx, userID uuid.UUID, size int64) error {
	if _, err := tx.Exec(ctx, `INSERT INTO upload_usage (user_id, bytes) VALUES ($1, $2)`, userID, size); err != nil {
		return fmt.Errorf("record upload usage: %w", err)
	}
	return nil
}

// pruneUploadUsage drops usage rows older than the budget window.
func pruneUploadUsage(ctx context.Context, p *db.Pool) error {
	if _, err := p.Exec(ctx, `DELETE FROM upload_usage WHERE created_at < $1`, time.Now().Add(-quotaWindow)); err != nil {
		return fmt.Errorf("prune upload usage: %w", err)
	}
	return nil
}
