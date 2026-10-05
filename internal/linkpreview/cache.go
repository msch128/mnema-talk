package linkpreview

import (
	"container/list"
	"sync"
	"time"
)

// flightGroup collapses concurrent calls with the same key into one: the
// first caller runs fn, the others wait for and share its result. A tiny
// stand-in for golang.org/x/sync/singleflight, which is not a dependency.
type flightGroup[T any] struct {
	mu    sync.Mutex
	calls map[string]*flight[T]
}

type flight[T any] struct {
	done chan struct{}
	val  T
	err  error
}

func (g *flightGroup[T]) do(key string, fn func() (T, error)) (T, error) {
	g.mu.Lock()
	if g.calls == nil {
		g.calls = map[string]*flight[T]{}
	}
	if c, ok := g.calls[key]; ok {
		g.mu.Unlock()
		<-c.done
		return c.val, c.err
	}
	c := &flight[T]{done: make(chan struct{})}
	g.calls[key] = c
	g.mu.Unlock()

	defer func() {
		g.mu.Lock()
		delete(g.calls, key)
		g.mu.Unlock()
		close(c.done)
	}()
	c.val, c.err = fn()
	return c.val, c.err
}

// imageCache is a byte-bounded LRU of proxied preview images with a TTL, so
// a card shown to many members is fetched from the origin once.
type imageCache struct {
	mu       sync.Mutex
	maxBytes int
	ttl      time.Duration
	now      func() time.Time
	bytes    int
	order    *list.List // front = most recently used
	items    map[string]*list.Element
}

type cachedImage struct {
	key     string
	body    []byte
	mime    string
	expires time.Time
}

func newImageCache(maxBytes int, ttl time.Duration) *imageCache {
	return &imageCache{maxBytes: maxBytes, ttl: ttl, now: time.Now, order: list.New(), items: map[string]*list.Element{}}
}

func (c *imageCache) get(key string) ([]byte, string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return nil, "", false
	}
	img := el.Value.(*cachedImage)
	if c.now().After(img.expires) {
		c.removeLocked(el)
		return nil, "", false
	}
	c.order.MoveToFront(el)
	return img.body, img.mime, true
}

func (c *imageCache) put(key string, body []byte, mime string) {
	if len(body) > c.maxBytes {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.removeLocked(el)
	}
	el := c.order.PushFront(&cachedImage{key: key, body: body, mime: mime, expires: c.now().Add(c.ttl)})
	c.items[key] = el
	c.bytes += len(body)
	for c.bytes > c.maxBytes {
		c.removeLocked(c.order.Back())
	}
}

func (c *imageCache) removeLocked(el *list.Element) {
	img := c.order.Remove(el).(*cachedImage)
	delete(c.items, img.key)
	c.bytes -= len(img.body)
}
