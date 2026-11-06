//go:build integration

package server

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/events"
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
