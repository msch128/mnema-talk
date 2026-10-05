package media

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

type Stats struct {
	TotalFiles     int64 `json:"total_files"`
	TotalSizeBytes int64 `json:"total_size_bytes"`
	DeletedFiles   int64 `json:"deleted_files"`
}

type DashboardItem struct {
	ID               uuid.UUID  `json:"id" format:"uuid"`
	UploaderID       uuid.UUID  `json:"uploader_id" format:"uuid"`
	UploaderName     string     `json:"uploader_name"`
	ChannelID        *uuid.UUID `json:"channel_id" format:"uuid" extensions:"x-nullable"`
	ChannelName      string     `json:"channel_name,omitempty" binding:"optional"`
	OriginalFilename string     `json:"original_filename"`
	MimeType         string     `json:"mime_type"`
	SizeBytes        int64      `json:"size_bytes"`
	URL              string     `json:"url"`
	IsDeleted        bool       `json:"is_deleted"`
	CreatedAt        time.Time  `json:"created_at" format:"date-time"`
}

// GetStats returns storage totals for the admin dashboard.
func GetStats(ctx context.Context, p *db.Pool) (*Stats, error) {
	var s Stats
	err := p.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE NOT is_deleted),
		       COALESCE(SUM(size_bytes) FILTER (WHERE NOT is_deleted), 0),
		       COUNT(*) FILTER (WHERE is_deleted)
		FROM media`).Scan(&s.TotalFiles, &s.TotalSizeBytes, &s.DeletedFiles)
	if err != nil {
		return nil, fmt.Errorf("media stats: %w", err)
	}
	return &s, nil
}

// ListMedia pages through all media, newest first.
func ListMedia(ctx context.Context, p *db.Pool, limit, offset int) ([]DashboardItem, error) {
	rows, err := p.Query(ctx, `
		SELECT m.id, m.uploader_id, u.display_name, m.channel_id,
		       COALESCE(c.name, ''),
		       m.original_filename, m.mime_type, m.size_bytes, m.is_deleted, m.created_at
		FROM media m
		JOIN users u ON u.id = m.uploader_id
		LEFT JOIN channels c ON c.id = m.channel_id
		ORDER BY m.created_at DESC
		LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list media: %w", err)
	}
	defer rows.Close()

	items := make([]DashboardItem, 0)
	for rows.Next() {
		var it DashboardItem
		if err := rows.Scan(&it.ID, &it.UploaderID, &it.UploaderName, &it.ChannelID, &it.ChannelName,
			&it.OriginalFilename, &it.MimeType, &it.SizeBytes, &it.IsDeleted, &it.CreatedAt); err != nil {
			return nil, err
		}
		it.URL = "/api/media/" + it.ID.String()
		items = append(items, it)
	}
	return items, rows.Err()
}

// DeleteMedia removes one object from storage and marks its row deleted, so
// chat history shows a placeholder instead of a broken file.
func DeleteMedia(ctx context.Context, p *db.Pool, store Store, id uuid.UUID) error {
	var key string
	var deleted bool
	err := p.QueryRow(ctx, `SELECT s3_key, is_deleted FROM media WHERE id = $1`, id).Scan(&key, &deleted)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.ErrNotFound("media not found")
	}
	if err != nil {
		return fmt.Errorf("load media: %w", err)
	}
	if deleted {
		return nil
	}
	if err := store.Delete(ctx, key); err != nil {
		return fmt.Errorf("delete object %s: %w", key, err)
	}
	_, err = p.Exec(ctx, `UPDATE media SET is_deleted = TRUE WHERE id = $1`, id)
	return err
}

// PruneOlderThan deletes chat attachments older than days. Avatars are never
// pruned: they are still in use however old they are.
func PruneOlderThan(ctx context.Context, p *db.Pool, store Store, days int) (int, error) {
	if days < 1 {
		return 0, httpx.ErrInvalidInput("days must be at least 1")
	}
	rows, err := p.Query(ctx, `
		SELECT id, s3_key FROM media
		WHERE created_at < NOW() - make_interval(days => $1)
		  AND NOT is_deleted
		  AND message_id IS NOT NULL`, days)
	if err != nil {
		return 0, fmt.Errorf("query prunable media: %w", err)
	}
	var ids []uuid.UUID
	var keys []string
	for rows.Next() {
		var id uuid.UUID
		var key string
		if err := rows.Scan(&id, &key); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
		keys = append(keys, key)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(keys) == 0 {
		return 0, nil
	}
	if err := store.DeleteBatch(ctx, keys); err != nil {
		return 0, fmt.Errorf("delete objects: %w", err)
	}
	if _, err := p.Exec(ctx, `UPDATE media SET is_deleted = TRUE WHERE id = ANY($1)`, ids); err != nil {
		return 0, fmt.Errorf("mark pruned media: %w", err)
	}
	slog.Info("media pruned", "count", len(keys), "older_than_days", days)
	return len(keys), nil
}

// StartRetentionWorker prunes daily when retentionDays > 0. With the default
// of 0 it does nothing at all.
func StartRetentionWorker(ctx context.Context, p *db.Pool, store Store, retentionDays int) {
	if retentionDays <= 0 || store == nil {
		slog.Info("automatic media pruning disabled")
		return
	}
	slog.Info("automatic media pruning enabled", "older_than_days", retentionDays)
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := PruneOlderThan(ctx, p, store, retentionDays); err != nil {
					slog.Error("media pruning failed", "err", err)
				}
			}
		}
	}()
}
