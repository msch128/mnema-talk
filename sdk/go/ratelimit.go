package mnema

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"time"
)

// The server's limits per bot (doc/bots.md, REST API): 50 requests per second
// overall, 5 messages per 5 seconds per channel for posts and uploads.
const (
	globalLimit  = 50
	globalWindow = time.Second
	postLimit    = 5
	postWindow   = 5 * time.Second
)

// window allows at most limit uses in any span of every.
type window struct {
	limit  int
	every  time.Duration
	stamps []time.Time
}

func (w *window) delay(now time.Time) time.Duration {
	cut := 0
	for cut < len(w.stamps) && !w.stamps[cut].After(now.Add(-w.every)) {
		cut++
	}
	w.stamps = w.stamps[cut:]
	if len(w.stamps) < w.limit {
		return 0
	}
	return w.stamps[len(w.stamps)-w.limit].Add(w.every).Sub(now)
}

func (w *window) take(now time.Time) { w.stamps = append(w.stamps, now) }

// bucket is one route's state: an optional local window and the pause a 429
// imposed.
type bucket struct {
	win         *window
	pausedUntil time.Time
}

// limiter keeps the client within the server's limits before it is told so,
// and honours the pauses of 429 answers.
type limiter struct {
	mu          sync.Mutex
	now         func() time.Time
	global      window
	globalPause time.Time
	buckets     map[string]*bucket
	post        window // template for the per-channel message windows
}

func newLimiter() *limiter {
	return &limiter{
		now:     time.Now,
		global:  window{limit: globalLimit, every: globalWindow},
		buckets: map[string]*bucket{},
		post:    window{limit: postLimit, every: postWindow},
	}
}

func (l *limiter) bucket(key string) *bucket {
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{}
		if isMessagePost(key) {
			b.win = &window{limit: l.post.limit, every: l.post.every}
		}
		l.buckets[key] = b
	}
	return b
}

// acquire waits until the global limit and the route's bucket allow one more
// request, and takes it.
func (l *limiter) acquire(ctx context.Context, key string) error {
	for {
		d := l.tryTake(key)
		if d <= 0 {
			return nil
		}
		if err := sleep(ctx, d); err != nil {
			return err
		}
	}
}

func (l *limiter) tryTake(key string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b := l.bucket(key)
	d := max(l.globalPause.Sub(now), b.pausedUntil.Sub(now), l.global.delay(now))
	if b.win != nil {
		d = max(d, b.win.delay(now))
	}
	if d > 0 {
		return d
	}
	l.global.take(now)
	if b.win != nil {
		b.win.take(now)
	}
	return 0
}

// pause holds back the bucket (or every request, when global) for d.
func (l *limiter) pause(key string, global bool, d time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	until := l.now().Add(d)
	if global {
		if until.After(l.globalPause) {
			l.globalPause = until
		}
		return
	}
	if b := l.bucket(key); until.After(b.pausedUntil) {
		b.pausedUntil = until
	}
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// bucketKey names the bucket of a route: method and path with ids replaced,
// except a channel id, which keeps channels apart (Discord's "major
// parameter"). Posting a message and uploading a file share the per-channel
// message bucket; all reactions of a message share one bucket.
func bucketKey(method, path string) string {
	path, _, _ = strings.Cut(path, "?")
	parts := strings.Split(path, "/")
	if method == "POST" && len(parts) == 4 && parts[1] == "channels" && (parts[3] == "messages" || parts[3] == "upload") {
		return "post-message:" + parts[2]
	}
	if len(parts) > 1 && parts[1] == "interactions" {
		return method + " /interactions/:id/:token/" + strings.Join(parts[min(4, len(parts)):], "/")
	}
	for i, p := range parts {
		if p == "reactions" {
			parts = parts[:i+1]
			break
		}
		if uuidPattern.MatchString(p) && (i != 2 || parts[1] != "channels") {
			parts[i] = ":id"
		}
	}
	return method + " " + strings.Join(parts, "/")
}

func isMessagePost(key string) bool { return strings.HasPrefix(key, "post-message:") }

// sleep waits for d or until ctx ends.
func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
