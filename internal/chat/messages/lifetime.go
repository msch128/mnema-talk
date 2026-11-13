package messages

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// Messages with a lifetime (doc/timed-messages.md): the door checks the
// lifetime against the community's settings, wakes the sweeper of its
// database, and DeleteExpired removes an ended message the way its author's
// delete would.

// errTimedForumPost refuses a lifetime on the root of a forum post: a post is
// meant to last, and its replies may still have one.
var errTimedForumPost = httpx.ErrInvalidInput("a forum post cannot have a lifetime")

// gateLifetime checks a message's lifetime in the insert's transaction, so a
// change of the settings that commits first is the one that holds.
func gateLifetime(ctx context.Context, tx pgx.Tx, kind string, nm NewMessage) error {
	if nm.LifetimeSeconds == 0 {
		return nil
	}
	if err := core.CheckLifetimeBounds(nm.LifetimeSeconds); err != nil {
		return err
	}
	if kind == string(core.ChannelTypeForum) && nm.ParentID == nil {
		return errTimedForumPost
	}
	s, err := core.LoadMessageSettings(ctx, tx)
	if err != nil {
		return err
	}
	return s.CheckLifetime(nm.LifetimeSeconds)
}

// LifetimeOf turns an optional lifetime_seconds of a request into
// NewMessage.LifetimeSeconds: 0 for none. A value that is given is checked as
// given, so 0 or less is refused by the bounds and never read as "none".
func LifetimeOf(seconds *int) int {
	if seconds == nil {
		return 0
	}
	if *seconds <= 0 {
		return -1
	}
	return *seconds
}

// expiryWakers maps a database to the function that wakes its sweeper.
var expiryWakers sync.Map

// OnExpiry registers wake as the sweeper of p: every message with a lifetime
// that p stores calls it with the message's end, after the commit.
func OnExpiry(p *db.Pool, wake func(at time.Time)) {
	expiryWakers.Store(p, wake)
}

func wakeExpiry(p *db.Pool, at time.Time) {
	if wake, ok := expiryWakers.Load(p); ok {
		wake.(func(time.Time))(at)
	}
}

// A message with a lifetime is never pinned (lifetimes are set at sending
// only, so a pinned message never gets one) and never forwarded (the forward
// would outlive the original).
var (
	errTimedPin     = httpx.ErrInvalidInput("a message with a lifetime cannot be pinned")
	errTimedForward = httpx.ErrInvalidInput("a message with a lifetime cannot be forwarded")
)
