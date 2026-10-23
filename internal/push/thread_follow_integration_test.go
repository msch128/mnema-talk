//go:build integration

package push

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// reply is a stored thread reply of author to the root message.
func (e *env) reply(author member, root uuid.UUID, content string, mentions ...member) {
	e.t.Helper()
	msg := message(author, e.general, content, mentions...)
	msg.ParentID = &root
	e.exec(`INSERT INTO messages (id, channel_id, user_id, content, parent_id) VALUES ($1, $2, $3, $4, $5)`, msg.ID, msg.ChannelID, msg.UserID, msg.Content, root)
	for _, m := range mentions {
		e.exec(`INSERT INTO message_mentions (message_id, user_id, kind) VALUES ($1, $2, 0)`, msg.ID, m.id)
	}
	e.publish(msg)
}

func TestAThreadReplyOnlyTellsTheThreadsFollowers(t *testing.T) {
	e := newEnv(t, http.StatusCreated)
	alice, bob, carol, dave := e.member("alice"), e.member("bob"), e.member("carol"), e.member("dave")
	root := e.stored(message(alice, e.general, "root"), 0)
	e.exec(`INSERT INTO thread_follows (user_id, thread_id) VALUES ($1, $3), ($2, $3)`, alice.id, bob.id, root.ID)
	e.exec(`INSERT INTO thread_follows (user_id, thread_id, following) VALUES ($1, $2, FALSE)`, dave.id, root.ID)

	e.reply(carol, root.ID, "a reply")
	e.pushedTo("/device/alice", 1)
	e.pushedTo("/device/bob", 1)
	settle()
	for _, m := range []member{carol, dave} {
		if got := e.requestsTo("/device/" + m.name); len(got) != 0 {
			t.Errorf("%s does not follow the thread but got %d pushes", m.name, len(got))
		}
	}
}

func TestAMentionInAThreadReachesAnUnfollowedMember(t *testing.T) {
	e := newEnv(t, http.StatusCreated)
	alice, bob := e.member("alice"), e.member("bob")
	root := e.stored(message(alice, e.general, "root"), 0)
	e.exec(`INSERT INTO thread_follows (user_id, thread_id, following) VALUES ($1, $2, FALSE)`, bob.id, root.ID)

	e.reply(alice, root.ID, "@bob look", bob)
	p := e.content(bob, e.pushedTo("/device/bob", 1)[0])
	if !p.Mention {
		t.Fatalf("push: %+v", p)
	}
}

func TestChannelMessagesAreNotLimitedByThreadFollows(t *testing.T) {
	e := newEnv(t, http.StatusCreated)
	alice := e.member("alice")
	e.member("bob")
	e.publish(message(alice, e.general, "top level"))
	e.pushedTo("/device/bob", 1)
}
