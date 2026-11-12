package chat

import (
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/channels"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/perm/permtest"
)

func TestLayoutAndCategoryChecks(t *testing.T) {
	f := permtest.NewFixture()
	plain, restricted, hidden := f.Category(), f.Category(), f.Category()
	f.RoleOverwrite(false, restricted, f.EveryoneID(), 0, perm.SendMessages)
	f.RoleOverwrite(false, hidden, f.EveryoneID(), 0, perm.ViewChannel)
	inPlain, inRestricted, inHidden := f.Channel(plain), f.Channel(restricted), f.Channel(hidden)
	snap := f.Snapshot()
	member := perm.Subject{ID: uuid.New()}
	owner := perm.Subject{ID: uuid.New(), Owner: true}
	ptr := func(id uuid.UUID) *uuid.UUID { return &id }

	layout := func(chans ...channels.ChannelPlacement) channels.LayoutRequest {
		return channels.LayoutRequest{Channels: chans}
	}
	for _, c := range []struct {
		what string
		req  channels.LayoutRequest
		sub  perm.Subject
		ok   bool
	}{
		{"within a plain category", layout(channels.ChannelPlacement{ID: inPlain, CategoryID: ptr(plain)}), member, true},
		{"unknown channel", layout(channels.ChannelPlacement{ID: uuid.New()}), member, false},
		{"hidden channel", layout(channels.ChannelPlacement{ID: inHidden, CategoryID: ptr(hidden)}), member, false},
		{"into a hidden category", layout(channels.ChannelPlacement{ID: inPlain, CategoryID: ptr(hidden)}), member, true},
		{"hidden category order", channels.LayoutRequest{Categories: []channels.CategoryOrder{{ID: hidden}}}, member, true},
		{"visible category order", channels.LayoutRequest{Categories: []channels.CategoryOrder{{ID: plain}}}, member, true},
		{"out of a restricted category", layout(channels.ChannelPlacement{ID: inRestricted}), member, false},
		{"into a restricted category", layout(channels.ChannelPlacement{ID: inPlain, CategoryID: ptr(restricted)}), member, false},
		{"the Owner moves anything", layout(channels.ChannelPlacement{ID: inRestricted, CategoryID: ptr(plain)}), owner, true},
	} {
		seen, err := channels.SeenLayout(snap, c.sub, c.req)
		if (err == nil) != c.ok {
			t.Errorf("%s: %v", c.what, err)
		}
		// What the caller cannot see never reaches the database under its own id:
		// it is applied as an id nobody has, like a missing one.
		for _, cat := range seen.Categories {
			if cat.ID == hidden && c.sub == member {
				t.Errorf("%s: the hidden category is still named", c.what)
			}
		}
		for _, ch := range seen.Channels {
			if c.sub == member && (ch.ID == inHidden || (ch.CategoryID != nil && *ch.CategoryID == hidden)) {
				t.Errorf("%s: a hidden id is still named", c.what)
			}
		}
	}

	for _, c := range []struct {
		what string
		id   uuid.UUID
		sub  perm.Subject
		ok   bool
	}{
		{"plain category", plain, member, true},
		{"unknown category", uuid.New(), member, true},
		{"hidden category", hidden, member, false},
		{"restricted category, plain member", restricted, member, false},
		{"restricted category, Owner", restricted, owner, true},
	} {
		if err := channels.CheckWholeCategory(snap, c.sub, c.id); (err == nil) != c.ok {
			t.Errorf("%s: %v", c.what, err)
		}
	}
}
