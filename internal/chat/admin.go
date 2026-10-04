package chat

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/s3"
)

type MediaStats struct {
	TotalFiles     int64 `json:"total_files"`
	TotalSizeBytes int64 `json:"total_size_bytes"`
	PrunedFiles    int64 `json:"pruned_files"`
}

type MediaDashboardItem struct {
	ID               uuid.UUID  `json:"id"`
	UploaderID       uuid.UUID  `json:"uploader_id"`
	UploaderName     string     `json:"uploader_name"`
	ChannelID        *uuid.UUID `json:"channel_id"`
	ChannelName      string     `json:"channel_name,omitempty"`
	OriginalFilename string     `json:"original_filename"`
	MimeType         string     `json:"mime_type"`
	SizeBytes        int64      `json:"size_bytes"`
	URL              string     `json:"url"`
	IsDeleted        bool       `json:"is_deleted"`
	CreatedAt        time.Time  `json:"created_at"`
}

// GetMediaStats returns storage metrics for Herzog's admin dashboard
func GetMediaStats(ctx context.Context, p *db.Pool) (*MediaStats, error) {
	var stats MediaStats

	query := `
		SELECT 
			COALESCE(COUNT(*), 0),
			COALESCE(SUM(CASE WHEN is_deleted = FALSE THEN size_bytes ELSE 0 END), 0),
			COALESCE(COUNT(*) FILTER (WHERE is_deleted = TRUE), 0)
		FROM media
	`

	err := p.QueryRow(ctx, query).Scan(&stats.TotalFiles, &stats.TotalSizeBytes, &stats.PrunedFiles)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate media stats: %w", err)
	}

	return &stats, nil
}

// ListMediaForDashboard returns a paginated list of media files with uploader and channel info
func ListMediaForDashboard(ctx context.Context, p *db.Pool, limit, offset int) ([]MediaDashboardItem, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	query := `
		SELECT 
			m.id, m.uploader_id, u.display_name, m.channel_id, COALESCE(c.name, ''),
			m.original_filename, m.mime_type, m.size_bytes, m.is_deleted, m.created_at
		FROM media m
		JOIN users u ON m.uploader_id = u.id
		LEFT JOIN channels c ON m.channel_id = c.id
		ORDER BY m.created_at DESC
		LIMIT $1 OFFSET $2
	`

	rows, err := p.Query(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query media: %w", err)
	}
	defer rows.Close()

	var items []MediaDashboardItem
	for rows.Next() {
		var item MediaDashboardItem
		if err := rows.Scan(
			&item.ID, &item.UploaderID, &item.UploaderName, &item.ChannelID, &item.ChannelName,
			&item.OriginalFilename, &item.MimeType, &item.SizeBytes, &item.IsDeleted, &item.CreatedAt,
		); err != nil {
			return nil, err
		}
		item.URL = fmt.Sprintf("/api/media/%s", item.ID.String())
		items = append(items, item)
	}

	return items, nil
}

// DeleteSingleMedia immediately deletes a file from S3 and marks it as deleted in DB
func DeleteSingleMedia(ctx context.Context, p *db.Pool, s3Cli *s3.Client, mediaID uuid.UUID) error {
	var s3Key string
	err := p.QueryRow(ctx, `SELECT s3_key FROM media WHERE id = $1`, mediaID).Scan(&s3Key)
	if err != nil {
		return fmt.Errorf("media record not found: %w", err)
	}

	// Delete from S3
	if err := s3Cli.Delete(ctx, s3Key); err != nil {
		log.Printf("[Admin] Warning: failed to delete S3 object %s: %v\n", s3Key, err)
	}

	// Update record in PostgreSQL
	_, err = p.Exec(ctx, `UPDATE media SET is_deleted = TRUE WHERE id = $1`, mediaID)
	return err
}

// PruneMediaOlderThan deletes all media older than X days from S3 and marks them pruned
func PruneMediaOlderThan(ctx context.Context, p *db.Pool, s3Cli *s3.Client, days int) (int, error) {
	if days <= 0 {
		return 0, fmt.Errorf("retention days must be greater than 0")
	}

	cutoff := time.Now().AddDate(0, 0, -days)

	// Fetch all candidate S3 keys
	rows, err := p.Query(ctx, `SELECT id, s3_key FROM media WHERE created_at < $1 AND is_deleted = FALSE`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("failed to query media for pruning: %w", err)
	}
	defer rows.Close()

	var ids []uuid.UUID
	var keys []string

	for rows.Next() {
		var id uuid.UUID
		var key string
		if err := rows.Scan(&id, &key); err == nil {
			ids = append(ids, id)
			keys = append(keys, key)
		}
	}

	if len(keys) == 0 {
		return 0, nil
	}

	// Batch delete from S3
	if err := s3Cli.DeleteBatch(ctx, keys); err != nil {
		log.Printf("[Prune] Warning: S3 batch deletion had errors: %v\n", err)
	}

	// Mark as deleted in PostgreSQL
	_, err = p.Exec(ctx, `UPDATE media SET is_deleted = TRUE WHERE id = ANY($1)`, ids)
	if err != nil {
		return 0, fmt.Errorf("failed to update media retention status: %w", err)
	}

	log.Printf("[Prune] Successfully pruned %d media files older than %d days\n", len(keys), days)
	return len(keys), nil
}

// StartRetentionWorker runs automated daily media cleanup at 03:00 UTC
func StartRetentionWorker(ctx context.Context, p *db.Pool, s3Cli *s3.Client, retentionDays int) {
	if retentionDays <= 0 {
		log.Println("[Retention] Auto-pruning is disabled (retention days <= 0)")
		return
	}

	log.Printf("[Retention] Auto-pruning active: media older than %d days will be pruned daily\n", retentionDays)
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _ = PruneMediaOlderThan(ctx, p, s3Cli, retentionDays)
			}
		}
	}()
}
