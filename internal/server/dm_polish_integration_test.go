//go:build integration

package server

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

type polishDM struct {
	ID                uuid.UUID `json:"id"`
	Hidden            bool      `json:"hidden"`
	FavouritePosition *int      `json:"favourite_position"`
	NotifyLevel       string    `json:"notify_level"`
	UnreadCount       int       `json:"unread_count"`
	MentionCount      int       `json:"mention_count"`
	LastMessage       *struct {
		UserID        uuid.UUID `json:"user_id"`
		Text          string    `json:"text"`
		HasAttachment bool      `json:"has_attachment"`
		HasPoll       bool      `json:"has_poll"`
	} `json:"last_message"`
}

func (c *client) polishList(t *testing.T) map[uuid.UUID]polishDM {
	t.Helper()
	var list []polishDM
	c.get("/api/dms").decode(t, &list)
	out := map[uuid.UUID]polishDM{}
	for _, d := range list {
		out[d.ID] = d
	}
	return out
}

func (c *client) setHidden(dm uuid.UUID, hidden bool) response {
	return c.put("/api/dms/"+dm.String()+"/hidden", map[string]any{"hidden": hidden})
}

func (c *client) setFavourites(ids ...uuid.UUID) response {
	if ids == nil {
		ids = []uuid.UUID{}
	}
	return c.put("/api/dms/favourites", map[string]any{"ids": ids})
}

func TestAClosedConversationReturnsWithTheNextMessageAndOnlyForItsOwner(t *testing.T) {
	f := newDMFixture(t, false)
	dm := f.alice.mustDM(t, f.bob)
	wantStatus(t, "first", f.alice.dmPost(dm.ID, "hello"), http.StatusCreated)

	wantStatus(t, "alice closes", f.alice.setHidden(dm.ID, true), http.StatusNoContent)
	if !f.alice.polishList(t)[dm.ID].Hidden || f.bob.polishList(t)[dm.ID].Hidden {
		t.Fatal("closing is the closer's own view")
	}
	// Nothing is deleted: the conversation and its history are still there.
	wantStatus(t, "history", f.alice.get("/api/channels/"+dm.ID.String()+"/messages"), http.StatusOK)

	wantStatus(t, "bob answers", f.bob.dmPost(dm.ID, "hello again"), http.StatusCreated)
	got := f.alice.polishList(t)[dm.ID]
	if got.Hidden || got.LastMessage == nil || got.LastMessage.Text != "hello again" || got.LastMessage.UserID != f.bob.user.ID {
		t.Fatalf("after a new message: %+v", got)
	}

	// Closing and reopening by hand; strangers cannot touch it.
	wantStatus(t, "close again", f.alice.setHidden(dm.ID, true), http.StatusNoContent)
	wantStatus(t, "reopen", f.alice.setHidden(dm.ID, false), http.StatusNoContent)
	wantStatus(t, "outsider", f.carol.setHidden(dm.ID, true), http.StatusNotFound)
	wantStatus(t, "owner is an outsider too", f.admin.setHidden(dm.ID, true), http.StatusNotFound)
	wantStatus(t, "unknown", f.alice.setHidden(uuid.New(), true), http.StatusNotFound)
	wantStatus(t, "signed out", f.a.anon().put("/api/dms/"+dm.ID.String()+"/hidden", map[string]any{"hidden": true}), http.StatusUnauthorized)
}

func TestAMessageDoesNotReopenAConversationForSomeoneWhoBlockedTheSender(t *testing.T) {
	f := newDMFixture(t, false)
	dm := f.alice.mustDM(t, f.bob, f.carol)
	wantStatus(t, "carol closes", f.carol.setHidden(dm.ID, true), http.StatusNoContent)
	wantStatus(t, "carol blocks bob", f.carol.put("/api/me/blocks/"+f.bob.user.ID.String(), nil), http.StatusNoContent)
	wantStatus(t, "bob writes", f.bob.dmPost(dm.ID, "you cannot see this"), http.StatusCreated)
	if !f.carol.polishList(t)[dm.ID].Hidden {
		t.Fatal("a message carol never sees reopened the conversation")
	}
	wantStatus(t, "alice writes", f.alice.dmPost(dm.ID, "hi all"), http.StatusCreated)
	got := f.carol.polishList(t)[dm.ID]
	if got.Hidden || got.LastMessage == nil || got.LastMessage.Text != "hi all" {
		t.Fatalf("alice's message: %+v", got)
	}
}

