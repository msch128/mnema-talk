//go:build integration

package server

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
)

func sendWithNonce(f pollFixture, c *client, channel uuid.UUID, content string, nonce uuid.UUID) response {
	return c.post("/api/channels/"+channel.String()+"/messages", map[string]any{"content": content, "nonce": nonce})
}

func (f pollFixture) messageCount(channel uuid.UUID) int {
	return f.a.countRows(`SELECT COUNT(*) FROM messages WHERE channel_id = $1`, channel)
}

func TestSendingTheSameNonceTwiceStoresOneMessageAndReturnsTheFirst(t *testing.T) {
	f := newPollFixture(t)
	nonce := uuid.New()
	first := sendWithNonce(f, f.alice, f.channel, "once", nonce)
	wantStatus(t, "first send", first, http.StatusCreated)
	var m1, m2 core.Message
	first.decode(t, &m1)
	if m1.Nonce == nil || *m1.Nonce != nonce {
		t.Fatalf("the reply does not echo the nonce: %+v", m1.Nonce)
	}

	// The retry may carry other text (the member edited the pending copy meanwhile): the first message stands.
	second := sendWithNonce(f, f.alice, f.channel, "once, again", nonce)
	wantStatus(t, "retry", second, http.StatusOK)
	second.decode(t, &m2)
	if m2.ID != m1.ID || m2.Content != "once" {
		t.Fatalf("retry returned %s %q, want the first message %s", m2.ID, m2.Content, m1.ID)
	}
	if n := f.messageCount(f.channel); n != 1 {
		t.Fatalf("%d messages stored, want 1", n)
	}
	creates := 0
	for _, ev := range f.a.events.Snapshot() {
		if ev.Type == events.MessageCreate {
			creates++
		}
	}
	if creates != 1 {
		t.Fatalf("%d message_create events, want 1: a retry must not announce the message again", creates)
	}
}

func TestTheSameNonceFromTwoMembersMakesTwoMessages(t *testing.T) {
	f := newPollFixture(t)
	nonce := uuid.New()
	wantStatus(t, "alice", sendWithNonce(f, f.alice, f.channel, "from alice", nonce), http.StatusCreated)
	wantStatus(t, "bob", sendWithNonce(f, f.bob, f.channel, "from bob", nonce), http.StatusCreated)
	if n := f.messageCount(f.channel); n != 2 {
		t.Fatalf("%d messages stored, want 2: nonces are per author", n)
	}
}

func TestSendsWithoutANonceAreNeverDeduplicated(t *testing.T) {
	f := newPollFixture(t)
	f.a.send(f.alice, f.channel, "same")
	f.a.send(f.alice, f.channel, "same")
	if n := f.messageCount(f.channel); n != 2 {
		t.Fatalf("%d messages stored, want 2", n)
	}
}

func TestANonceUsedInOneConversationIsRefusedInAnother(t *testing.T) {
	f := newPollFixture(t)
	other := f.a.createChannel(f.owner, "random", "text")
	nonce := uuid.New()
	wantStatus(t, "first", sendWithNonce(f, f.alice, f.channel, "here", nonce), http.StatusCreated)
	wantStatus(t, "other channel", sendWithNonce(f, f.alice, other, "there", nonce), http.StatusBadRequest)
}

func TestSimultaneousSendsOfOneNonceStoreOneMessage(t *testing.T) {
	f := newPollFixture(t)
	nonce := uuid.New()
	codes := make(chan int, 6)
	for range 6 {
		go func() { codes <- sendWithNonce(f, f.alice, f.channel, "race", nonce).status }()
	}
	created := 0
	for range 6 {
		switch code := <-codes; code {
		case http.StatusCreated:
			created++
		case http.StatusOK:
		default:
			t.Fatalf("status %d", code)
		}
	}
	if n := f.messageCount(f.channel); n != 1 || created != 1 {
		t.Fatalf("%d messages stored, %d created answers; want 1 and 1", n, created)
	}
}

func TestRetryingADeletedMessageSaysDeletedAndDoesNotPostItAgain(t *testing.T) {
	f := newPollFixture(t)
	nonce := uuid.New()
	var m core.Message
	first := sendWithNonce(f, f.alice, f.channel, "purged", nonce)
	wantStatus(t, "first send", first, http.StatusCreated)
	first.decode(t, &m)
	wantStatus(t, "moderator deletes it", f.owner.delete(f.base()+"/messages/"+m.ID.String()), http.StatusNoContent)

	retry := sendWithNonce(f, f.alice, f.channel, "purged", nonce)
	wantStatus(t, "retry", retry, http.StatusGone)
	if want := `"code":"MESSAGE_DELETED"`; !strings.Contains(string(retry.body), want) {
		t.Fatalf("retry body %s lacks %s", retry.body, want)
	}
	if n := f.messageCount(f.channel); n != 0 {
		t.Fatalf("%d messages stored, want 0: the retry must not undo the deletion", n)
	}
}

func TestRetryOfAStoredMessageSucceedsAfterTheAuthorLostTheRightToWrite(t *testing.T) {
	f := newPollFixture(t)
	nonce := uuid.New()
	wantStatus(t, "first send", sendWithNonce(f, f.alice, f.channel, "arrived", nonce), http.StatusCreated)
	wantStatus(t, "timeout", f.owner.post("/api/admin/users/"+f.alice.user.ID.String()+"/timeout", map[string]any{"duration_seconds": 3600}), http.StatusOK)

	wantStatus(t, "a new message is refused", sendWithNonce(f, f.alice, f.channel, "new", uuid.New()), http.StatusForbidden)
	wantStatus(t, "the retry still gets its message", sendWithNonce(f, f.alice, f.channel, "arrived", nonce), http.StatusOK)
}

func TestRetryDoesNotReturnTheMessageToSomeoneWhoLostAccessToTheChannel(t *testing.T) {
	f := newPollFixture(t)
	nonce := uuid.New()
	wantStatus(t, "first send", sendWithNonce(f, f.alice, f.channel, "arrived", nonce), http.StatusCreated)
	everyone := everyoneID(t, f.owner)
	wantStatus(t, "hide the channel", putOverwrites(f.owner, "channels", f.channel, overwrite("role", everyone, 0, perm.ViewChannel)), http.StatusNoContent)

	res := sendWithNonce(f, f.alice, f.channel, "arrived", nonce)
	if res.status != http.StatusForbidden && res.status != http.StatusNotFound {
		t.Fatalf("retry after losing access: status %d %s, want the normal refusal", res.status, res.body)
	}
	if strings.Contains(string(res.body), "arrived") {
		t.Fatalf("the refusal leaks the message: %s", res.body)
	}
}

func TestOldNoncesArePruned(t *testing.T) {
	f := newPollFixture(t)
	nonce := uuid.New()
	wantStatus(t, "send", sendWithNonce(f, f.alice, f.channel, "old", nonce), http.StatusCreated)
	if _, err := f.a.db.Exec(context.Background(), `UPDATE message_nonces SET created_at = now() - interval '2 days'`); err != nil {
		t.Fatal(err)
	}
	if _, err := messages.PruneNonces(context.Background(), f.a.db); err != nil {
		t.Fatal(err)
	}
	if n := f.a.countRows(`SELECT COUNT(*) FROM message_nonces`); n != 0 {
		t.Fatalf("%d nonce rows left, want 0", n)
	}
}
