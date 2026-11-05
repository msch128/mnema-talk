package core

import (
	"context"
	"log/slog"
)

// ObjectDeleter removes stored media objects (implemented by media.Store).
type ObjectDeleter interface {
	DeleteBatch(ctx context.Context, keys []string) error
}

// DeleteObjects removes media objects after their rows are gone. A failure
// only leaves an orphaned object behind, so it is logged, not returned. With
// no store (objs nil) or no keys it does nothing.
func DeleteObjects(ctx context.Context, objs ObjectDeleter, keys []string) {
	if objs == nil || len(keys) == 0 {
		return
	}
	if err := objs.DeleteBatch(ctx, keys); err != nil {
		slog.Warn("delete media objects", "count", len(keys), "err", err)
	}
}
