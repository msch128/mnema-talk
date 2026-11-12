//go:build integration

package server

import (
	"math/rand/v2"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/gateway"
	"github.com/msch128/mnema-talk/internal/perm"
)

// The canary world: every object carries a word of its own in every text
// field, and every word belongs to a class that says who may read it. The
// sweep (visibility_canary_sweep_integration_test.go) then asks every route as
// every observer and fails on any word, or the id of any object, that reaches
// someone outside its class.

type canaryClass int

const (
	clsOpen     canaryClass = iota // an open channel: every member
	clsStaff                       // the Staff role's channels: Staff and the Owner
	clsDM                          // the conversation of insider and carol
	clsInsider                     // insider's own pending things
	clsExOwn                       // what exstaff wrote while on Staff
	clsReport                      // report notes and moderator notes: Manage reports and the Owner
	clsOwner                       // the Owner alone
	clsReporter                    // carol's report note: carol and Manage reports
)

var classNames = []string{"open", "staff", "dm", "insider", "exstaff-own", "report", "owner", "reporter"}

func (c canaryClass) String() string { return classNames[c] }

// canaryMay is what each observer may read; the observers are the members the
// sweep asks as. insider is the control: it must find its classes somewhere.
var canaryMay = map[string][]canaryClass{
	"outsider": {clsOpen},
	"carol":    {clsOpen, clsDM, clsReporter},
	"mod":      {clsOpen, clsReport, clsReporter},
	"exstaff":  {clsOpen, clsExOwn},
	"anon":     {},
	"bot":      {clsOpen},
	"insider":  {clsOpen, clsStaff, clsDM, clsInsider, clsExOwn},
	// Administrators and the Owner see every channel, but no conversation
	// they are not in and nobody's pending things (SECURITY.md, "Who can read").
	"admin": {clsOpen, clsStaff, clsExOwn, clsReport, clsOwner, clsReporter},
	"owner": {clsOpen, clsStaff, clsExOwn, clsReport, clsOwner, clsReporter},
}

func mayRead(who string, c canaryClass) bool {
	for _, k := range canaryMay[who] {
		if k == c {
			return true
		}
	}
	return false
}

type canaryWorld struct {
	a                                             *app
	owner, insider, outsider, carol, mod, exstaff *client
	admin                                         *client // holds Administrator, is not the Owner
	calls                                         bool    // the conversation gets a call (even seeds)
	bot                                           *botClient
	staffRole, modRole                            uuid.UUID
	open, openForum, staffText, staffForum        uuid.UUID
	staffTalk, staffCat, dm                       uuid.UUID
	words                                         map[string]canaryClass // word -> class
	wordWhat                                      map[string]string      // word -> the object it sits in
	ids                                           map[string]canaryClass // object id -> class (open ones are not listed)
	idWhat                                        map[string]string
	pool                                          map[string][]string // path parameter kind -> values to try
	rng                                           *rand.Rand
}

// word mints a canary of a class for one object.
func (w *canaryWorld) word(c canaryClass, what string) string {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	b := []byte("zq")
	for range 10 {
		b = append(b, letters[w.rng.IntN(len(letters))])
	}
	s := string(b)
	w.words[s], w.wordWhat[s] = c, what
	return s
}

// hide records an object id of a class; it also joins the pool of its kind.
func (w *canaryWorld) hide(c canaryClass, kind, what string, id uuid.UUID) uuid.UUID {
	if c != clsOpen {
		w.ids[id.String()], w.idWhat[id.String()] = c, what
	}
	w.pool[kind] = append(w.pool[kind], id.String())
	return id
}

