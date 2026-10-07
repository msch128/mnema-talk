package media

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/msch128/mnema-talk/internal/db"
)

// Orphans are stored objects no media row refers to: left behind when an
// object delete failed after its row was gone, or by a crash between upload
// and insert. Nothing shows or serves them; they only take space.
type Orphans struct {
	Count int64 `json:"count"`
	Bytes int64 `json:"bytes"`
	// Deleted is how many were removed (cleanup only).
	Deleted int64 `json:"deleted"`
}

// orphanPrefixes are the key spaces the app writes. Other objects in a
// shared bucket are never looked at.
var orphanPrefixes = []string{"uploads/", "avatars/"}

// orphanMinAge skips objects younger than this: an upload stores its object
// before its row is committed.
const orphanMinAge = 24 * time.Hour

// orphanBatch is how many keys one database lookup (and one delete) covers.
const orphanBatch = 1000

// FindOrphans lists unreferenced objects older than orphanMinAge. With
// remove it deletes them as well. A media row marked deleted (pruned) no
// longer refers to its object, so a leftover object of it counts too.
func FindOrphans(ctx context.Context, p *db.Pool, store Store, remove bool) (*Orphans, error) {
	res := &Orphans{}
	cutoff := time.Now().Add(-orphanMinAge)
	var batch []ObjectInfo
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		keys := make([]string, len(batch))
		for i, o := range batch {
			keys[i] = o.Key
		}
		rows, err := p.Query(ctx, `SELECT s3_key FROM media WHERE s3_key = ANY($1) AND NOT is_deleted`, keys)
		if err != nil {
			return fmt.Errorf("look up media keys: %w", err)
		}
		known := map[string]bool{}
		for rows.Next() {
			var k string
			if err := rows.Scan(&k); err != nil {
				rows.Close()
				return err
			}
			known[k] = true
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		var orphans []string
		for _, o := range batch {
			if !known[o.Key] {
				orphans = append(orphans, o.Key)
				res.Count++
				res.Bytes += o.Size
			}
		}
		batch = batch[:0]
		if remove && len(orphans) > 0 {
			if err := store.DeleteBatch(ctx, orphans); err != nil {
				return fmt.Errorf("delete orphaned objects: %w", err)
			}
			res.Deleted += int64(len(orphans))
		}
		return nil
	}
	for _, prefix := range orphanPrefixes {
		err := store.List(ctx, prefix, func(o ObjectInfo) error {
			if o.LastModified.After(cutoff) {
				return nil
			}
			batch = append(batch, o)
			if len(batch) >= orphanBatch {
				return flush()
			}
			return nil
		})
		if err != nil {
			return res, err
		}
		if err := flush(); err != nil {
			return res, err
		}
	}
	return res, nil
}

// orphanScanFirstRun leaves startup alone; the scan then repeats daily.
const orphanScanFirstRun = 10 * time.Minute

// StartOrphanScan reports orphaned objects once a day in the log. It never
// deletes anything: removal is an explicit admin action.
func StartOrphanScan(ctx context.Context, p *db.Pool, store Store) {
	if store == nil {
		return
	}
	go runPeriodically(ctx, orphanScanFirstRun, retentionInterval, func(ctx context.Context) {
		o, err := FindOrphans(ctx, p, store, false)
		if err != nil {
			slog.Warn("orphaned media scan failed", "err", err)
			return
		}
		if o.Count > 0 {
			slog.Warn("orphaned media objects found; an admin can remove them with POST /api/admin/media/orphans/cleanup", "count", o.Count, "bytes", o.Bytes)
		}
	})
}
