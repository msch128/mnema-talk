package ws

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/perm/permtest"
)

// openWorld is the permission state of the hub tests that are not about
// permissions: every channel made by openChannel exists, has no overwrites and
// is open to @everyone with the default permissions. Tests about permissions
// build their own permtest.Fixture.
type openWorld struct {
	mu  sync.Mutex
	f   *permtest.Fixture
	svc *perm.Service
}

var open = newOpenWorld()

func newOpenWorld() *openWorld {
	w := &openWorld{f: permtest.NewFixture()}
	// Every load gets its own copy, so a hub goroutine never reads a snapshot
	// that a later openChannel is changing.
	w.svc = perm.NewServiceFrom(func(context.Context, time.Time) (*perm.Snapshot, error) {
		w.mu.Lock()
		defer w.mu.Unlock()
		return w.f.Fork().Snapshot(), nil
	})
	return w
}

// openPerms is the permission service of the open world.
func openPerms() *perm.Service { return open.svc }

// openChannel adds a channel everybody may use to the open world.
func openChannel() uuid.UUID {
	open.mu.Lock()
	id := open.f.Channel(uuid.Nil)
	open.mu.Unlock()
	open.svc.Invalidate()
	return id
}

// openSnapshot is the current state of the open world.
func openSnapshot() *perm.Snapshot {
	snap, _ := open.svc.Snapshot(context.Background())
	return snap
}
