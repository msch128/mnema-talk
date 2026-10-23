//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
)

const (
	secretText = "TOPSECRETWORDING"
	secretChan = "staffroom"
)

type quote struct {
	Available bool       `json:"available"`
	Deleted   bool       `json:"deleted"`
	Content   string     `json:"content"`
	Channel   string     `json:"channel_name"`
	MessageID *uuid.UUID `json:"message_id"`
}

func (c *client) forward(msg, to uuid.UUID, comment string) response {
	body := map[string]any{"channel_id": to}
	if comment != "" {
		body["comment"] = comment
	}
	return c.post("/api/messages/"+msg.String()+"/forward", body)
}

func (c *client) quote(t *testing.T, forwarded uuid.UUID) quote {
	t.Helper()
	res := c.get("/api/messages/" + forwarded.String() + "/forward")
	wantStatus(t, "quote", res, http.StatusOK)
	var q quote
	res.decode(t, &q)
	return q
}

func idOf(t *testing.T, res response) uuid.UUID {
	t.Helper()
	var m struct {
		ID uuid.UUID `json:"id"`
	}
	res.decode(t, &m)
	return m.ID
}

// forwardWorld: staffroom is visible to the staff role (alice) and the Owner;
// general is open to everybody (bob has no access to staffroom).
type forwardWorld struct {
	a                  *app
	admin, alice, bob  *client
	staff              uuid.UUID
	secret, open, role uuid.UUID
}

func newForwardWorld(t *testing.T) *forwardWorld {
	a := newApp(t, false)
	admin := a.seedAdmin()
	w := &forwardWorld{a: a, admin: admin, alice: a.register(admin, "alice"), bob: a.register(admin, "bobby")}
	w.role = createRole(t, admin, "Staff", 0)
	assignRoles(t, admin, w.alice, w.role)
	w.secret = a.createChannel(admin, secretChan, "text")
	w.open = a.createChannel(admin, "general", "text")
	wantStatus(t, "hide", putOverwrites(admin, "channels", w.secret,
		overwrite("role", everyoneID(t, admin), 0, perm.ViewChannel), overwrite("role", w.role, perm.ViewChannel, 0)), http.StatusNoContent)
	w.staff = a.send(admin, w.secret, secretText)
	return w
}

