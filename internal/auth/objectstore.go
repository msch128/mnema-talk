package auth

import (
	"context"
	"io"
)

// ObjectStore is the part of the object store the account flows use (uploads
// to remove after a deletion, the export archive). media.Store satisfies it.
type ObjectStore interface {
	Upload(ctx context.Context, key string, body io.Reader, mimeType string, size int64) error
	GetObjectFrom(ctx context.Context, key string, offset int64) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	DeleteBatch(ctx context.Context, keys []string) error
}
