package media

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
)

// Store is the object storage the media package needs. *s3.Client satisfies
// it in production; MemoryStore backs the tests.
type Store interface {
	Upload(ctx context.Context, key string, body io.Reader, mimeType string, size int64) error
	GetObject(ctx context.Context, key string) (io.ReadCloser, string, int64, error)
	Delete(ctx context.Context, key string) error
	DeleteBatch(ctx context.Context, keys []string) error
}

// ErrObjectNotFound is returned by MemoryStore for unknown keys.
var ErrObjectNotFound = errors.New("object not found")

// MemoryStore is an in-memory Store for tests.
type MemoryStore struct {
	mu      sync.Mutex
	objects map[string]memObject
}

type memObject struct {
	data []byte
	mime string
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{objects: map[string]memObject{}} }

func (m *MemoryStore) Upload(_ context.Context, key string, body io.Reader, mimeType string, _ int64) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = memObject{data: data, mime: mimeType}
	return nil
}

func (m *MemoryStore) GetObject(_ context.Context, key string) (io.ReadCloser, string, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.objects[key]
	if !ok {
		return nil, "", 0, ErrObjectNotFound
	}
	return io.NopCloser(bytes.NewReader(o.data)), o.mime, int64(len(o.data)), nil
}

func (m *MemoryStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, key)
	return nil
}

func (m *MemoryStore) DeleteBatch(ctx context.Context, keys []string) error {
	for _, k := range keys {
		_ = m.Delete(ctx, k)
	}
	return nil
}

// Len reports how many objects are stored.
func (m *MemoryStore) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.objects)
}