func TestAForwardLeaksNothingOfAHiddenOriginalToAnyReader(t *testing.T) {
	w := newForwardWorld(t)
	res := w.alice.forward(w.staff, w.open, "worth a look")
	wantStatus(t, "alice forwards", res, http.StatusCreated)
	fwd := idOf(t, res)
	var created struct {
		Content string `json:"content"`
		Forward *struct {
			Forwarded bool `json:"forwarded"`
		} `json:"forward"`
	}
	res.decode(t, &created)
	if created.Content != "worth a look" || created.Forward == nil || !created.Forward.Forwarded {
		t.Fatalf("the new message holds the comment and says it is a forward: %s", res.body)
	}
	if strings.Contains(string(res.body), w.staff.String()) || strings.Contains(string(res.body), w.secret.String()) {
		t.Fatalf("the payload names the original: %s", res.body)
	}

	// bobby cannot see the original: nothing of it in any payload he or a bot can get.
	leaks := map[string]string{
		"list":    string(w.bob.get("/api/channels/" + w.open.String() + "/messages").body),
		"thread":  string(w.bob.get("/api/messages/" + fwd.String() + "/thread").body),
		"quote":   string(w.bob.get("/api/messages/" + fwd.String() + "/forward").body),
		"pins":    string(w.bob.get("/api/channels/" + w.open.String() + "/pins").body),
		"mention": string(w.bob.get("/api/mentions").body),
	}
	srch, _ := w.bob.search(t, map[string]string{"q": secretText})
	raw, _ := json.Marshal(srch)
	leaks["search"] = string(raw)
	srch, _ = w.bob.search(t, map[string]string{"q": "worth"})
	raw, _ = json.Marshal(srch)
	leaks["search comment"] = string(raw)
	for _, e := range w.a.events.Snapshot() {
		raw, _ := json.Marshal(e.Payload)
		if strings.Contains(string(raw), `"forward"`) {
			leaks["event "+e.Type] += string(raw)
		}
	}
	for name, body := range leaks {
		if strings.Contains(body, secretText) || strings.Contains(body, secretChan) || strings.Contains(body, w.staff.String()) || strings.Contains(body, w.secret.String()) {
			t.Errorf("%s leaks the hidden original: %s", name, body)
		}
	}
	if q := w.bob.quote(t, fwd); q.Available || q.Content != "" || q.Channel != "" {
		t.Errorf("bobby's quote: %+v", q)
	}
	if !strings.Contains(leaks["search comment"], "worth a look") {
		t.Errorf("the comment is searchable: %s", leaks["search comment"])
	}

	// alice and the Owner see the quote, and it follows edits.
	for name, c := range map[string]*client{"alice": w.alice, "owner": w.admin} {
		if q := c.quote(t, fwd); !q.Available || q.Content != secretText || q.Channel != secretChan || q.MessageID == nil || *q.MessageID != w.staff {
			t.Errorf("%s's quote: %+v", name, q)
		}
	}
	wantStatus(t, "edit", w.admin.put("/api/channels/"+w.secret.String()+"/messages/"+w.staff.String(), map[string]string{"content": "edited original"}), http.StatusOK)
	if q := w.alice.quote(t, fwd); q.Content != "edited original" {
		t.Errorf("an edit shows through: %+v", q)
	}

	// Losing access takes the quote away from the one who could see it.
	assignRoles(t, w.admin, w.alice)
	if q := w.alice.quote(t, fwd); q.Available || q.Content != "" {
		t.Errorf("alice after losing access: %+v", q)
	}

	// A deleted original shows through as deleted; the message stays a forward.
	wantStatus(t, "delete", w.admin.delete("/api/channels/"+w.secret.String()+"/messages/"+w.staff.String()), http.StatusNoContent)
	if q := w.bob.quote(t, fwd); q.Available || !q.Deleted {
		t.Errorf("a deleted original: %+v", q)
	}
	body := string(w.bob.get("/api/channels/" + w.open.String() + "/messages").body)
	if !strings.Contains(body, `"forward":{"forwarded":true}`) {
		t.Errorf("a forward of a deleted original still reads as a forward: %s", body)
	}
}

func TestForwardingAForwardPointsAtTheRootAndEveryRefusalReadsTheSame(t *testing.T) {
	w := newForwardWorld(t)
	first := idOf(t, w.alice.forward(w.staff, w.open, "one"))
	res := w.alice.forward(first, w.open, "two")
	wantStatus(t, "forward a forward", res, http.StatusCreated)
	if q := w.alice.quote(t, idOf(t, res)); q.MessageID == nil || *q.MessageID != w.staff {
		t.Errorf("a forward of a forward points at the root: %+v", q)
	}
	// Someone who cannot see the root cannot forward a forward of it either.
	refusals := map[string]response{
		"hidden original":        w.bob.forward(w.staff, w.open, ""),
		"forward of hidden root": w.bob.forward(first, w.open, ""),
		"unknown message":        w.bob.forward(uuid.New(), w.open, ""),
		"unknown target":         w.alice.forward(first, uuid.New(), ""),
		"hidden target":          w.bob.forward(w.open2(t), w.secret, ""),
	}
	for name, r := range refusals {
		if r.status != http.StatusForbidden {
			t.Errorf("%s: %d %s", name, r.status, r.body)
			continue
		}
		if r.errorBody(t).Error.Message != "Couldn't forward this message." {
			t.Errorf("%s answers differently: %s", name, r.body)
		}
	}
	wantStatus(t, "a bad body", w.alice.post("/api/messages/"+first.String()+"/forward", map[string]any{"nope": 1}), http.StatusBadRequest)
	wantStatus(t, "signed out", w.a.anon().forward(first, w.open, ""), http.StatusUnauthorized)
}

// open2 posts a message everybody can see.
func (w *forwardWorld) open2(t *testing.T) uuid.UUID {
	return w.a.send(w.admin, w.open, "for everybody")
}

