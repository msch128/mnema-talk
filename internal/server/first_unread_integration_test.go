//go:build integration

package server

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
)

func firstUnreadPath(ch uuid.UUID, since string) string {
	return "/api/channels/" + ch.String() + "/first-unread?since=" + url.QueryEscape(since)
}

func (c *client) firstUnread(t *testing.T, ch uuid.UUID, since time.Time) *uuid.UUID {
	t.Helper()
	res := c.get(firstUnreadPath(ch, since.Format(time.RFC3339Nano)))
	wantStatus(t, "first unread", res, http.StatusOK)
	var out struct {
		MessageID *uuid.UUID `json:"message_id"`
	}
	res.decode(t, &out)
	return out.MessageID
}

func TestFirstUnreadIsTheOldestTopLevelMessageOfOthersAfterTheMarker(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "allgemein", "text")
	max := a.register(admin, "max")
	since := time.Now().Add(-time.Hour)

	if got := max.firstUnread(t, ch, since); got != nil {
		t.Fatalf("empty channel: first unread %v", got)
	}
	own := postMessage(t, max, ch, map[string]any{"content": "mine never counts"})
	root := postMessage(t, admin, ch, map[string]any{"content": "the first unread one"})
	postMessage(t, admin, ch, map[string]any{"content": "a thread reply never counts", "parent_id": own})
	postMessage(t, admin, ch, map[string]any{"content": "later"})

	if got := max.firstUnread(t, ch, since); got == nil || *got != root {
		t.Fatalf("first unread = %v, want %s", got, root)
	}
	if got := max.firstUnread(t, ch, time.Now().Add(time.Hour)); got != nil {
		t.Fatalf("nothing after the future marker, got %v", got)
	}
}

func TestFirstUnreadRejectsBadInputAndHiddenChannels(t *testing.T) {
	f := newAccessFixture(t, false)
	secret := f.privateChannel(t, "staff-only", "text")
	postMessage(t, f.owner, secret, map[string]any{"content": "secret"})

	wantStatus(t, "no since", f.insider.get("/api/channels/"+secret.String()+"/first-unread"), http.StatusBadRequest)
	wantStatus(t, "bad since", f.insider.get(firstUnreadPath(secret, "yesterday")), http.StatusBadRequest)
	wantStatus(t, "outsider", f.outsider.get(firstUnreadPath(secret, time.Now().Add(-time.Hour).Format(time.RFC3339))), http.StatusNotFound)
	wantStatus(t, "unknown channel", f.insider.get(firstUnreadPath(uuid.New(), time.Now().Format(time.RFC3339))), http.StatusNotFound)
	wantStatus(t, "signed out", f.a.anon().get(firstUnreadPath(secret, time.Now().Format(time.RFC3339))), http.StatusUnauthorized)
	if got := f.insider.firstUnread(t, secret, time.Now().Add(-time.Hour)); got == nil {
		t.Fatal("the insider sees the unread message")
	}
}

func TestReadingAChannelWithNothingNewLeavesTheMarkerAlone(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "allgemein", "text")
	max := a.register(admin, "max")
	empty := a.createChannel(admin, "leer", "text")
	for i := 0; i < 2; i++ {
		wantStatus(t, "read empty channel", max.post("/api/channels/"+empty.String()+"/read", nil), http.StatusNoContent)
	}
	if n := a.countRows(`SELECT COUNT(*) FROM channel_reads WHERE user_id = $1 AND channel_id = $2 AND last_read_at IS NOT NULL`, max.user.ID, empty); n != 1 {
		t.Fatalf("the first read of an empty channel must leave exactly one marker for the divider, got %d", n)
	}
	postMessage(t, admin, ch, map[string]any{"content": "news"})

	read := func() {
		wantStatus(t, "mark read", max.post("/api/channels/"+ch.String()+"/read", nil), http.StatusNoContent)
	}
	read()
	var marker time.Time
	query := `SELECT last_read_at FROM channel_reads WHERE user_id = $1 AND channel_id = $2`
	if err := a.db.QueryRow(t.Context(), query, max.user.ID, ch).Scan(&marker); err != nil {
		t.Fatal(err)
	}
	read()
	read()
	var again time.Time
	if err := a.db.QueryRow(t.Context(), query, max.user.ID, ch).Scan(&again); err != nil {
		t.Fatal(err)
	}
	if !again.Equal(marker) {
		t.Fatalf("a read without news moved the marker from %v to %v", marker, again)
	}

	postMessage(t, admin, ch, map[string]any{"content": "more news"})
	if got := max.readState(t, ch); got.UnreadCount != 1 {
		t.Fatalf("unread = %d, want 1", got.UnreadCount)
	}
	read()
	if got := max.readState(t, ch); got.UnreadCount != 0 {
		t.Fatalf("unread after read = %d, want 0", got.UnreadCount)
	}
}
