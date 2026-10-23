//go:build integration

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
)

type savedEntry struct {
	MessageID uuid.UUID `json:"message_id"`
	Note      string    `json:"note"`
	Available bool      `json:"available"`
	Message   *struct {
		Content string `json:"content"`
	} `json:"message"`
}

func (c *client) saved(t *testing.T) (items []savedEntry, raw string) {
	t.Helper()
	res := c.get("/api/me/saved")
	wantStatus(t, "list saved", res, http.StatusOK)
	var page struct {
		Items []savedEntry `json:"items"`
	}
	res.decode(t, &page)
	return page.Items, string(res.body)
}

// hiddenChannel makes a channel only the Owner (and whoever gets the role) may view.
func (a *app) hiddenChannel(admin *client, name string) uuid.UUID {
	ch := a.createChannel(admin, name, "text")
	everyone := everyoneID(a.t, admin)
	wantStatus(a.t, "hide", putOverwrites(admin, "channels", ch, overwrite("role", everyone, 0, perm.ViewChannel)), http.StatusNoContent)
	return ch
}

func TestSavedMessagesArePrivateAndRecheckedOnEveryRead(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	alice, bob := a.register(admin, "alice"), a.register(admin, "bobby")
	open := a.createChannel(admin, "general", "text")
	secret := a.hiddenChannel(admin, "staff")
	public := a.send(bob, open, "a public line")
	private := a.send(admin, secret, "the secret wording")

	wantStatus(t, "save public", alice.put("/api/me/saved/"+public.String(), map[string]string{"note": "read later"}), http.StatusOK)
	wantStatus(t, "save again with another note", alice.put("/api/me/saved/"+public.String(), map[string]string{"note": "read soon"}), http.StatusOK)
	wantStatus(t, "no body keeps the note", alice.put("/api/me/saved/"+public.String(), nil), http.StatusOK)
	wantStatus(t, "a hidden message does not exist", alice.put("/api/me/saved/"+private.String(), nil), http.StatusNotFound)
	wantStatus(t, "unknown message", alice.put("/api/me/saved/"+uuid.NewString(), nil), http.StatusNotFound)
	wantStatus(t, "long note", alice.put("/api/me/saved/"+public.String(), map[string]string{"note": strings.Repeat("x", 201)}), http.StatusBadRequest)
	wantStatus(t, "signed out", a.anon().put("/api/me/saved/"+public.String(), nil), http.StatusUnauthorized)

	items, _ := alice.saved(t)
	if len(items) != 1 || !items[0].Available || items[0].Message == nil || items[0].Message.Content != "a public line" || items[0].Note != "read soon" {
		t.Fatalf("alice's list: %+v", items)
	}
	if other, _ := bob.saved(t); len(other) != 0 {
		t.Fatalf("a saved list is private: %+v", other)
	}

	// Access is rechecked at read time: the entry stays, the message is gone from view.
	a.runSQL(`INSERT INTO saved_messages (user_id, message_id, note) VALUES ($1, $2, 'my own words')`, alice.user.ID, private)
	items, raw := alice.saved(t)
	if len(items) != 2 {
		t.Fatalf("two entries expected: %s", raw)
	}
	if strings.Contains(raw, "secret wording") || strings.Contains(raw, secret.String()) {
		t.Fatalf("the list leaks a message the member cannot see: %s", raw)
	}
	var gone *savedEntry
	for i := range items {
		if items[i].MessageID == private {
			gone = &items[i]
		}
	}
	if gone == nil || gone.Available || gone.Message != nil || gone.Note != "my own words" {
		t.Fatalf("an unavailable entry keeps the id and the note only: %+v", gone)
	}
	wantStatus(t, "remove unavailable", alice.delete("/api/me/saved/"+private.String()), http.StatusNoContent)
	wantStatus(t, "remove again", alice.delete("/api/me/saved/"+private.String()), http.StatusNoContent)
	if items, _ := alice.saved(t); len(items) != 1 {
		t.Fatalf("after removing: %+v", items)
	}

	// Deleting the message removes the entry.
	wantStatus(t, "delete", bob.delete("/api/channels/"+open.String()+"/messages/"+public.String()), http.StatusNoContent)
	if items, _ := alice.saved(t); len(items) != 0 {
		t.Fatalf("a deleted message leaves the list: %+v", items)
	}
}

func TestSavedMessagesAreAnnouncedToTheOwnSessionsOnlyAndBounded(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	ch := a.createChannel(admin, "general", "text")
	msg := a.send(admin, ch, "hello")
	wantStatus(t, "save", alice.put("/api/me/saved/"+msg.String(), nil), http.StatusOK)
	wantStatus(t, "save again", alice.put("/api/me/saved/"+msg.String(), map[string]string{"note": "n"}), http.StatusOK)
	wantStatus(t, "remove", alice.delete("/api/me/saved/"+msg.String()), http.StatusNoContent)
	wantStatus(t, "remove again", alice.delete("/api/me/saved/"+msg.String()), http.StatusNoContent)
	var told int
	for _, e := range a.events.Snapshot() {
		if e.Type == events.SavedUpdated {
			told++
			if len(e.Recipients) != 1 || e.Recipients[0] != alice.user.ID {
				t.Errorf("saved_updated went to %v", e.Recipients)
			}
		}
	}
	if told != 3 {
		t.Errorf("saved_updated sent %d times, want 3 (save, note change, remove)", told)
	}
}

