package communityevents

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/metrics"
)

type panickingPusher struct{}

func (panickingPusher) SendEventSignupNotice(context.Context, uuid.UUID, uuid.UUID, string, string, string, time.Time, *uuid.UUID) error {
	panic("push failed")
}

// TestPanicsInNoticeJobsAreCountedAndReleaseTheWaitGroup fires the panics the
// event_signup_notice and event_comment_notice sites abandon and checks what
// they leave: the notice is lost (by design: a notice is best effort), the
// panic is counted, and the wait group shutdown waits on is released.
func TestPanicsInNoticeJobsAreCountedAndReleaseTheWaitGroup(t *testing.T) {
	h := &Handler{} // a nil Store makes the comment job panic on its first query
	signup := metrics.GoroutinePanics.With("event_signup_notice").Load()
	comment := metrics.GoroutinePanics.With("event_comment_notice").Load()
	ev := &CommunityEvent{ID: uuid.New(), Title: "Raid night"}

	h.pushNotice(context.Background(), panickingPusher{}, ev, uuid.New(), "signup", "")
	h.notifyCommentAsync(context.Background(), ev, EventComment{ID: uuid.New(), EventID: ev.ID, Content: "x"})

	done := make(chan struct{})
	go func() { h.notices.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the notice wait group was not released after the panics")
	}
	if got := metrics.GoroutinePanics.With("event_signup_notice").Load() - signup; got != 1 {
		t.Errorf("sign-up notice panics counted: %d, want 1", got)
	}
	if got := metrics.GoroutinePanics.With("event_comment_notice").Load() - comment; got != 1 {
		t.Errorf("comment notice panics counted: %d, want 1", got)
	}
}
