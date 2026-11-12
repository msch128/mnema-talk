//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

// counts waits for the next read_state of channel that carries counts.
func counts(t *testing.T, ws *wsConn, channel uuid.UUID) (unread, mentions int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		raw, ok := ws.expect("read_state", time.Until(deadline))
		if !ok {
			t.Fatal("no read_state with counts")
		}
		var p struct {
			ChannelID    uuid.UUID `json:"channel_id"`
			UnreadCount  *int      `json:"unread_count"`
			MentionCount *int      `json:"mention_count"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatal(err)
		}
		if p.ChannelID == channel && p.UnreadCount != nil && p.MentionCount != nil {
			return *p.UnreadCount, *p.MentionCount
		}
	}
}

// The badge counts the messages that exist now and their current mentions
// (doc/architecture.md: the live badge matches the badge after a reload). A
// delete or an edit brings no message a client could count, so the server
// sends the members whose count it may have moved their counts again.
func TestADeleteOrAnEditTellsTheMembersWhoseBadgeItMovesTheirCounts(t *testing.T) {
	f := newMessageModerationFixture(t, true)
	reader := f.user.dialLiveWS(f.a.origin())

	id := f.a.send(f.peer, f.channel, "@talker look")
	f.edit(f.peer, id, "never mind")
	if u, m := counts(t, reader, f.channel); u != 1 || m != 0 {
		t.Fatalf("after the edit that removes the mention: unread %d, mentions %d; want 1, 0", u, m)
	}
	f.edit(f.peer, id, "@talker look again")
	if u, m := counts(t, reader, f.channel); u != 1 || m != 1 {
		t.Fatalf("after the edit that adds it: unread %d, mentions %d; want 1, 1", u, m)
	}
	if res := f.peer.delete(f.path("/messages/" + id.String())); res.status != http.StatusNoContent {
		t.Fatalf("delete: %d %s", res.status, res.body)
	}
	if u, m := counts(t, reader, f.channel); u != 0 || m != 0 {
		t.Fatalf("after the delete: unread %d, mentions %d; want 0, 0", u, m)
	}
}

// A moderator's bulk delete (spam with @all) leaves no badge behind either.
func TestABulkDeleteTellsTheReadersTheirCountsAgain(t *testing.T) {
	f := newMessageModerationFixture(t, true)
	reader := f.user.dialLiveWS(f.a.origin())
	f.a.send(f.peer, f.channel, "@all buy now")
	f.a.send(f.peer, f.channel, "@all buy now")
	res := f.mod.post(f.path("/messages/bulk-delete"), map[string]any{"user_id": f.peer.user.ID, "count": 2})
	if res.status != http.StatusOK {
		t.Fatalf("bulk delete: %d %s", res.status, res.body)
	}
	if u, m := counts(t, reader, f.channel); u != 0 || m != 0 {
		t.Fatalf("after the bulk delete: unread %d, mentions %d; want 0, 0", u, m)
	}
}

// A member who read the channel was not counting the message: they are not
// told, and neither is one who cannot see the channel.
func TestARecountSkipsWhoReadTheChannel(t *testing.T) {
	f := newMessageModerationFixture(t, true)
	id := f.a.send(f.peer, f.channel, "hello")
	if res := f.user.post(f.path("/read"), nil); res.status != http.StatusOK && res.status != http.StatusNoContent {
		t.Fatalf("read: %d %s", res.status, res.body)
	}
	reader := f.user.dialLiveWS(f.a.origin())
	if res := f.peer.delete(f.path("/messages/" + id.String())); res.status != http.StatusNoContent {
		t.Fatalf("delete: %d %s", res.status, res.body)
	}
	if raw, ok := reader.expect("read_state", 500*time.Millisecond); ok {
		t.Fatalf("a member who had read the channel was told: %s", raw)
	}
}
