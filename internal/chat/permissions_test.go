package chat

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/channels"
	"github.com/msch128/mnema-talk/internal/chat/core"
)

// A handler built without a permission service must not come up: every way
// of mounting its routes panics, so the server fails to start instead of
// serving channels nobody checks.
func TestMountingAHandlerWithoutPermissionsPanics(t *testing.T) {
	for name, mount := range map[string]func(*Handler){
		"member routes": func(h *Handler) { h.Mount(chi.NewRouter()) },
		"admin routes":  func(h *Handler) { h.MountAdmin(chi.NewRouter()) },
		"bot routes": func(h *Handler) {
			h.MountBot(chi.NewRouter(), func(next http.Handler) http.Handler { return next })
		},
		"direct messages": func(h *Handler) { h.DMs() },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("mounted without a permission service")
				}
			}()
			mount(&Handler{})
		})
	}
}

// The channel list shows nothing when the permission state cannot be asked.
func TestChannelListFailsWithoutPermissions(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	cats := []core.Category{{ID: uuid.New(), Channels: []core.Channel{{ID: uuid.New()}}}}
	uncat := []core.Channel{{ID: uuid.New()}}
	gotCats, gotUncat, err := channels.VisibleHierarchy(nil, r, cats, uncat)
	if err == nil || gotCats != nil || gotUncat != nil {
		t.Fatalf("hierarchy: %+v %+v %v", gotCats, gotUncat, err)
	}
}

// A member's total shown to a caller drops the messages in channels the
// caller cannot view; callers who see everything get the stored total.
func TestEnrichMembersSubtractsHiddenMessages(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	members := []core.Member{{ID: a, MessageCount: 10}, {ID: b, MessageCount: 3}}
	got := enrichMembers(members, nil, nil, map[uuid.UUID]int64{a: 4, b: 5})
	if got[0].MessageCount != 6 || got[1].MessageCount != 0 {
		t.Fatalf("counts: %d %d", got[0].MessageCount, got[1].MessageCount)
	}
	got = enrichMembers(members, nil, nil, nil)
	if got[0].MessageCount != 10 || got[1].MessageCount != 3 {
		t.Fatalf("stored totals: %d %d", got[0].MessageCount, got[1].MessageCount)
	}
}
