//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/chat/channels"
	"github.com/msch128/mnema-talk/internal/perm"
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
func TestARecountSkipsWhoReadTheChannelAndWhoCannotSeeIt(t *testing.T) {
	f := newMessageModerationFixture(t, true)
	id := f.a.send(f.peer, f.channel, "@talker @msgmod hello")
	if res := f.user.post(f.path("/read"), nil); res.status != http.StatusOK && res.status != http.StatusNoContent {
		t.Fatalf("read: %d %s", res.status, res.body)
	}
	// The moderator has not read it, but loses the channel before the delete.
	if res := putOverwrites(f.admin, "channels", f.channel, overwrite("member", f.mod.user.ID, 0, perm.ViewChannel)); res.status != http.StatusNoContent {
		t.Fatalf("hide: %d %s", res.status, res.body)
	}
	reader := f.user.dialLiveWS(f.a.origin())
	hidden := f.mod.dialLiveWS(f.a.origin())
	if res := f.peer.delete(f.path("/messages/" + id.String())); res.status != http.StatusNoContent {
		t.Fatalf("delete: %d %s", res.status, res.body)
	}
	waitRecountIdle(t, f.a)
	if raw, ok := reader.expect("read_state", 300*time.Millisecond); ok {
		t.Fatalf("a member who had read the channel was told: %s", raw)
	}
	for {
		raw, ok := hidden.expect("read_state", 300*time.Millisecond)
		if !ok {
			break
		}
		if strings.Contains(string(raw), f.channel.String()) {
			t.Fatalf("a member who cannot see the channel was told: %s", raw)
		}
	}
}

// waitRecountIdle waits until the server's recount worker has nothing left.
func waitRecountIdle(t *testing.T, a *app) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !a.router.recount.Idle() {
		if time.Now().After(deadline) {
			t.Fatal("the recount worker did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// The auto-delete sweep tells an online reader their counts again (written by
// the independent review of the H-L5-01 fix).
func TestTheAutoDeleteSweepTellsTheReaderTheirCounts(t *testing.T) {
	a := newApp(t, true)
	owner := a.seedAdmin()
	ch := a.createChannel(owner, "old", "text")
	reader := a.register(owner, "max")
	alice := a.register(owner, "alice")
	// The reader signed up before the old message, or it was never unread for them.
	if _, err := a.db.Exec(t.Context(), `UPDATE users SET created_at = now() - interval '30 days' WHERE id = $1`, reader.user.ID); err != nil {
		t.Fatal(err)
	}
	ws := reader.dialLiveWS(a.origin())
	id := a.send(alice, ch, "@max old news")
	if _, err := a.db.Exec(t.Context(), `UPDATE messages SET created_at = now() - interval '10 days' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if res := owner.put("/api/admin/channels/"+ch.String()+"/auto-delete", map[string]any{"days": 7}); res.status != http.StatusOK {
		t.Fatalf("auto-delete: %d %s", res.status, res.body)
	}
	if s := reader.readState(t, ch); s.UnreadCount != 1 || s.MentionCount != 1 {
		t.Fatalf("before the sweep: %+v", s)
	}
	hub := a.router.Hub
	d := &channels.AutoDeleter{DB: a.db, Events: hub, Objects: a.store, Audit: &audit.Log{DB: a.db}, Recount: a.router.recount}
	if err := d.Sweep(t.Context()); err != nil {
		t.Fatal(err)
	}
	if u, m := counts(t, ws, ch); u != 0 || m != 0 {
		t.Fatalf("after the sweep: unread %d, mentions %d; want 0, 0", u, m)
	}
}
