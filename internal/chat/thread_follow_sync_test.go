package chat

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/perm/permtest"
)

type countingPublisher struct {
	events.Publisher
	sent int
}

func (c *countingPublisher) BroadcastPerm(perm.Set, events.Event)  {}
func (c *countingPublisher) SendToUsers([]uuid.UUID, events.Event) { c.sent++ }

func TestTheFollowTapNeedsAPermissionService(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("built a follow tap without a permission service")
		}
	}()
	messages.NewFollowSync(context.Background(), nil, nil, &countingPublisher{})
}

func TestTheFollowTapIgnoresWhatIsNoThreadReplyAndFailsClosedWithoutPermissions(t *testing.T) {
	pub := &countingPublisher{}
	tap := messages.NewFollowSync(context.Background(), nil, permtest.Unloadable(), pub)
	root := uuid.New()
	tap(uuid.New(), (*core.MessageUpdate)(&core.Message{ParentID: &root}))
	tap(uuid.New(), (*core.MessageCreate)(&core.Message{}))
	tap(uuid.New(), (*core.MessageCreate)(nil))
	// A thread reply whose permission state cannot be read tells nobody (and needs no database to know it).
	tap(uuid.New(), (*core.MessageCreate)(&core.Message{ParentID: &root}))
	time.Sleep(50 * time.Millisecond)
	if pub.sent != 0 {
		t.Fatalf("%d events sent", pub.sent)
	}
}