func newCanaryWorld(t *testing.T, seed uint64) *canaryWorld {
	t.Helper()
	a := newApp(t, true)
	owner := a.seedAdmin()
	w := &canaryWorld{a: a, owner: owner, words: map[string]canaryClass{}, wordWhat: map[string]string{},
		ids: map[string]canaryClass{}, idWhat: map[string]string{}, pool: map[string][]string{}, rng: rand.New(rand.NewPCG(seed, seed^0x9e37))}
	w.insider, w.outsider, w.carol = a.register(owner, "insider"), a.register(owner, "outsider"), a.register(owner, "carol")
	w.mod, w.exstaff, w.admin = a.register(owner, "moderator"), a.register(owner, "exstaff"), a.register(owner, "boss")
	w.calls = seed%2 == 0
	for _, c := range []*client{owner, w.insider, w.outsider, w.carol, w.mod, w.exstaff, w.admin} {
		w.pool["user"] = append(w.pool["user"], c.user.ID.String())
	}
	w.staffRole = createRole(t, owner, "Staff", 0)
	// The moderator holds every permission but Administrator and Manage bots,
	// and no role that opens the Staff channels.
	w.modRole = createRole(t, owner, "Moderators", perm.All&^(perm.Administrator|perm.ManageBots))
	w.pool["role"] = append(w.pool["role"], w.staffRole.String(), w.modRole.String(), everyoneID(t, owner).String())
	assignRoles(t, owner, w.insider, w.staffRole)
	assignRoles(t, owner, w.exstaff, w.staffRole)
	assignRoles(t, owner, w.mod, w.modRole)
	assignRoles(t, owner, w.admin, createRole(t, owner, "Admins", perm.Administrator))
	return w
}

// private hides a channel or category from @everyone and shows it to Staff.
func (w *canaryWorld) private(t *testing.T, kind string, id uuid.UUID) {
	t.Helper()
	wantStatus(t, "make private", putOverwrites(w.owner, kind, id,
		overwrite("role", everyoneID(t, w.owner), 0, perm.ViewChannel),
		overwrite("role", w.staffRole, perm.ViewChannel, 0)), http.StatusNoContent)
}

func (w *canaryWorld) channel(t *testing.T, name, kind string, cat uuid.UUID) uuid.UUID {
	t.Helper()
	body := map[string]any{"name": name, "type": kind, "topic": name}
	if cat != uuid.Nil {
		body["category_id"] = cat
	}
	res := w.owner.post("/api/admin/channels", body)
	wantStatus(t, "channel "+name, res, http.StatusCreated)
	return idOf(t, res)
}

// channels builds an open channel and forum, and a private category holding a
// Staff text channel (renamed once, so it has a previous slug), a Staff forum
// and a Staff Talk.
func (w *canaryWorld) channels(t *testing.T) {
	t.Helper()
	w.open = w.hide(clsOpen, "channel", "open", w.channel(t, "open-"+w.word(clsOpen, "open name"), "text", uuid.Nil))
	w.openForum = w.hide(clsOpen, "channel", "open forum", w.channel(t, "board-"+w.word(clsOpen, "open forum name"), "forum", uuid.Nil))
	res := w.owner.post("/api/admin/categories", map[string]any{"name": "cat-" + w.word(clsStaff, "category name")})
	wantStatus(t, "category", res, http.StatusCreated)
	w.staffCat = w.hide(clsStaff, "category", "staff category", idOf(t, res))
	w.private(t, "categories", w.staffCat)
	w.staffText = w.hide(clsStaff, "channel", "staff text", w.channel(t, "staff-"+w.word(clsStaff, "staff text first name"), "text", w.staffCat))
	w.staffForum = w.hide(clsStaff, "channel", "staff forum", w.channel(t, "forum-"+w.word(clsStaff, "staff forum name"), "forum", w.staffCat))
	w.staffTalk = w.hide(clsStaff, "channel", "staff talk", w.channel(t, "talk-"+w.word(clsStaff, "staff talk name"), "voice", w.staffCat))
	for _, ch := range []uuid.UUID{w.staffText, w.staffForum, w.staffTalk} {
		w.private(t, "channels", ch)
	}
	wantStatus(t, "rename", w.owner.patch("/api/admin/channels/"+w.staffText.String(), map[string]any{
		"name": "staff-" + w.word(clsStaff, "staff text second name"), "topic": w.word(clsStaff, "staff text topic")}), http.StatusOK)
}

// say posts and records the message under its class.
func (w *canaryWorld) say(t *testing.T, c *client, cls canaryClass, ch uuid.UUID, what string, body map[string]any) uuid.UUID {
	t.Helper()
	if _, ok := body["content"]; !ok {
		body["content"] = what + " " + w.word(cls, what)
	}
	res := c.post("/api/channels/"+ch.String()+"/messages", body)
	wantStatus(t, "post "+what, res, http.StatusCreated)
	var m struct {
		ID     uuid.UUID
		Number int64
	}
	res.decode(t, &m)
	if m.Number > 0 {
		w.pool["number"] = append(w.pool["number"], strconv.FormatInt(m.Number, 10))
	}
	return w.hide(cls, "message", what, m.ID)
}

