//go:build integration

package push

import (
	"net/http"
	"testing"
)

func TestTheServerDefaultDecidesWhereChannelAndCategoryDoNot(t *testing.T) {
	e := newEnv(t, http.StatusCreated)
	alice, bob := e.member("alice"), e.member("bob")
	e.exec(`INSERT INTO server_notify (user_id, notify_level) VALUES ($1, 'mentions')`, bob.id)

	e.publish(message(alice, e.general, "just chatting"))
	settle()
	if got := e.requestsTo("/device/bob"); len(got) != 0 {
		t.Fatalf("server default mentions pushed %d times for an ordinary message", len(got))
	}
	e.publish(e.stored(message(alice, e.general, "@bob"), 0, bob))
	e.pushedTo("/device/bob", 1)

	// The channel's own setting beats the server default.
	e.exec(`INSERT INTO channel_reads (user_id, channel_id, notify_level) VALUES ($1, $2, 'all')`, bob.id, e.general)
	e.publish(message(alice, e.general, "now everything"))
	e.pushedTo("/device/bob", 2)
}

func TestTheCategoryBeatsTheServerDefault(t *testing.T) {
	e := newEnv(t, http.StatusCreated)
	alice, bob := e.member("alice"), e.member("bob")
	e.exec(`INSERT INTO server_notify (user_id, notify_level) VALUES ($1, 'mute')`, bob.id)
	e.exec(`INSERT INTO category_notify (user_id, category_id, notify_level) VALUES ($1, $2, 'all')`, bob.id, e.category)
	e.publish(message(alice, e.general, "category says all"))
	e.pushedTo("/device/bob", 1)

	// A channel that follows its category's "inherit" falls through to the server default.
	e.exec(`UPDATE category_notify SET notify_level = 'inherit' WHERE user_id = $1`, bob.id)
	e.publish(message(alice, e.general, "server says mute"))
	settle()
	if got := e.requestsTo("/device/bob"); len(got) != 1 {
		t.Fatalf("%d pushes, want only the first", len(got))
	}
}

func TestAServerWideMuteLetsThroughOnlyWhatIsAddressedAndExpires(t *testing.T) {
	e := newEnv(t, http.StatusCreated)
	alice, bob := e.member("alice"), e.member("bob")
	e.exec(`INSERT INTO server_notify (user_id, notify_level, muted_until) VALUES ($1, 'mute', NOW() + INTERVAL '1 hour')`, bob.id)
	e.publish(message(alice, e.general, "quiet please"))
	e.publish(e.stored(message(alice, e.general, "@all"), 1, bob))
	settle()
	if got := e.requestsTo("/device/bob"); len(got) != 0 {
		t.Fatalf("a muted server pushed %d times", len(got))
	}
	e.publish(e.stored(message(alice, e.general, "@bob"), 0, bob))
	e.pushedTo("/device/bob", 1)

	e.exec(`UPDATE server_notify SET muted_until = NOW() - INTERVAL '1 minute' WHERE user_id = $1`, bob.id)
	e.publish(message(alice, e.general, "back again"))
	e.pushedTo("/device/bob", 2)
}

func TestSuppressingEveryoneSilencesAllAndHereButNotAMention(t *testing.T) {
	e := newEnv(t, http.StatusCreated)
	alice, bob := e.member("alice"), e.member("bob")
	e.exec(`INSERT INTO server_notify (user_id, notify_level, suppress_everyone) VALUES ($1, 'mentions', TRUE)`, bob.id)
	e.publish(e.stored(message(alice, e.general, "@all"), 1, bob))
	settle()
	if got := e.requestsTo("/device/bob"); len(got) != 0 {
		t.Fatalf("@all pushed %d times to a member who suppresses it", len(got))
	}
	e.publish(e.stored(message(alice, e.general, "@bob"), 0, bob))
	e.pushedTo("/device/bob", 1)
}
