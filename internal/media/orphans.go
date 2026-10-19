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
	sc := &orphanScan{ctx: ctx, pool: p, store: store, remove: remove, res: &Orphans{}}
	cutoff := time.Now().Add(-orphanMinAge)
	for _, prefix := range orphanPrefixes {
		err := store.List(ctx, prefix, func(o ObjectInfo) error {
			if o.LastModified.After(cutoff) {
				return nil
			}
			sc.batch = append(sc.batch, o)
			if len(sc.batch) >= orphanBatch {
				return sc.flush()
			}
			return nil
		})
		if err != nil {
			return sc.res, err
		}
		if err := sc.flush(); err != nil {
			return sc.res, err
		}
	}
	return sc.res, nil
}

// orphanScan collects listed objects in batches and checks each batch
// against the media table.
type orphanScan struct {
	ctx    context.Context
	pool   *db.Pool
	store  Store
	remove bool
	batch  []ObjectInfo
	res    *Orphans
}

// flush resolves the pending batch: counts the unreferenced objects and,
// when removing, deletes them.
func (sc *orphanScan) flush() error {
	if len(sc.batch) == 0 {
		return nil
	}
	known, err := sc.referencedKeys()
	if err != nil {
		return err
	}
	var orphans []string
	for _, o := range sc.batch {
		if !known[o.Key] {
			orphans = append(orphans, o.Key)
			sc.res.Count++
			sc.res.Bytes += o.Size
		}
	}
	sc.batch = sc.batch[:0]
	if !sc.remove || len(orphans) == 0 {
		return nil
	}
	if err := sc.store.DeleteBatch(sc.ctx, orphans); err != nil {
		return fmt.Errorf("delete orphaned objects: %w", err)
	}
	sc.res.Deleted += int64(len(orphans))
	return nil
}

// referencedKeys returns the batch keys that a live media row points at.
func (sc *orphanScan) referencedKeys() (map[string]bool, error) {
	keys := make([]string, len(sc.batch))
	for i, o := range sc.batch {
		keys[i] = o.Key
	}
	rows, err := sc.pool.Query(sc.ctx, `SELECT s3_key FROM media WHERE s3_key = ANY($1) AND NOT is_deleted`, keys)
	if err != nil {
		return nil, fmt.Errorf("look up media keys: %w", err)
	}
	defer rows.Close()
	known := map[string]bool{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		known[k] = true
	}
	return known, rows.Err()
}

// orphanScanFirstRun leaves startup alone; the scan then repeats daily.
const orphanScanFirstRun = 10 * time.Minute

// StartOrphanScan reports orphaned objects once a day in the log. It never
// deletes anything: removal is an explicit admin action.
func StartOrphanScan(ctx context.Context, p *db.Pool, store Store) {
	if store == nil {
		return
	}
	go runPeriodically(ctx, orphanScanFirstRun, retentionInterval, orphanScanTask(p, store))
}

// orphanScanTask reports scan results without coupling the scan to its timer.
func orphanScanTask(p *db.Pool, store Store) func(context.Context) {
	return func(ctx context.Context) {
		o, err := FindOrphans(ctx, p, store, false)
		if err != nil {
			slog.Warn("orphaned media scan failed", "err", err)
			return
		}
		if o.Count > 0 {
			slog.Warn("orphaned media objects found; an admin can remove them with POST /api/admin/media/orphans/cleanup", "count", o.Count, "bytes", o.Bytes)
		}
	}
}
