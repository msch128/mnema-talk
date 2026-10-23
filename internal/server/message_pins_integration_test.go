//go:build integration

package server

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
)

func (f *messageModerationFixture) pin(c *client, id uuid.UUID) response {
	return c.do(http.MethodPut, f.path("/messages/"+id.String()+"/pin"), nil, nil)
}

func (f *messageModerationFixture) pinned(t *testing.T, c *client) []chat.Message {
	t.Helper()
	res := c.get(f.path("/pins"))
	if res.status != http.StatusOK {
		t.Fatalf("pins: %d %s", res.status, res.body)
	}
	var list []chat.Message
	res.decode(t, &list)
	return list
}

func TestPinnedListShowsTheNewestPinFirstAndLeavesOutUnpinned(t *testing.T) {
	f := newMessageModerationFixture(t, false)
	first := f.a.send(f.user, f.channel, "first")
	second := f.a.send(f.user, f.channel, "second")
	f.a.send(f.user, f.channel, "never pinned")
	for _, id := range []uuid.UUID{first, second} {
		if r := f.pin(f.mod, id); r.status != http.StatusOK {
			t.Fatalf("pin: %d %s", r.status, r.body)
		}
	}
	list := f.pinned(t, f.peer)
	if len(list) != 2 || list[0].ID != second || list[1].ID != first || !list[0].IsPinned {
		t.Fatalf("pinned list %+v", list)
	}
	if r := f.mod.do(http.MethodDelete, f.path("/messages/"+second.String()+"/pin"), nil, nil); r.status != http.StatusOK {
		t.Fatalf("unpin: %d", r.status)
	}
	if list := f.pinned(t, f.peer); len(list) != 1 || list[0].ID != first {
		t.Errorf("after unpin %+v", list)
	}
}

func TestDeletingAPinnedMessageUnpinsIt(t *testing.T) {
	f := newMessageModerationFixture(t, false)
	id := f.a.send(f.user, f.channel, "short lived")
	f.pin(f.mod, id)
	if r := f.user.delete(f.path("/messages/" + id.String())); r.status != http.StatusNoContent {
		t.Fatalf("delete: %d", r.status)
	}
	if list := f.pinned(t, f.peer); len(list) != 0 {
		t.Errorf("deleted message still listed: %+v", list)
	}
}

func TestAChannelKeepsAtMostFiftyPins(t *testing.T) {
	f := newMessageModerationFixture(t, false)
	var ids []uuid.UUID
	for i := 0; i < chat.MaxPinsPerChannel+1; i++ {
		ids = append(ids, f.a.send(f.user, f.channel, "note"))
	}
	for _, id := range ids[:chat.MaxPinsPerChannel] {
		if r := f.pin(f.mod, id); r.status != http.StatusOK {
			t.Fatalf("pin: %d %s", r.status, r.body)
		}
	}
	if r := f.pin(f.mod, ids[chat.MaxPinsPerChannel]); r.status != http.StatusConflict {
		t.Fatalf("51st pin: %d %s", r.status, r.body)
	}
	// Pinning a message that is already pinned is not a new pin.
	if r := f.pin(f.mod, ids[0]); r.status != http.StatusOK {
		t.Errorf("repeat pin: %d", r.status)
	}
	f.mod.do(http.MethodDelete, f.path("/messages/"+ids[0].String()+"/pin"), nil, nil)
	if r := f.pin(f.mod, ids[chat.MaxPinsPerChannel]); r.status != http.StatusOK {
		t.Errorf("pin after a slot freed: %d", r.status)
	}
}

func TestPinsChangedIsAnnouncedOnceToTheChannel(t *testing.T) {
	f := newMessageModerationFixture(t, false)
	id := f.a.send(f.user, f.channel, "announce me")
	f.pin(f.mod, id)
	f.pin(f.mod, id)
	var got []chat.PinsChanged
	for _, e := range f.a.events.Snapshot() {
		if e.Type != events.PinsChanged {
			continue
		}
		if e.ChannelID != f.channel {
			t.Errorf("event not limited to the channel: %+v", e)
		}
		got = append(got, e.Payload.(chat.PinsChanged))
	}
	if len(got) != 1 || got[0].MessageID != id || !got[0].Pinned || got[0].ActorID != f.mod.user.ID {
		t.Errorf("events %+v", got)
	}
}

func TestPinnedListFollowsChannelVisibility(t *testing.T) {
	f := newMessageModerationFixture(t, false)
	id := f.a.send(f.user, f.channel, "secret note")
	f.pin(f.mod, id)
	everyone := everyoneID(t, f.admin)
	if r := putOverwrites(f.admin, "channels", f.channel, overwrite("role", everyone, 0, perm.ViewChannel)); r.status != http.StatusNoContent {
		t.Fatalf("hide channel: %d %s", r.status, r.body)
	}
	if r := f.peer.get(f.path("/pins")); r.status != http.StatusNotFound {
		t.Errorf("hidden channel pins: %d %s", r.status, r.body)
	}
	if r := f.pin(f.peer, id); r.status == http.StatusOK {
		t.Errorf("outsider pinned: %d", r.status)
	}
}
