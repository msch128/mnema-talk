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
	"github.com/msch128/mnema-talk/internal/s3"
	"github.com/msch128/mnema-talk/internal/safego"
)

// lazyStore is a media.Store that becomes usable once object storage could be
// initialised. Until then every call fails with 503 and Ready reports why, so
// a storage outage at startup no longer disables uploads until the next
// restart while /api/health claims all is well.
type lazyStore struct {
	mu    sync.RWMutex
	store media.Store
	err   error

	// probeMu serialises the live check and guards its cached result.
	probeMu  sync.Mutex
	probedAt time.Time
	wait     time.Duration // how long the last probe stays current
	fails    int           // failed probes in a row
	probeErr error         // what is reported: set after probeFailures failures in a row
}

// Reaching storage again after the start is checked at most once per
// probeTTL, and one check may take probeTimeout: a health endpoint that
// anyone can call must not become a stream of requests to object storage.
//
// One failed or slow probe is not reported: the container health check and
// the scripts that wait on /api/health must not change outcome in a short
// storage blip. The outage is reported after probeFailures failed probes in
// a row, the second asked after probeRetry instead of probeTTL, so it shows
// within about 10-15 s; a successful probe reports storage healthy at once.
// One /api/health call runs at most one probe: its worst case is the 2 s
// database ping plus probeTimeout, 4 s, under Docker's 5 s health-check timeout.
const (
	probeTTL      = 10 * time.Second
	probeRetry    = 3 * time.Second
	probeTimeout  = 2 * time.Second
	probeFailures = 2
)

// The live check is on only while the production client can answer it.
var _ prober = (*s3.Client)(nil)

// prober is a store that can say whether its storage is reachable right now
// (*s3.Client does, with one HEAD request on the bucket).
type prober interface {
	Probe(ctx context.Context) error
}

var errStorageStarting = errors.New("object storage not initialised yet")

// startLazyStore returns at once and runs init in the background, so an
// unreachable endpoint cannot delay the listener. A failed attempt is retried
// with exponential backoff (minWait doubling up to maxWait) until it succeeds
// or ctx ends.
func startLazyStore(ctx context.Context, init func(context.Context) (media.Store, error), minWait, maxWait time.Duration) *lazyStore {
	l := &lazyStore{err: errStorageStarting}
	safego.Go("storage_init", safego.Crash, func() { l.retryInit(ctx, init, minWait, maxWait) })
	return l
}

// retryInit runs init until it succeeds or ctx ends, waiting minWait before
// the second attempt and doubling up to maxWait after each further failure.
func (l *lazyStore) retryInit(ctx context.Context, init func(context.Context) (media.Store, error), minWait, maxWait time.Duration) {
	wait := minWait
	for first := true; ; first = false {
		if !first {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
			wait = min(wait*2, maxWait)
		}
		if ctx.Err() != nil {
			return
		}
		if l.try(ctx, init) {
			if !first {
				slog.Info("object storage available, uploads enabled")
			}
			return
		}
	}
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

// Ready returns nil while the store is usable, otherwise why not: the last
// init error before the store was first reached, and afterwards the result of
// a live check (cached for probeTTL), so storage that dies while the server
// runs is reported and so is its return.
func (l *lazyStore) Ready() error {
	l.mu.RLock()
	store, err := l.store, l.err
	l.mu.RUnlock()
	if store == nil {
		return err
	}
	p, ok := store.(prober)
	if !ok {
		return nil
	}
	l.probeMu.Lock()
	defer l.probeMu.Unlock()
	if l.probedAt.IsZero() || time.Since(l.probedAt) >= l.wait {
		ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
		defer cancel()
		l.wait = probeTTL
		if err := p.Probe(ctx); err == nil {
			l.fails, l.probeErr = 0, nil
		} else if l.fails++; l.fails >= probeFailures {
			l.probeErr = err
		} else {
			l.wait = probeRetry
		}
		l.probedAt = time.Now()
	}
	return l.probeErr
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

func (l *lazyStore) List(ctx context.Context, prefix string, fn func(media.ObjectInfo) error) error {
	s, err := l.get()
	if err != nil {
		return err
	}
	return s.List(ctx, prefix, fn)
}
