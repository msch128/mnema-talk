package ws

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	core "github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/perm/permtest"
)

// A public Talk stay stays public when the member's connection resumes while
// the permission state cannot be read: its voice time must still be counted.
func TestResumedStayInAPublicTalkStaysPublicWhenPermissionsAreUnavailable(t *testing.T) {
	f := permtest.NewFixture()
	var failing atomic.Bool
	svc := perm.NewServiceFrom(func(context.Context, time.Time) (*perm.Snapshot, error) {
		if failing.Load() {
			return nil, errors.New("database busy")
		}
		return f.Snapshot(), nil
	})
	h := NewHub(nil, nil, nil, nil)
	h.SetPermissions(svc)
	t.Cleanup(h.Close)
	talk := &core.ChannelInfo{ID: f.Channel(uuid.Nil), Type: core.ChannelTypeVoice}
	c := testClient(h, auth.User{ID: uuid.New()})
	key := voiceKey{c.User.ID, talk.ID}

	if _, ok := h.takeSeat(c, talk, false); !ok {
		t.Fatal("no seat")
	}
	svc.Invalidate()
	failing.Store(true)
	if _, ok := h.takeSeat(c, talk, true); !ok {
		t.Fatal("no seat on resume")
	}
	h.mu.RLock()
	class := h.seatClasses[key]
	h.mu.RUnlock()
	if class != seatPublic {
		t.Fatalf("the resumed stay of a public Talk was classed %d, want public", class)
	}
}

// How a stay counts is decided when the seat is taken: a Talk everyone can see
// counts publicly, a Talk some members cannot see only in the private total,
// the call of a conversation (and a Talk whose permissions cannot be read) not
// at all.
func TestASeatCountsByWhoCanSeeItsTalk(t *testing.T) {
	f := permtest.NewFixture()
	open := f.Channel(uuid.Nil)
	hidden := f.Channel(uuid.Nil)
	f.RoleOverwrite(true, hidden, f.EveryoneID(), 0, perm.ViewChannel)
	dm := f.DM(uuid.New(), uuid.New())
	call := f.DMTalk(dm)
	var failing atomic.Bool
	svc := perm.NewServiceFrom(func(context.Context, time.Time) (*perm.Snapshot, error) {
		if failing.Load() {
			return nil, errors.New("database busy")
		}
		return f.Snapshot(), nil
	})
	h := NewHub(nil, nil, nil, nil)
	h.SetPermissions(svc)
	t.Cleanup(h.Close)

	for name, tc := range map[string]struct {
		ch   uuid.UUID
		want seatClass
	}{"open Talk": {open, seatPublic}, "Talk hidden from @everyone": {hidden, seatLimited}, "call of a conversation": {call, seatPrivate}} {
		if got := h.classifySeat(tc.ch); got != tc.want {
			t.Errorf("%s: class %d, want %d", name, got, tc.want)
		}
	}
	svc.Invalidate()
	failing.Store(true)
	if got := h.classifySeat(open); got != seatPrivate {
		t.Errorf("unreadable permissions: class %d, want private", got)
	}
}
