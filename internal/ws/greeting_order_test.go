package ws

import (
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
)

// The wire contract (api/openapi.json) promises server_info first on connect,
// even when the connection also triggers a presence broadcast to itself.
func TestServerInfoIsTheFirstEventOnConnect(t *testing.T) {
	h, _ := newSFUHub(t)
	t.Cleanup(h.Close)
	c := testClient(h, auth.User{ID: uuid.New(), Username: "newcomer"})
	types := drainTypes(t, c)
	if len(types) < 2 || types[0] != "server_info" {
		t.Fatalf("first events on connect = %v, want server_info first", types)
	}
}

// The per-account state (presence choice, profile, counters) lives exactly as
// long as one of the account's connections does.
func TestLastDisconnectForgetsTheAccountsHubState(t *testing.T) {
	h, _ := newSFUHub(t)
	u := auth.User{ID: uuid.New(), Username: "tabs"}
	first, second := testClient(h, u), testClient(h, u)

	h.unregister(first)
	h.mu.RLock()
	kept := h.online[u.ID] == 1 && h.active[u.ID] == 1 && h.chosen[u.ID] != "" && h.profiles[u.ID].ID == u.ID
	h.mu.RUnlock()
	if !kept {
		t.Fatal("one closed tab dropped the state of the remaining connection")
	}

	h.unregister(second)
	h.mu.RLock()
	defer h.mu.RUnlock()
	if _, ok := h.online[u.ID]; ok {
		t.Fatal("online counter survived the last disconnect")
	}
	if _, ok := h.active[u.ID]; ok {
		t.Fatal("active counter survived the last disconnect")
	}
	if _, ok := h.chosen[u.ID]; ok {
		t.Fatal("chosen presence survived the last disconnect")
	}
	if _, ok := h.profiles[u.ID]; ok {
		t.Fatal("profile survived the last disconnect")
	}
}
