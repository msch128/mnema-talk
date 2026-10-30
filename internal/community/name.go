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
	icon   string
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
	n.refresh(ctx)
	return n.shown()
}

// Icon returns the version of the community icon the Owner set, or "" while
// there is none. It changes with every upload, so it serves as a cache key.
func (n *Names) Icon(ctx context.Context) string {
	if n == nil {
		return ""
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.refresh(ctx)
	return n.icon
}

// refresh reloads the settings when the cached ones are older than ttl; the
// caller holds the lock.
func (n *Names) refresh(ctx context.Context) {
	if !n.loaded.IsZero() && time.Since(n.loaded) < ttl {
		return
	}
	if n.q == nil {
		n.name, n.icon, n.loaded = "", "", time.Now()
		return
	}
	// A failed read keeps the last answer and is retried on the next call.
	var name, icon string
	if err := n.q.QueryRow(ctx, `SELECT community_name, COALESCE(community_icon_version, '') FROM instance_settings`).Scan(&name, &icon); err == nil {
		n.name, n.icon, n.loaded = strings.TrimSpace(name), icon, time.Now()
	}
}

// Forget drops the cached name and icon; call it after the setting changed.
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
