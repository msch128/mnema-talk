//go:build integration

package server

import (
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Search, forward, saved, later, the bot API and push each decide for
// themselves whether a member may see a message: some through the permission
// snapshot, some through SQL (the conversation rule of chat.conversationReadableSQL).
// This test asks all of them about the same four messages and the same four
// members and requires one answer, so a rule that changes in one place and not
// in the others fails here. A block is a different matter: it hides a person's
// writing from the blocker in inboxes and stops pushes, but it never takes a
// message away from anyone's reach, so the reader table must not move.

type surfaceWorld struct {
	a                        *app
	admin, alice, bob, carol *client
	bot                      *botClient
	push                     *fakePushService
	loc                      map[string]uuid.UUID // channel or conversation per location
	author                   map[string]*client
	replier                  map[string]*client // the member who answers in a conversation
}

// Locations: an open channel, a channel only the Staff role (alice) and the
// Owner see, a conversation of alice and bobby, and one of bobby and carol.
const (
	locOpen  = "open"
	locStaff = "staff"
	locDMAB  = "dm alice-bobby"
	locDMBC  = "dm bobby-carol"
)

func newSurfaceWorld(t *testing.T) *surfaceWorld {
	t.Helper()
	a, fake := newPushApp(t, true, nil)
	admin := a.seedAdmin()
	w := &surfaceWorld{a: a, admin: admin, push: fake, alice: a.register(admin, "alice"), bob: a.register(admin, "bobby"), carol: a.register(admin, "carol")}

	staff := createRole(t, admin, "Staff", 0)
	assignRoles(t, admin, w.alice, staff)
	open := a.createChannel(admin, "general", "text")
	hidden := a.createChannel(admin, "staffroom", "text")
	wantStatus(t, "hide", putOverwrites(admin, "channels", hidden,
		overwrite("role", everyoneID(t, admin), 0, perm.ViewChannel), overwrite("role", staff, perm.ViewChannel, 0)), http.StatusNoContent)

	// The bot may see the open channel and nothing else, like botFixture's.
	botRole := createRole(t, admin, "Reader bot", perm.SendMessages)
	b := createBot(t, admin, "reader")
	wantStatus(t, "bot role", admin.put("/api/admin/bots/"+b.ID.String()+"/roles", map[string]any{"role_ids": []uuid.UUID{botRole}}), http.StatusOK)
	wantStatus(t, "bot scope", putOverwrites(admin, "channels", open, overwrite("role", botRole, perm.ViewChannel, 0)), http.StatusNoContent)
	tok := issueToken(t, admin, b.ID, map[string]any{"name": "test"})
	w.bot = &botClient{t: t, a: a, token: tok.Secret, user: b.UserID}

	w.loc = map[string]uuid.UUID{
		locOpen:  open,
		locStaff: hidden,
		locDMAB:  w.alice.mustDM(t, w.bob).ID,
		locDMBC:  w.bob.mustDM(t, w.carol).ID,
	}
	w.author = map[string]*client{locOpen: admin, locStaff: admin, locDMAB: w.alice, locDMBC: w.bob}
	w.replier = map[string]*client{locDMAB: w.bob, locDMBC: w.carol}
	for name, c := range map[string]*client{"alice": w.alice, "bobby": w.bob, "carol": w.carol, "owner": admin} {
		wantStatus(t, "subscribe "+name, c.post("/api/push/subscriptions", subscription(t, "/"+name)), http.StatusCreated)
	}
	return w
}

// post writes a message into the location as its author.
func (w *surfaceWorld) post(t *testing.T, loc, text string) uuid.UUID {
	t.Helper()
	res := w.author[loc].post("/api/channels/"+w.loc[loc].String()+"/messages", map[string]any{"content": text})
	wantStatus(t, "post "+loc, res, http.StatusCreated)
	return idOf(t, res)
}

// readers is the rule every surface must agree with. The Owner has every
// permission but reads no conversation they are not in.
func readers(loc string) map[string]bool {
	switch loc {
	case locOpen:
		return map[string]bool{"alice": true, "bobby": true, "carol": true, "owner": true, "bot": true}
	case locStaff:
		return map[string]bool{"alice": true, "owner": true}
	case locDMAB:
		return map[string]bool{"alice": true, "bobby": true}
	default:
		return map[string]bool{"bobby": true, "carol": true}
	}
}

func (w *surfaceWorld) people() map[string]*client {
	return map[string]*client{"alice": w.alice, "bobby": w.bob, "carol": w.carol, "owner": w.admin}
}

// sees asks every surface that can answer for one member and one message.
// The returned map is surface -> whether the message was within reach.
func (w *surfaceWorld) sees(t *testing.T, who string, c *client, loc, word string, id uuid.UUID) map[string]bool {
	t.Helper()
	out := map[string]bool{}

	found, _ := c.search(t, map[string]string{"q": word})
	for _, m := range found.Messages {
		if m.ID == id {
			out["search"] = true
		}
	}

	// Forwarding copies a quote into the open channel; a message out of reach is refused.
	out["forward"] = c.forward(id, w.loc[locOpen], "").status == http.StatusCreated

	save := c.put("/api/me/saved/"+id.String(), nil)
	out["saved"] = save.status == http.StatusOK
	if out["saved"] {
		wantStatus(t, "unsave", c.delete("/api/me/saved/"+id.String()), http.StatusNoContent)
	}

	out["later"] = c.post("/api/me/reminders", map[string]any{"message_id": id, "remind_at": inFuture(time.Hour)}).status == http.StatusCreated
	return out
}

// check requires every surface to give the table's answer for every member.
func (w *surfaceWorld) check(t *testing.T, state string, words map[string]string, ids map[string]uuid.UUID) {
	t.Helper()
	for loc, id := range ids {
		for who, c := range w.people() {
			want := readers(loc)[who]
			for surface, got := range w.sees(t, who, c, loc, words[loc], id) {
				if got != want {
					t.Errorf("[%s] %s on %q: %s answers %v, the rule says %v", state, who, loc, surface, got, want)
				}
			}
		}
		// The bot API reaches a message through its channel only.
		res := w.bot.get(botPath(w.loc[loc], "/messages/"+id.String()))
		if got, want := res.status == http.StatusOK, readers(loc)["bot"]; got != want {
			t.Errorf("[%s] bot on %q: bot API answers %d, the rule says %v", state, loc, res.status, want)
		}
	}
}

// pushes sends one new message into every location and requires the push
// service to reach exactly the readers, minus the author and anyone who
// blocked the author.
func (w *surfaceWorld) pushes(t *testing.T, state string, blocked map[string]map[uuid.UUID]bool) {
	t.Helper()
	for loc := range w.loc {
		// A conversation holds back a third message nobody answered, so the
		// other member answers first. Earlier sends push in the background,
		// so let those settle before counting.
		if reply, ok := w.replier[loc]; ok {
			wantStatus(t, "reply in "+loc, reply.post("/api/channels/"+w.loc[loc].String()+"/messages", map[string]any{"content": "answer"}), http.StatusCreated)
		}
		time.Sleep(250 * time.Millisecond)
		before := len(w.push.received())
		w.post(t, loc, "ping in "+loc)
		want := map[string]bool{}
		for who, ok := range readers(loc) {
			c, human := w.people()[who]
			if !ok || !human || c == w.author[loc] || blocked[who][w.author[loc].user.ID] {
				continue
			}
			want[who] = true
		}
		for who := range want {
			w.push.waitFor(t, "/"+who)
		}
		time.Sleep(250 * time.Millisecond)
		got := map[string]bool{}
		for _, p := range w.push.received()[before:] {
			got[p[1:]] = true
		}
		if !sameNames(got, want) {
			t.Errorf("[%s] push for a message in %q reached %v, the rule says %v", state, loc, readerNames(got), readerNames(want))
		}
	}
}

func sameNames(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func readerNames(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestEverySurfaceAgreesOnWhoMayReadAMessage(t *testing.T) {
	w := newSurfaceWorld(t)
	words := map[string]string{locOpen: "okapiopen", locStaff: "okapistaff", locDMAB: "okapialicebobby", locDMBC: "okapibobbycarol"}
	ids := map[string]uuid.UUID{}
	for loc, word := range words {
		ids[loc] = w.post(t, loc, "the "+word+" wording")
	}

	w.check(t, "no blocks", words, ids)
	w.pushes(t, "no blocks", nil)

	// One-way blocks that touch no conversation pair: they quiet the blocker's
	// pushes about the blocked person, and nothing else.
	wantStatus(t, "alice blocks the owner", w.alice.put("/api/me/blocks/"+w.admin.user.ID.String(), nil), http.StatusNoContent)
	wantStatus(t, "carol blocks alice", w.carol.put("/api/me/blocks/"+w.alice.user.ID.String(), nil), http.StatusNoContent)
	blocked := map[string]map[uuid.UUID]bool{
		"alice": {w.admin.user.ID: true},
		"carol": {w.alice.user.ID: true},
	}
	w.check(t, "one-way blocks", words, ids)
	w.pushes(t, "one-way blocks", blocked)

	// A mutual block inside a conversation closes it for writing, not for reading.
	wantStatus(t, "bobby blocks alice", w.bob.put("/api/me/blocks/"+w.alice.user.ID.String(), nil), http.StatusNoContent)
	wantStatus(t, "alice blocks bobby", w.alice.put("/api/me/blocks/"+w.bob.user.ID.String(), nil), http.StatusNoContent)
	w.check(t, "mutual block", words, ids)
}