func TestForwardingIntoAndOutOfDirectMessages(t *testing.T) {
	f := newDMFixture(t, false)
	open := f.a.createChannel(f.admin, "general", "text")
	pub := f.a.send(f.admin, open, "a public line")
	dm := f.alice.mustDM(t, f.bob)

	// Into a conversation: members only, and the quote is for members only.
	res := f.alice.forward(pub, dm.ID, "see this")
	wantStatus(t, "into a dm", res, http.StatusCreated)
	inDM := idOf(t, res)
	if q := f.bob.quote(t, inDM); !q.Available || q.Content != "a public line" {
		t.Errorf("bobby reads the quote: %+v", q)
	}
	wantStatus(t, "a stranger cannot read it", f.carol.get("/api/messages/"+inDM.String()+"/forward"), http.StatusNotFound)
	wantStatus(t, "the owner is no member", f.admin.get("/api/messages/"+inDM.String()+"/forward"), http.StatusNotFound)
	same := f.carol.forward(pub, dm.ID, "")
	if same.status != http.StatusForbidden || same.errorBody(t).Error.Message != "Couldn't forward this message." {
		t.Errorf("a non-member forwarding into a conversation: %d %s", same.status, same.body)
	}
	// Forwards in a conversation never reach bots and webhooks: no channel event, only the member sockets.
	for _, e := range f.a.events.Snapshot() {
		if e.Type == events.MessageCreate {
			raw, _ := json.Marshal(e.Payload)
			if strings.Contains(string(raw), "see this") && !strings.Contains(string(raw), dm.ID.String()) {
				t.Errorf("the forward event does not belong to the conversation: %s", raw)
			}
		}
	}

	// A block stops forwarding into a one-to-one conversation, with the same text.
	wantStatus(t, "block", f.bob.put("/api/me/blocks/"+f.alice.user.ID.String(), nil), http.StatusNoContent)
	blocked := f.alice.forward(pub, dm.ID, "again")
	if blocked.status != http.StatusForbidden || blocked.errorBody(t).Error.Message != "Couldn't forward this message." {
		t.Errorf("a block: %d %s", blocked.status, blocked.body)
	}

	// Out of a conversation into a channel: the ids of the conversation stay out of the payload.
	dm2 := f.alice.mustDM(t, f.carol)
	private := idOf(t, f.carol.dmPost(dm2.ID, "SECRET-DM-TEXT"))
	res = f.alice.forward(private, open, "from a dm")
	wantStatus(t, "out of a dm", res, http.StatusCreated)
	if strings.Contains(string(res.body), dm2.ID.String()) || strings.Contains(string(res.body), private.String()) || strings.Contains(string(res.body), "SECRET-DM-TEXT") {
		t.Errorf("the forward names the conversation: %s", res.body)
	}
	out := idOf(t, res)
	if q := f.bob.quote(t, out); q.Available {
		t.Errorf("bobby is no member of that conversation: %+v", q)
	}
	if q := f.admin.quote(t, out); q.Available {
		t.Errorf("neither is the owner: %+v", q)
	}
	if q := f.carol.quote(t, out); !q.Available || q.Content != "SECRET-DM-TEXT" || q.Channel != "" {
		t.Errorf("carol is: %+v", q)
	}
	// Forwarding the message of a conversation one is not in is refused like any other.
	if r := f.bob.forward(private, open, ""); r.status != http.StatusForbidden {
		t.Errorf("bobby forwarding a foreign conversation message: %d", r.status)
	}
}

func TestTheExportOfAForwarderHoldsTheCommentNotTheOriginal(t *testing.T) {
	a, _ := mailApp(t)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	open := a.createChannel(admin, "general", "text")
	src := a.send(admin, open, "the original wording")
	wantStatus(t, "forward", alice.forward(src, open, "my comment"), http.StatusCreated)
	wantStatus(t, "request", a.requestExport(alice, memberPassword, ""), http.StatusAccepted)
	a.waitForExport(alice)
	files := unzipAll(t, alice.get("/api/users/me/export/download").body)
	if !strings.Contains(files["messages.json"], "my comment") || strings.Contains(files["messages.json"], "original wording") {
		t.Errorf("messages.json: %s", files["messages.json"])
	}
}