func TestTheSavedListIsCappedAtAThousand(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	ch := a.createChannel(admin, "general", "text")
	first := a.send(admin, ch, "first")
	extra := a.send(admin, ch, "one too many")
	a.runSQL(`INSERT INTO messages (channel_id, user_id, content)
		SELECT $1, $2, 'filler' FROM generate_series(1, 999)`, ch, admin.user.ID)
	a.runSQL(`INSERT INTO saved_messages (user_id, message_id) SELECT $1, id FROM messages WHERE content = 'filler'`, alice.user.ID)
	wantStatus(t, "the 1000th", alice.put("/api/me/saved/"+first.String(), nil), http.StatusOK)
	wantStatus(t, "one too many", alice.put("/api/me/saved/"+extra.String(), nil), http.StatusConflict)
	wantStatus(t, "a note on a saved one still works", alice.put("/api/me/saved/"+first.String(), map[string]string{"note": "n"}), http.StatusOK)
}

func TestSavedMessagesPageNewestFirst(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	ch := a.createChannel(admin, "general", "text")
	var ids []uuid.UUID
	for _, text := range []string{"one", "two", "three"} {
		id := a.send(admin, ch, text)
		ids = append(ids, id)
		wantStatus(t, "save", alice.put("/api/me/saved/"+id.String(), nil), http.StatusOK)
		time.Sleep(5 * time.Millisecond)
	}
	res := alice.get("/api/me/saved?limit=2")
	wantStatus(t, "page 1", res, http.StatusOK)
	var p1 struct {
		Items   []savedEntry `json:"items"`
		HasMore bool         `json:"has_more"`
	}
	res.decode(t, &p1)
	if len(p1.Items) != 2 || !p1.HasMore || p1.Items[0].MessageID != ids[2] || p1.Items[1].MessageID != ids[1] {
		t.Fatalf("page 1: %+v", p1)
	}
	res = alice.get("/api/me/saved?limit=2&before=" + ids[1].String())
	wantStatus(t, "page 2", res, http.StatusOK)
	var p2 struct {
		Items   []savedEntry `json:"items"`
		HasMore bool         `json:"has_more"`
	}
	res.decode(t, &p2)
	if len(p2.Items) != 1 || p2.HasMore || p2.Items[0].MessageID != ids[0] {
		t.Fatalf("page 2: %+v", p2)
	}
}

func TestTheExportAndTheEraseCoverSavedMessages(t *testing.T) {
	a, _ := mailApp(t)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	ch := a.createChannel(admin, "general", "text")
	msg := a.send(admin, ch, "the saved wording")
	wantStatus(t, "save", alice.put("/api/me/saved/"+msg.String(), map[string]string{"note": "alice's note"}), http.StatusOK)

	wantStatus(t, "request", a.requestExport(alice, memberPassword, ""), http.StatusAccepted)
	a.waitForExport(alice)
	files := unzipAll(t, alice.get("/api/users/me/export/download").body)
	if !strings.Contains(files["saved.json"], msg.String()) || !strings.Contains(files["saved.json"], "alice's note") || strings.Contains(files["saved.json"], "wording") {
		t.Errorf("saved.json lists the reference and the note, not the text: %s", files["saved.json"])
	}

	wantStatus(t, "erase", a.deleteAccount(alice, memberPassword, ""), http.StatusNoContent)
	var n int
	if err := a.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM saved_messages`).Scan(&n); err != nil || n != 0 {
		t.Errorf("saved rows after erase: %d, %v", n, err)
	}
}

func TestPinsCarryWhoPinnedAndWhen(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	ch := a.createChannel(admin, "general", "text")
	msg := a.send(alice, ch, "worth keeping")
	other := a.send(alice, ch, "not pinned")

	wantStatus(t, "pin", admin.put("/api/channels/"+ch.String()+"/messages/"+msg.String()+"/pin", nil), http.StatusOK)
	res := admin.get("/api/channels/" + ch.String() + "/pins")
	wantStatus(t, "pins", res, http.StatusOK)
	var pins []struct {
		ID       uuid.UUID  `json:"id"`
		PinnedAt *time.Time `json:"pinned_at"`
		PinnedBy *struct {
			ID          uuid.UUID `json:"id"`
			DisplayName string    `json:"display_name"`
		} `json:"pinned_by"`
	}
	res.decode(t, &pins)
	if len(pins) != 1 || pins[0].ID != msg || pins[0].PinnedAt == nil || pins[0].PinnedBy == nil || pins[0].PinnedBy.ID != admin.user.ID {
		t.Fatalf("pins: %s", res.body)
	}

	// The pin event carries them too, and an unpinned message has neither.
	var seen bool
	for _, e := range a.events.Snapshot() {
		if e.Type != events.PinsChanged {
			continue
		}
		raw, err := json.Marshal(e.Payload)
		if err != nil {
			t.Fatal(err)
		}
		seen = seen || strings.Contains(string(raw), `"pinned_by"`)
	}
	if !seen {
		t.Error("pins_changed lacks pinned_by")
	}
	res = alice.get("/api/channels/" + ch.String() + "/messages")
	if strings.Count(string(res.body), `"pinned_by"`) != 1 || !strings.Contains(string(res.body), other.String()) {
		t.Errorf("only the pinned message carries pinned_by: %s", res.body)
	}
	wantStatus(t, "unpin", admin.delete("/api/channels/"+ch.String()+"/messages/"+msg.String()+"/pin"), http.StatusOK)
	res = alice.get("/api/channels/" + ch.String() + "/messages")
	if strings.Contains(string(res.body), `"pinned_by"`) || strings.Contains(string(res.body), `"pinned_at"`) {
		t.Errorf("an unpinned message has no pin fields: %s", res.body)
	}
}
