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
	private := h.privateSeat[key]
	h.mu.RUnlock()
	if private {
		t.Fatal("the resumed stay of a public Talk was marked private")
	}
}