func TestFavouritesAreOrderedPersonalAndLimited(t *testing.T) {
	f := newDMFixture(t, false)
	ab := f.alice.mustDM(t, f.bob)
	ac := f.alice.mustDM(t, f.carol)
	ad := f.alice.mustDM(t, f.dave)
	bc := f.bob.mustDM(t, f.carol)

	wantStatus(t, "favourite", f.alice.setFavourites(ac.ID, ab.ID), http.StatusNoContent)
	list := f.alice.polishList(t)
	if p := list[ac.ID].FavouritePosition; p == nil || *p != 0 {
		t.Fatalf("first: %+v", list[ac.ID])
	}
	if p := list[ab.ID].FavouritePosition; p == nil || *p != 1 {
		t.Fatalf("second: %+v", list[ab.ID])
	}
	if list[ad.ID].FavouritePosition != nil {
		t.Fatal("ad is not a favourite")
	}
	if f.bob.polishList(t)[ab.ID].FavouritePosition != nil {
		t.Fatal("favourites are personal")
	}

	// Reordering and dropping replace the whole list.
	wantStatus(t, "reorder", f.alice.setFavourites(ab.ID), http.StatusNoContent)
	list = f.alice.polishList(t)
	if p := list[ab.ID].FavouritePosition; p == nil || *p != 0 || list[ac.ID].FavouritePosition != nil {
		t.Fatalf("after dropping: %+v %+v", list[ab.ID], list[ac.ID])
	}

	// Somebody else's conversation, a made-up one and a repeat are refused and change nothing.
	wantStatus(t, "not a member", f.alice.setFavourites(ab.ID, bc.ID), http.StatusNotFound)
	wantStatus(t, "unknown", f.alice.setFavourites(uuid.New()), http.StatusNotFound)
	wantStatus(t, "repeat", f.alice.setFavourites(ab.ID, ab.ID), http.StatusBadRequest)
	wantStatus(t, "signed out", f.a.anon().put("/api/dms/favourites", map[string]any{"ids": []uuid.UUID{}}), http.StatusUnauthorized)
	if p := f.alice.polishList(t)[ab.ID].FavouritePosition; p == nil || *p != 0 {
		t.Fatal("a refused change altered the favourites")
	}
	if f.bob.polishList(t)[bc.ID].FavouritePosition != nil {
		t.Fatal("alice reached bob's conversation")
	}

	// A favourite stays in the list; favouriting reopens a closed one.
	wantStatus(t, "cannot close a favourite", f.alice.setHidden(ab.ID, true), http.StatusConflict)
	wantStatus(t, "close ad", f.alice.setHidden(ad.ID, true), http.StatusNoContent)
	wantStatus(t, "favourite ad", f.alice.setFavourites(ab.ID, ad.ID), http.StatusNoContent)
	if f.alice.polishList(t)[ad.ID].Hidden {
		t.Fatal("a favourite is shown")
	}
}

func TestAtMostTwentyFiveFavourites(t *testing.T) {
	f := newDMFixture(t, false)
	ids := make([]uuid.UUID, 26)
	for i := range ids {
		ids[i] = uuid.New()
	}
	wantStatus(t, "too many", f.alice.setFavourites(ids...), http.StatusBadRequest)
}

func TestMuteAllDirectMessagesKeepsUnreadButDropsTheHighlightUnlessAConversationOverrides(t *testing.T) {
	f := newDMFixture(t, false)
	dm := f.alice.mustDM(t, f.bob)
	other := f.carol.mustDM(t, f.bob)
	var s struct {
		MuteAll       bool `json:"mute_all"`
		ListCollapsed bool `json:"list_collapsed"`
	}
	f.bob.get("/api/users/me/dm-settings").decode(t, &s)
	if s.MuteAll || s.ListCollapsed {
		t.Fatalf("defaults: %+v", s)
	}
	wantStatus(t, "bob mutes all", f.bob.put("/api/users/me/dm-settings", map[string]any{"mute_all": true, "list_collapsed": true}), http.StatusOK)
	f.bob.get("/api/users/me/dm-settings").decode(t, &s)
	if !s.MuteAll || !s.ListCollapsed {
		t.Fatalf("stored: %+v", s)
	}
	f.alice.get("/api/users/me/dm-settings").decode(t, &s)
	if s.MuteAll {
		t.Fatal("settings are personal")
	}
	wantStatus(t, "unknown field", f.bob.put("/api/users/me/dm-settings", map[string]any{"mute_all": true, "x": 1}), http.StatusBadRequest)

	wantStatus(t, "alice", f.alice.dmPost(dm.ID, "hey @bobby"), http.StatusCreated)
	wantStatus(t, "carol", f.carol.dmPost(other.ID, "hey @bobby"), http.StatusCreated)
	list := f.bob.polishList(t)
	if got := list[dm.ID]; got.NotifyLevel != "mute" || got.UnreadCount != 1 || got.MentionCount != 0 {
		t.Fatalf("muted conversation: %+v", got)
	}

	// A setting of the conversation's own wins.
	wantStatus(t, "own setting", f.bob.put("/api/channels/"+other.ID.String()+"/notifications", map[string]any{"level": "all"}), http.StatusNoContent)
	if got := f.bob.polishList(t)[other.ID]; got.NotifyLevel != "all" || got.UnreadCount != 1 {
		t.Fatalf("overridden conversation: %+v", got)
	}
	// Others see no change in how they are notified.
	if got := f.alice.polishList(t)[dm.ID]; got.NotifyLevel != "all" {
		t.Fatalf("alice: %+v", got)
	}
}

func TestTheConversationListPreviewsTheLatestMessage(t *testing.T) {
	f := newDMFixture(t, false)
	dm := f.alice.mustDM(t, f.bob)
	if f.alice.polishList(t)[dm.ID].LastMessage != nil {
		t.Fatal("no message yet")
	}
	wantStatus(t, "text", f.bob.dmPost(dm.ID, repeat("x", 300)), http.StatusCreated)
	got := f.alice.polishList(t)[dm.ID].LastMessage
	if got == nil || len(got.Text) != 200 || got.UserID != f.bob.user.ID {
		t.Fatalf("preview: %+v", got)
	}
	wantStatus(t, "block", f.alice.put("/api/me/blocks/"+f.bob.user.ID.String(), nil), http.StatusNoContent)
	if f.alice.polishList(t)[dm.ID].LastMessage != nil {
		t.Fatal("a blocked member's message is previewed")
	}
}

func repeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for range n {
		out = append(out, s...)
	}
	return string(out)
}
