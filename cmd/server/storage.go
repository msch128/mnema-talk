package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/media"
)

// lazyStore is a media.Store that becomes usable once object storage could be
// initialised. Until then every call fails with 503 and Ready reports why, so
// a storage outage at startup no longer disables uploads until the next
// restart while /api/health claims all is well.
type lazyStore struct {
	mu    sync.RWMutex
	store media.Store
	err   error
}

var errStorageStarting = errors.New("object storage not initialised yet")

// startLazyStore tries init once right away and, if that fails, keeps
// retrying in the background with exponential backoff (minWait doubling up
// to maxWait) until it succeeds or ctx ends.
func startLazyStore(ctx context.Context, init func(context.Context) (media.Store, error), minWait, maxWait time.Duration) *lazyStore {
	l := &lazyStore{err: errStorageStarting}
	if l.try(ctx, init) {
		return l
	}
	go func() {
		wait := minWait
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
			if l.try(ctx, init) {
				slog.Info("object storage available, uploads enabled")
				return
			}
			wait = min(wait*2, maxWait)
		}
	}()
	return l
}

func (l *lazyStore) try(ctx context.Context, init func(context.Context) (media.Store, error)) bool {
	attemptCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	s, err := init(attemptCtx)
	l.mu.Lock()
	defer l.mu.Unlock()
	if err != nil {
		l.err = err
		slog.Warn("object storage unavailable, uploads disabled until it is reachable", "err", err)
		return false
	}
	l.store, l.err = s, nil
	return true
}

// Ready returns nil once the store is usable, otherwise the last init error.
func (l *lazyStore) Ready() error {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.err
}

func (l *lazyStore) get() (media.Store, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.store == nil {
		return nil, httpx.ErrUnavailable("file storage is not available")
	}
	return l.store, nil
}

func (l *lazyStore) Upload(ctx context.Context, key string, body io.Reader, mimeType string, size int64) error {
	s, err := l.get()
	if err != nil {
		return err
	}
	return s.Upload(ctx, key, body, mimeType, size)
}

func (l *lazyStore) GetObject(ctx context.Context, key string) (io.ReadCloser, string, int64, error) {
	s, err := l.get()
	if err != nil {
		return nil, "", 0, err
	}
	return s.GetObject(ctx, key)
}

func (l *lazyStore) GetObjectFrom(ctx context.Context, key string, offset int64) (io.ReadCloser, error) {
	s, err := l.get()
	if err != nil {
		return nil, err
	}
	return s.GetObjectFrom(ctx, key, offset)
}

func (l *lazyStore) Delete(ctx context.Context, key string) error {
	s, err := l.get()
	if err != nil {
		return err
	}
	return s.Delete(ctx, key)
}

func (l *lazyStore) DeleteBatch(ctx context.Context, keys []string) error {
	s, err := l.get()
	if err != nil {
		return err
	}
	return s.DeleteBatch(ctx, keys)
}