// staffMessages fills the Staff channels: a message naming every observer, a
// thread, a poll, a pin, an edit, an attachment, a forum post with a tag and a
// message in the Talk's chat.
func (w *canaryWorld) staffMessages(t *testing.T) {
	t.Helper()
	o := w.owner
	w.say(t, o, clsStaff, w.staffText, "staff mentions", map[string]any{
		"content": "@outsider @carol @moderator @exstaff @everyone " + w.word(clsStaff, "staff mentions")})
	root := w.say(t, o, clsStaff, w.staffText, "staff thread root", map[string]any{})
	w.say(t, o, clsStaff, w.staffText, "staff thread reply", map[string]any{"parent_id": root})
	edited := w.say(t, o, clsStaff, w.staffText, "staff edited before", map[string]any{})
	wantStatus(t, "edit", o.put("/api/channels/"+w.staffText.String()+"/messages/"+edited.String(),
		map[string]any{"content": "after " + w.word(clsStaff, "staff edited after")}), http.StatusOK)
	pinned := w.say(t, o, clsStaff, w.staffText, "staff pinned", map[string]any{})
	wantStatus(t, "pin", o.put("/api/channels/"+w.staffText.String()+"/messages/"+pinned.String()+"/pin", nil), http.StatusOK)
	res := o.post("/api/channels/"+w.staffText.String()+"/polls", map[string]any{"question": "poll " + w.word(clsStaff, "staff poll question"),
		"options": []string{w.word(clsStaff, "staff poll option 1"), w.word(clsStaff, "staff poll option 2")}})
	wantStatus(t, "poll", res, http.StatusCreated)
	w.hide(clsStaff, "message", "staff poll", idOf(t, res))
	file := w.a.stage(o, w.staffText, "plan-"+w.word(clsStaff, "staff file name")+".png")
	w.hide(clsStaff, "media", "staff file", file.ID)
	wantStatus(t, "send file", sendWith(o, w.staffText, "file "+w.word(clsStaff, "staff file message"), file.ID), http.StatusCreated)
	res = o.post("/api/channels/"+w.staffForum.String()+"/tags", map[string]any{"name": w.word(clsStaff, "staff forum tag")})
	wantStatus(t, "tag", res, http.StatusCreated)
	tag := idOf(t, res)
	res = o.post("/api/channels/"+w.staffForum.String()+"/posts", map[string]any{"title": "post " + w.word(clsStaff, "staff post title"),
		"content": "body " + w.word(clsStaff, "staff post body"), "tag_ids": []uuid.UUID{tag}})
	wantStatus(t, "post", res, http.StatusCreated)
	w.hide(clsStaff, "message", "staff forum post", idOf(t, res))
	w.say(t, o, clsStaff, w.staffTalk, "staff talk chat", map[string]any{})
}

// firstStaffMessage is the first Staff text message the world posted.
func (w *canaryWorld) firstStaffMessage() uuid.UUID {
	for _, id := range w.pool["message"] {
		if w.ids[id] == clsStaff {
			return uuid.MustParse(id)
		}
	}
	return uuid.Nil
}

// moderatedPost is a post of insider's in the Staff forum whose title a
// moderator (the Owner) changes, the way a moderator tidies a forum.
func (w *canaryWorld) moderatedPost(t *testing.T) {
	t.Helper()
	res := w.insider.post("/api/channels/"+w.staffForum.String()+"/posts", map[string]any{"title": "ask " + w.word(clsStaff, "staff post first title"),
		"content": "body " + w.word(clsStaff, "staff moderated post body")})
	wantStatus(t, "insider post", res, http.StatusCreated)
	post := w.hide(clsStaff, "message", "staff moderated post", idOf(t, res))
	wantStatus(t, "retitle", w.owner.patch("/api/channels/"+w.staffForum.String()+"/posts/"+post.String(), map[string]any{
		"title": "asked " + w.word(clsStaff, "staff post title set by a moderator")}), http.StatusOK)
}

