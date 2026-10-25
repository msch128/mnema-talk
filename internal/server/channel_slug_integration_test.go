//go:build integration

package server

import (
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

type slugChannel struct {
	ID            uuid.UUID `json:"id"`
	Name          string    `json:"name"`
	Slug          string    `json:"slug"`
	PreviousSlugs []string  `json:"previous_slugs"`
}

func createSlugChannel(t *testing.T, owner *client, name, typ string) slugChannel {
	t.Helper()
	res := owner.post("/api/admin/channels", map[string]any{"name": name, "type": typ})
	wantStatus(t, "create "+name, res, http.StatusCreated)
	var ch slugChannel
	res.decode(t, &ch)
	return ch
}

// A channel's slug and its former slugs travel with the channel object, so
// only members who may see the channel ever learn them.
func TestChannelSlugsAreVisibleOnlyWithTheChannel(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	member := a.register(owner, "slugmember")
	every := everyoneID(t, owner)

	open := createSlugChannel(t, owner, "Open Plans", "text")
	if open.Slug != "open-plans" || len(open.PreviousSlugs) != 0 {
		t.Fatalf("created: %+v", open)
	}
	secret := createSlugChannel(t, owner, "Secret Plans", "voice")
	if secret.Slug != "secret-plans" {
		t.Fatalf("created: %+v", secret)
	}
	wantStatus(t, "hide", putOverwrites(owner, "channels", secret.ID, overwrite("role", every, 0, perm.ViewChannel)), http.StatusNoContent)

	var renamed slugChannel
	res := owner.patch("/api/admin/channels/"+secret.ID.String(), map[string]any{"name": "Quiet Room"})
	wantStatus(t, "rename", res, http.StatusOK)
	res.decode(t, &renamed)
	if renamed.Slug != "quiet-room" || !reflect.DeepEqual(renamed.PreviousSlugs, []string{"secret-plans"}) {
		t.Fatalf("renamed: %+v", renamed)
	}
	wantStatus(t, "rename open", owner.patch("/api/admin/channels/"+open.ID.String(), map[string]any{"name": "Public Plans"}), http.StatusOK)

	list := member.get("/api/channels")
	wantStatus(t, "member list", list, http.StatusOK)
	body := string(list.body)
	for _, leak := range []string{"secret-plans", "quiet-room", "Quiet Room", secret.ID.String()} {
		if strings.Contains(body, leak) {
			t.Errorf("a member who cannot see the channel learns %q", leak)
		}
	}
	for _, want := range []string{`"slug":"public-plans"`, `"previous_slugs":["open-plans"]`} {
		if !strings.Contains(body, want) {
			t.Errorf("the visible channel lacks %s in %s", want, body)
		}
	}
	if !strings.Contains(string(owner.get("/api/channels").body), `"previous_slugs":["secret-plans"]`) {
		t.Error("the owner does not see the former slug")
	}
}
