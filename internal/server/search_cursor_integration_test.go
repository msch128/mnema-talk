//go:build integration

package server

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

// A page cursor the caller may not read must answer exactly like one that does
// not exist, or the cursor check tells which message ids exist behind a
// hidden channel or a conversation.
func TestASearchCursorTheCallerCannotReadAnswersLikeAMissingOne(t *testing.T) {
	f := newCallFixture(t)
	alice, bob := f.dmFixture.alice, f.dmFixture.bob
	secret := f.a.createChannel(f.admin, "secret", "text")
	wantStatus(t, "hide", putOverwrites(f.admin, "channels", secret, overwrite("member", bob.user.ID, 0, perm.ViewChannel)), http.StatusNoContent)
	hidden := f.a.send(alice, secret, "needle")
	res := alice.dmPost(f.dm.ID, "needle")
	wantStatus(t, "a conversation message", res, http.StatusCreated)
	var dmMsg struct {
		ID uuid.UUID `json:"id"`
	}
	res.decode(t, &dmMsg)

	missing := uuid.New().String()
	for _, tc := range []struct {
		name, path string
		cursor     string
	}{
		{"search, hidden channel", "/api/search?q=needle&before=", hidden.String()},
		{"search, conversation", "/api/search?q=needle&before=", dmMsg.ID.String()},
		{"inbox, hidden channel", "/api/mentions?before=", hidden.String()},
	} {
		want := bob.get(tc.path + missing)
		got := bob.get(tc.path + tc.cursor)
		if got.status != want.status || got.status != http.StatusNotFound {
			t.Errorf("%s: unreadable cursor %d, missing cursor %d; both must be 404", tc.name, got.status, want.status)
		}
	}
}
