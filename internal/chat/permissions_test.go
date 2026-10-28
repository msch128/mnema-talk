package chat

import (
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// A handler built without a permission service (unit tests of other
// features) limits nothing in channel lists, categories and layouts.
func TestHandlerWithoutPermissionsLimitsNothing(t *testing.T) {
	h := &Handler{}
	r := httptest.NewRequest("GET", "/", nil)
	cats := []Category{{ID: uuid.New(), Channels: []Channel{{ID: uuid.New()}}}}
	uncat := []Channel{{ID: uuid.New()}}
	gotCats, gotUncat, err := h.visibleHierarchy(r, cats, uncat)
	if err != nil || len(gotCats) != 1 || len(gotCats[0].Channels) != 1 || len(gotUncat) != 1 {
		t.Fatalf("hierarchy: %+v %+v %v", gotCats, gotUncat, err)
	}
	states := []ReadState{{ChannelID: uuid.New()}}
	if got, err := h.visibleReadStates(r, states); err != nil || len(got) != 1 {
		t.Fatalf("read states: %v %v", got, err)
	}
	if visible, err := h.viewableChannels(r); err != nil || visible != nil {
		t.Fatalf("viewable: %v %v", visible, err)
	}
	if err := h.requireCategory(r, uuid.New()); err != nil {
		t.Fatal(err)
	}
	if err := h.require(r, uuid.New(), 1); err != nil {
		t.Fatal(err)
	}
}

// A member's total shown to a caller drops the messages in channels the
// caller cannot view; callers who see everything get the stored total.
func TestEnrichMembersSubtractsHiddenMessages(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	members := []Member{{ID: a, MessageCount: 10}, {ID: b, MessageCount: 3}}
	got := enrichMembers(members, nil, nil, map[uuid.UUID]int64{a: 4, b: 5})
	if got[0].MessageCount != 6 || got[1].MessageCount != 0 {
		t.Fatalf("counts: %d %d", got[0].MessageCount, got[1].MessageCount)
	}
	got = enrichMembers(members, nil, nil, nil)
	if got[0].MessageCount != 10 || got[1].MessageCount != 3 {
		t.Fatalf("stored totals: %d %d", got[0].MessageCount, got[1].MessageCount)
	}
}