// openMessages fills the open channel and forum, and gives carol an open
// message to report.
func (w *canaryWorld) openMessages(t *testing.T) uuid.UUID {
	t.Helper()
	w.say(t, w.owner, clsOpen, w.open, "open hello", map[string]any{})
	target := w.say(t, w.insider, clsOpen, w.open, "open by insider", map[string]any{})
	res := w.owner.post("/api/channels/"+w.openForum.String()+"/posts", map[string]any{"title": "open " + w.word(clsOpen, "open post title"),
		"content": "body " + w.word(clsOpen, "open post body")})
	wantStatus(t, "open post", res, http.StatusCreated)
	w.hide(clsOpen, "message", "open post", idOf(t, res))
	return target
}

// conversation is insider and carol's conversation with an attachment and a
// call; carol answers, so both may write more than two.
func (w *canaryWorld) conversation(t *testing.T) {
	t.Helper()
	w.dm = w.hide(clsDM, "channel", "dm insider-carol", w.insider.mustDM(t, w.carol).ID)
	w.pool["dm"] = append(w.pool["dm"], w.dm.String())
	w.say(t, w.insider, clsDM, w.dm, "dm first", map[string]any{})
	answer := w.say(t, w.carol, clsDM, w.dm, "dm answer", map[string]any{})
	// Both members carry words out: a Staff message and a conversation message
	// forwarded into the open channel, each with an open comment. The quote is
	// built per reader (SECURITY.md): only those who may read the original see it.
	for what, id := range map[string]uuid.UUID{"forward of staff": w.firstStaffMessage(), "forward of dm": answer} {
		res := w.insider.forward(id, w.open, "fwd "+w.word(clsOpen, what+" comment"))
		wantStatus(t, what, res, http.StatusCreated)
		w.hide(clsOpen, "message", what, idOf(t, res))
	}
	file := w.a.stage(w.insider, w.dm, "dm-"+w.word(clsDM, "dm file name")+".png")
	w.hide(clsDM, "media", "dm file", file.ID)
	wantStatus(t, "dm file", sendWith(w.insider, w.dm, "file "+w.word(clsDM, "dm file message"), file.ID), http.StatusCreated)
	if !w.calls {
		return // without a call an Administrator's channel list is "all" (perm.ViewableChannels nil)
	}
	res := w.insider.post("/api/dms/"+w.dm.String()+"/talk", nil)
	if res.status != http.StatusCreated && res.status != http.StatusOK {
		t.Fatalf("call: %d %s", res.status, res.body)
	}
	var talk struct {
		ID uuid.UUID `json:"channel_id"`
	}
	res.decode(t, &talk)
	if talk.ID == uuid.Nil {
		t.Fatalf("call without an id: %s", res.body)
	}
	w.hide(clsDM, "channel", "dm talk", talk.ID)
	wantStatus(t, "name the call", w.insider.patch("/api/talks/"+talk.ID.String(), map[string]any{"name": "call-" + w.word(clsDM, "dm talk name")}), http.StatusOK)
	w.say(t, w.carol, clsDM, talk.ID, "dm talk chat", map[string]any{})
}

// pending is what insider keeps for later and the event of the Staff Talk.
func (w *canaryWorld) pending(t *testing.T, reported uuid.UUID) {
	t.Helper()
	res := w.insider.post("/api/channels/"+w.open.String()+"/scheduled", map[string]any{
		"content": "later " + w.word(clsInsider, "insider scheduled"), "send_at": inFuture(time.Hour)})
	wantStatus(t, "schedule", res, http.StatusCreated)
	w.hide(clsInsider, "scheduled", "insider scheduled", idOf(t, res))
	res = w.owner.post("/api/events", map[string]any{"title": "event " + w.word(clsStaff, "staff event title"),
		"description": w.word(clsStaff, "staff event description"), "channel_id": w.staffTalk,
		"start_time": time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)})
	wantStatus(t, "event", res, http.StatusCreated)
	ev := w.hide(clsStaff, "event", "staff event", idOf(t, res))
	wantStatus(t, "comment", w.owner.post("/api/events/"+ev.String()+"/comments", map[string]any{
		"content": w.word(clsStaff, "staff event comment")}), http.StatusCreated)
	res = w.owner.post("/api/events", map[string]any{"title": "meetup " + w.word(clsOpen, "open event title"),
		"location": "Hall", "start_time": time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339)})
	wantStatus(t, "open event", res, http.StatusCreated)
	w.hide(clsOpen, "event", "open event", idOf(t, res))
	res = w.carol.post("/api/messages/"+reported.String()+"/report", map[string]any{"category": "spam", "note": w.word(clsReporter, "report note")})
	wantStatus(t, "report", res, http.StatusCreated)
	w.hide(clsReporter, "report", "report", idOf(t, res))
	wantStatus(t, "note", w.owner.post("/api/admin/users/"+w.outsider.user.ID.String()+"/moderation/notes", map[string]any{
		"body": w.word(clsReport, "moderator note about outsider")}), http.StatusCreated)
}

