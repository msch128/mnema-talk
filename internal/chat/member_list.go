package chat

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/mnema-xyz/talk/internal/chat/core"
	"github.com/mnema-xyz/talk/internal/db"
	"github.com/mnema-xyz/talk/internal/safego"
)

// memberLoadTimeout bounds one shared read of the member list. It is not
// cancelled with a caller: one caller that gives up must not fail the others.
const memberLoadTimeout = 10 * time.Second

// memberLoads shares the read of the member list between requests that ask
// at the same time. After a server restart every client reads GET /api/members
// again within seconds, and each read is the whole list: without sharing that
// is one full read per member, the same rows every time.
//
// A request never takes a read that began before it arrived: it joins the next
// one, which starts once the read in progress has finished. So every answer is
// at least as fresh as an unshared read would be (a change committed before
// the request is in it), and at most two reads run back to back however many
// requests are waiting.
type memberLoads struct {
	mu      sync.Mutex
	waiting *memberFlight // not started yet: new requests join it
	busy    bool          // a read is running
	load    func(ctx context.Context) ([]core.Member, error)
}

type memberFlight struct {
	done    chan struct{}
	members []core.Member
	err     error
}

// get returns the member list, read after the call began. The slice is
// shared: callers copy before they change anything.
func (l *memberLoads) get(ctx context.Context, p *db.Pool) ([]core.Member, error) {
	l.mu.Lock()
	if l.load == nil {
		l.load = func(ctx context.Context) ([]core.Member, error) { return core.GetAllMembers(ctx, p) }
	}
	f := l.waiting
	if f == nil {
		f = &memberFlight{done: make(chan struct{})}
		l.waiting = f
	}
	start := !l.busy
	l.busy = true
	l.mu.Unlock()
	if start {
		// The read outlives this caller (others share it) but keeps its values.
		base := context.WithoutCancel(ctx)
		safego.Go("member_list", safego.Abandon, func() { l.run(base) })
	}
	select {
	case <-f.done:
		return f.members, f.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// run reads for the waiting flight, then for the one that gathered meanwhile,
// until nobody waits.
func (l *memberLoads) run(base context.Context) {
	for {
		l.mu.Lock()
		f := l.waiting
		l.waiting = nil
		if f == nil {
			l.busy = false
			l.mu.Unlock()
			return
		}
		load := l.load
		l.mu.Unlock()
		l.read(base, f, load)
	}
}

// errMemberLoad answers the requests of a read that panicked; the next
// request reads again.
var errMemberLoad = errors.New("member list read failed")

func (l *memberLoads) read(base context.Context, f *memberFlight, load func(ctx context.Context) ([]core.Member, error)) {
	defer close(f.done)
	ctx, cancel := context.WithTimeout(base, memberLoadTimeout)
	defer cancel()
	safego.Run("member_list_read", func() { f.members, f.err = nil, errMemberLoad }, func() {
		f.members, f.err = load(ctx)
	})
}
