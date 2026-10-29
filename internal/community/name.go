// Package community knows what the instance calls itself. The Owner sets the
// name in the onboarding settings (instance_settings.community_name); until
// then the host of PUBLIC_URL stands in, and a server without a usable
// address is called by the product name.
package community

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/msch128/mnema-talk/internal/db"
)

// Product is the name of last resort.
const Product = "Mnema Talk"

// ttl bounds how long a name read from the database is reused when nothing
// told the cache that it changed (a restore, a manual SQL update).
const ttl = time.Minute

// Names serves the community name from a short-lived cache. A nil *Names
// reports the product name, so components built without one keep working.
type Names struct {
	q        db.Querier
	fallback string

	mu     sync.Mutex
	name   string
	loaded time.Time
}

// New returns a Names reading from q. publicURL supplies the host used while
// no name is set.
func New(q db.Querier, publicURL string) *Names {
	return &Names{q: q, fallback: Fallback(publicURL)}
}

// Fallback is the host of publicURL, or the product name when there is none.
func Fallback(publicURL string) string {
	if u, err := url.Parse(strings.TrimSpace(publicURL)); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return Product
}

// Get returns the community name, never empty.
func (n *Names) Get(ctx context.Context) string {
	if n == nil {
		return Product
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.loaded.IsZero() && time.Since(n.loaded) < ttl {
		return n.shown()
	}
	var name string
	if n.q == nil {
		n.name, n.loaded = "", time.Now()
		return n.shown()
	}
	// A failed read keeps the last answer and is retried on the next call.
	if err := n.q.QueryRow(ctx, `SELECT community_name FROM instance_settings`).Scan(&name); err == nil {
		n.name, n.loaded = strings.TrimSpace(name), time.Now()
	}
	return n.shown()
}

// Forget drops the cached name; call it after the setting changed.
func (n *Names) Forget() {
	if n == nil {
		return
	}
	n.mu.Lock()
	n.loaded = time.Time{}
	n.mu.Unlock()
}

func (n *Names) shown() string {
	if n.name != "" {
		return n.name
	}
	return n.fallback
}
