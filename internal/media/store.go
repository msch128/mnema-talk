package media

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
)

// Store is the object storage the media package needs. *s3.Client satisfies
// it in production; MemoryStore backs the tests.
type Store interface {
	// Upload stores body; size < 0 means its length is not known up front.
	Upload(ctx context.Context, key string, body io.Reader, mimeType string, size int64) error
	// GetObjectFrom streams the object starting at byte offset (for Range requests).
	GetObjectFrom(ctx context.Context, key string, offset int64) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	DeleteBatch(ctx context.Context, keys []string) error
	// List calls fn for every object whose key starts with prefix.
	List(ctx context.Context, prefix string, fn func(ObjectInfo) error) error
}

// ObjectInfo describes a stored object.
type ObjectInfo struct {
	Key          string
	Size         int64
	LastModified time.Time
}

// ErrObjectNotFound is returned by MemoryStore for unknown keys.
var ErrObjectNotFound = errors.New("object not found")

// MemoryStore is an in-memory Store for tests.
type MemoryStore struct {
	mu      sync.Mutex
	objects map[string]memObject
}

type memObject struct {
	data     []byte
	mime     string
	modified time.Time
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{objects: map[string]memObject{}} }

func (m *MemoryStore) Upload(_ context.Context, key string, body io.Reader, mimeType string, _ int64) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = memObject{data: data, mime: mimeType, modified: time.Now()}
	return nil
}

func (m *MemoryStore) GetObjectFrom(_ context.Context, key string, offset int64) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.objects[key]
	if !ok {
		return nil, ErrObjectNotFound
	}
	if offset > int64(len(o.data)) {
		offset = int64(len(o.data))
	}
	return io.NopCloser(bytes.NewReader(o.data[offset:])), nil
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

func (m *MemoryStore) List(_ context.Context, prefix string, fn func(ObjectInfo) error) error {
	m.mu.Lock()
	infos := make([]ObjectInfo, 0, len(m.objects))
	for k, o := range m.objects {
		if strings.HasPrefix(k, prefix) {
			infos = append(infos, ObjectInfo{Key: k, Size: int64(len(o.data)), LastModified: o.modified})
		}
	}
	m.mu.Unlock()
	sort.Slice(infos, func(i, j int) bool { return infos[i].Key < infos[j].Key })
	for _, info := range infos {
		if err := fn(info); err != nil {
			return err
		}
	}
	return nil
}

// Put stores an object directly, as if uploaded at modified (tests only).
func (m *MemoryStore) Put(key string, data []byte, modified time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = memObject{data: data, modified: modified}
}

// Has reports whether key is stored.
func (m *MemoryStore) Has(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.objects[key]
	return ok
}
