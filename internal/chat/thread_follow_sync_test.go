package chat

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/events"
)

func TestMessageOfReadsValuesAndPointersOnly(t *testing.T) {
	m := Message{ID: uuid.New()}
	if got, ok := messageOf(m); !ok || got.ID != m.ID {
		t.Errorf("value: %v %v", got.ID, ok)
	}
	if got, ok := messageOf(&m); !ok || got.ID != m.ID {
		t.Errorf("pointer: %v %v", got.ID, ok)
	}
	if _, ok := messageOf((*Message)(nil)); ok {
		t.Error("a nil pointer is no message")
	}
	if _, ok := messageOf("text"); ok {
		t.Error("another payload is no message")
	}
}

type countingPublisher struct {
	events.Publisher
	sent int
}

func (c *countingPublisher) SendToUsers([]uuid.UUID, string, any) { c.sent++ }

func TestTheFollowTapIgnoresWhatIsNoThreadReplyAndFailsClosedWithoutPermissions(t *testing.T) {
	pub := &countingPublisher{}
	tap := NewFollowSync(context.Background(), nil, nil, pub)
	root := uuid.New()
	tap(uuid.New(), events.MessageUpdate, Message{ParentID: &root})
	tap(uuid.New(), events.MessageCreate, Message{})
	tap(uuid.New(), events.MessageCreate, "not a message")
	// A thread reply without a permission service tells nobody (and needs no database to know it).
	tap(uuid.New(), events.MessageCreate, &Message{ParentID: &root})
	time.Sleep(50 * time.Millisecond)
	if pub.sent != 0 {
		t.Fatalf("%d events sent", pub.sent)
	}
}
