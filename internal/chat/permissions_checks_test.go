package chat

import (
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

func TestLayoutAndCategoryChecks(t *testing.T) {
	f := perm.NewFixture()
	plain, restricted, hidden := f.Category(), f.Category(), f.Category()
	f.RoleOverwrite(false, restricted, f.EveryoneID(), 0, perm.SendMessages)
	f.RoleOverwrite(false, hidden, f.EveryoneID(), 0, perm.ViewChannel)
	inPlain, inRestricted, inHidden := f.Channel(plain), f.Channel(restricted), f.Channel(hidden)
	snap := f.Snapshot()
	member := perm.Subject{ID: uuid.New()}
	owner := perm.Subject{ID: uuid.New(), Owner: true}
	ptr := func(id uuid.UUID) *uuid.UUID { return &id }

	layout := func(chans ...ChannelPlacement) LayoutRequest { return LayoutRequest{Channels: chans} }
	for _, c := range []struct {
		what string
		req  LayoutRequest
		sub  perm.Subject
		ok   bool
	}{
		{"within a plain category", layout(ChannelPlacement{ID: inPlain, CategoryID: ptr(plain)}), member, true},
		{"unknown channel", layout(ChannelPlacement{ID: uuid.New()}), member, false},
		{"hidden channel", layout(ChannelPlacement{ID: inHidden, CategoryID: ptr(hidden)}), member, false},
		{"into a hidden category", layout(ChannelPlacement{ID: inPlain, CategoryID: ptr(hidden)}), member, false},
		{"hidden category order", LayoutRequest{Categories: []CategoryOrder{{ID: hidden}}}, member, false},
		{"visible category order", LayoutRequest{Categories: []CategoryOrder{{ID: plain}}}, member, true},
		{"out of a restricted category", layout(ChannelPlacement{ID: inRestricted}), member, false},
		{"into a restricted category", layout(ChannelPlacement{ID: inPlain, CategoryID: ptr(restricted)}), member, false},
		{"the Owner moves anything", layout(ChannelPlacement{ID: inRestricted, CategoryID: ptr(plain)}), owner, true},
	} {
		if err := checkLayout(snap, c.sub, c.req); (err == nil) != c.ok {
			t.Errorf("%s: %v", c.what, err)
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
		if err := checkWholeCategory(snap, c.sub, c.id); (err == nil) != c.ok {
			t.Errorf("%s: %v", c.what, err)
		}
	}
}
