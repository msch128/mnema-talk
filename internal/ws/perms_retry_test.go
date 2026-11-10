package ws

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/perm/permtest"
)

// A change whose permission state cannot be loaded right away must not leave
// the member who lost access in the Talk: they are told at once and removed
// as soon as the state can be read again.
func TestMemberWhoLostConnectIsRemovedAfterAFailedStateLoad(t *testing.T) {
	f := permtest.NewFixture()
	var failing atomic.Bool
	svc := perm.NewServiceFrom(func(context.Context, time.Time) (*perm.Snapshot, error) {
		if failing.Load() {
			failing.Store(false) // fails once
			return nil, errors.New("database busy")
		}
		return f.Snapshot(), nil
	})
	h := NewHub(nil, nil, nil, nil, svc)
	h.permRetry.base = 10 * time.Millisecond
	t.Cleanup(h.Close)
	talk := &core.ChannelInfo{ID: f.Channel(uuid.Nil), Type: core.ChannelTypeVoice}
	member := testClient(h, auth.User{ID: uuid.New()})
	h.joinVoice(member, talk)
	if !inRoom(h, talk.ID, member.User.ID) {
		t.Fatal("the member could not join")
	}
	drainTypes(t, member)

	f.RoleOverwrite(true, talk.ID, f.EveryoneID(), 0, perm.Connect)
	svc.Invalidate()
	failing.Store(true)
	h.PermissionsChanged([]uuid.UUID{member.User.ID})

	deadline := time.Now().Add(3 * time.Second)
	for inRoom(h, talk.ID, member.User.ID) {
		if time.Now().After(deadline) {
			t.Fatal("the member kept their seat after the retry")
		}
		time.Sleep(5 * time.Millisecond)
	}
	var got []string
	for end := time.Now().Add(time.Second); time.Now().Before(end) && count(got, "voice_kicked") == 0; time.Sleep(5 * time.Millisecond) {
		got = append(got, drainTypes(t, member)...)
	}
	if count(got, "permissions_changed") != 1 || count(got, "voice_kicked") != 1 {
		t.Fatalf("the member's events: %v", got)
	}
}

// Retrying for a long outage doubles the wait up to the cap and stays there;
// it never wraps around into a negative wait, which would retry in a tight loop.
func TestPermRetryWaitStaysWithinTheCapAfterManyFailures(t *testing.T) {
	r := permRetry{base: time.Second}
	var last time.Duration
	for i := range 70 {
		wait := r.nextWait()
		if wait <= 0 || wait > permRetryMax {
			t.Fatalf("failure %d: wait %v, want within (0, %v]", i+1, wait, permRetryMax)
		}
		if wait < last {
			t.Fatalf("failure %d: wait %v shrank from %v", i+1, wait, last)
		}
		last = wait
	}
	if last != permRetryMax {
		t.Fatalf("wait after 70 failures = %v, want the cap %v", last, permRetryMax)
	}
}