// demote has exstaff write, answer, save, follow and schedule in the Staff
// channel, then takes the role away. What exstaff wrote stays theirs; what
// others wrote there is out of reach from then on.
func (w *canaryWorld) demote(t *testing.T) {
	t.Helper()
	x := w.exstaff
	target := w.say(t, w.owner, clsStaff, w.staffText, "staff answered by exstaff", map[string]any{})
	w.say(t, x, clsExOwn, w.staffText, "exstaff own", map[string]any{})
	w.say(t, x, clsExOwn, w.staffText, "exstaff reply", map[string]any{"reply_to_id": target})
	w.say(t, x, clsExOwn, w.staffText, "exstaff in thread", map[string]any{"parent_id": target})
	wantStatus(t, "save", x.put("/api/me/saved/"+target.String(), nil), http.StatusOK)
	wantStatus(t, "follow", x.put("/api/threads/"+target.String()+"/follow", nil), http.StatusOK)
	wantStatus(t, "remind", x.post("/api/me/reminders", map[string]any{"message_id": target, "remind_at": inFuture(time.Hour)}), http.StatusCreated)
	if res := x.post("/api/messages/"+target.String()+"/reactions", map[string]string{"emoji": "👍"}); res.status >= 300 {
		t.Fatalf("react: %d %s", res.status, res.body)
	}
	res := x.post("/api/channels/"+w.staffText.String()+"/scheduled", map[string]any{
		"content": "later " + w.word(clsExOwn, "exstaff scheduled"), "send_at": inFuture(time.Hour)})
	wantStatus(t, "schedule", res, http.StatusCreated)
	w.say(t, w.owner, clsStaff, w.staffText, "staff after exstaff", map[string]any{"content": "@exstaff " + w.word(clsStaff, "staff mention of exstaff")})
	if res := setRoles(w.owner, x.user.ID); res.status != http.StatusNoContent {
		t.Fatalf("demote: %d %s", res.status, res.body)
	}
}

// build runs every step; the bot sees the open channel only.
func (w *canaryWorld) build(t *testing.T) {
	t.Helper()
	w.channels(t)
	reported := w.openMessages(t)
	w.staffMessages(t)
	w.moderatedPost(t)
	w.conversation(t)
	w.pending(t, reported)
	w.demote(t)
	botRole := createRole(t, w.owner, "Reader bot", perm.SendMessages)
	b := createBot(t, w.owner, "reader")
	wantStatus(t, "bot role", w.owner.put("/api/admin/bots/"+b.ID.String()+"/roles", map[string]any{"role_ids": []uuid.UUID{botRole}}), http.StatusOK)
	wantStatus(t, "bot scope", putOverwrites(w.owner, "channels", w.open, overwrite("role", botRole, perm.ViewChannel, 0)), http.StatusNoContent)
	wantStatus(t, "intents", w.owner.patch("/api/admin/bots/"+b.ID.String(), map[string]any{"allowed_intents": allBotIn}), http.StatusOK)
	tok := issueToken(t, w.owner, b.ID, map[string]any{"name": "test"})
	w.bot = &botClient{t: t, a: w.a, token: tok.Secret, user: b.UserID}
}

// gatewayFrames connects the bot to its gateway with every intent it holds
// and returns a function that hands back the dispatches received so far.
func (w *canaryWorld) gatewayFrames(t *testing.T) func() []wsEvent {
	t.Helper()
	w.a.router.Gateway.SetTimings(gateway.Timings{Heartbeat: time.Minute, Identify: 10 * time.Second, Resume: time.Minute, TokenCheck: time.Second})
	g := (&botFixture{a: w.a, client: w.bot}).dialGW("Bot " + w.bot.token)
	g.identify("", allBotIn)
	return func() []wsEvent {
		var out []wsEvent
		for {
			fr, ok := g.next(300 * time.Millisecond)
			if !ok {
				return out
			}
			if fr.T != nil {
				out = append(out, wsEvent{Type: *fr.T, Payload: fr.D})
			}
		}
	}
}
