//go:build integration

package push

import (
	"net/http"
	"testing"
)

func TestMuteAllDirectMessagesSilencesConversationsUnlessOneHasItsOwnSetting(t *testing.T) {
	e := newEnv(t, http.StatusCreated)
	alice, bob := e.member("alice"), e.member("bob")
	dm := e.perms.DM(alice.id, bob.id)
	e.exec(`INSERT INTO channels (id, number, name, type) OVERRIDING SYSTEM VALUE VALUES ($1, -1, '', 'dm')`, dm)
	e.exec(`UPDATE users SET dm_mute_all = TRUE WHERE id = $1`, bob.id)

	// Not even a message that names the member gets through.
	e.publish(message(alice, dm, "psst", bob))
	settle()
	if n := len(e.requestsTo("/device/bob")); n != 0 {
		t.Fatalf("bob muted all direct messages but has %d pushes", n)
	}

	// The conversation's own setting wins over the general one.
	e.exec(`INSERT INTO channel_reads (user_id, channel_id, notify_level) VALUES ($1, $2, 'all')`, bob.id, dm)
	e.publish(message(alice, dm, "now it counts"))
	e.pushedTo("/device/bob", 1)

	// Another member who did not mute is not affected.
	e.exec(`UPDATE users SET dm_mute_all = FALSE WHERE id = $1`, bob.id)
	e.exec(`DELETE FROM channel_reads WHERE user_id = $1`, bob.id)
	e.publish(message(alice, dm, "and again"))
	e.pushedTo("/device/bob", 2)
}
