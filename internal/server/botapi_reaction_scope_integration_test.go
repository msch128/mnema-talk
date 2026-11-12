//go:build integration

package server

import (
	"bytes"
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

// TestBotCannotTakeBackAReactionWhereItCannotSee is the DELETE half of the
// reaction row of TestBotAPIScope, which channelRoutesNotProbed names for both
// methods but which probes only PUT. Taking back a reaction in a channel the
// bot cannot see, or through an allowed channel with a message of a hidden one,
// must answer exactly as for ids nobody has.
func TestBotCannotTakeBackAReactionWhereItCannotSee(t *testing.T) {
	f := newBotFixture(t, false)
	secret := f.a.createChannel(f.owner, "staff-only", "text")
	wantStatus(t, "private", putOverwrites(f.owner, "channels", secret,
		overwrite("role", everyoneID(t, f.owner), 0, perm.ViewChannel)), http.StatusNoContent)
	inSecret := postMessage(t, f.owner, secret, map[string]any{"content": "classified"})
	inOther := postMessage(t, f.owner, f.other, map[string]any{"content": "not for the bot"})
	mine := postMessage(t, f.owner, f.open, map[string]any{"content": "react to me"})
	emoji := url.PathEscape("👍")
	reaction := func(ch, msg uuid.UUID) string {
		return botPath(ch, "/messages/"+msg.String()+"/reactions/"+emoji+"/@me")
	}
	wantStatus(t, "take back where it may", f.client.delete(reaction(f.open, mine)), http.StatusNoContent)

	for name, c := range map[string]struct{ hidden, twin string }{
		"private channel":         {reaction(secret, inSecret), reaction(uuid.New(), uuid.New())},
		"channel without a grant": {reaction(f.other, inOther), reaction(uuid.New(), uuid.New())},
		"hidden message via open": {reaction(f.open, inSecret), reaction(f.open, uuid.New())},
	} {
		got, twin := f.client.delete(c.hidden), f.client.delete(c.twin)
		if got.status != http.StatusNotFound {
			t.Errorf("%s: %d %s, want 404", name, got.status, got.body)
		}
		if got.status != twin.status || !bytes.Equal(got.body, twin.body) {
			t.Errorf("%s: a hidden message answers %d %s, an unknown one %d %s", name, got.status, got.body, twin.status, twin.body)
		}
	}
}
