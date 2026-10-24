//go:build integration

package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
)

type automodFixture struct {
	a                     *app
	admin, mod, member    *client
	delegate              *client // holds the Administrator permission through a role
	general               uuid.UUID
	modRole, everyoneRole uuid.UUID
}

func newAutomodFixture(t *testing.T) *automodFixture {
	t.Helper()
	a := newApp(t, false)
	admin := a.seedAdmin()
	f := &automodFixture{a: a, admin: admin}
	f.mod = a.register(admin, "moddy")
	f.member = a.register(admin, "chatty")
	f.delegate = a.register(admin, "deputy")
	f.modRole = createRole(t, admin, "queue readers", perm.ManageReports|perm.TimeoutMembers|perm.BanMembers|perm.ManageMessages)
	assignRoles(t, admin, f.mod, f.modRole)
	assignRoles(t, admin, f.delegate, createRole(t, admin, "deputies", perm.Administrator))
	f.everyoneRole = everyoneID(t, admin)
	f.general = a.createChannel(admin, "general", "text")
	return f
}

// age makes the accounts old enough for the new-account rule.
func (f *automodFixture) age(clients ...*client) {
	f.a.t.Helper()
	for _, c := range clients {
		if _, err := f.a.db.Exec(context.Background(), `UPDATE users SET created_at = NOW() - INTERVAL '30 days' WHERE id = $1`, c.user.ID); err != nil {
			f.a.t.Fatal(err)
		}
	}
}

func (f *automodFixture) addRule(body map[string]any) (id uuid.UUID) {
	f.a.t.Helper()
	res := f.admin.post("/api/admin/automod/rules", body)
	wantStatus(f.a.t, "add rule", res, http.StatusCreated)
	var out struct{ ID uuid.UUID }
	res.decode(f.a.t, &out)
	return out.ID
}

func wordRule(name string, action string, words ...string) map[string]any {
	return map[string]any{"kind": "words", "name": name, "enabled": true, "action": action, "config": map[string]any{"words": words}}
}

func (f *automodFixture) say(c *client, ch uuid.UUID, content string) response {
	return c.post("/api/channels/"+ch.String()+"/messages", map[string]any{"content": content})
}

type queueEntry struct {
	ID             uuid.UUID  `json:"id"`
	Kind           string     `json:"kind"`
	Category       string     `json:"category"`
	Source         string     `json:"source"`
	AutomodRule    string     `json:"automod_rule"`
	AutomodOutcome string     `json:"automod_outcome"`
	HitCount       int        `json:"hit_count"`
	MessageID      *uuid.UUID `json:"message_id"`
	ChannelID      *string    `json:"channel_id"`
	Reporter       *struct{}  `json:"reporter"`
	Reported       *struct {
		Username string `json:"username"`
	} `json:"reported"`
}

func (f *automodFixture) queue() []queueEntry {
	f.a.t.Helper()
	res := f.mod.get("/api/admin/reports?status=open")
	wantStatus(f.a.t, "queue", res, http.StatusOK)
	var page struct{ Reports []queueEntry }
	res.decode(f.a.t, &page)
	return page.Reports
}

func (f *automodFixture) messageTexts(ch uuid.UUID) string {
	f.a.t.Helper()
	res := f.admin.get("/api/channels/" + ch.String() + "/messages")
	wantStatus(f.a.t, "list messages", res, http.StatusOK)
	return string(res.body)
}

func TestAutomodRulesAreAdministratorOnlyOnEveryRoute(t *testing.T) {
	f := newAutomodFixture(t)
	id := f.addRule(wordRule("words", "block", "spam"))
	rule := wordRule("words", "block", "spam")
	calls := map[string]func(c *client) response{
		"list":   func(c *client) response { return c.get("/api/admin/automod") },
		"create": func(c *client) response { return c.post("/api/admin/automod/rules", rule) },
		"update": func(c *client) response { return c.put("/api/admin/automod/rules/"+id.String(), rule) },
		"delete": func(c *client) response { return c.delete("/api/admin/automod/rules/" + uuid.NewString()) },
	}
	for name, call := range calls {
		wantStatus(t, "anonymous "+name, call(f.a.anon()), http.StatusUnauthorized)
		wantStatus(t, "member "+name, call(f.member), http.StatusForbidden)
		// Every moderation permission short of Administrator is not enough.
		wantStatus(t, "moderator "+name, call(f.mod), http.StatusForbidden)
	}
	wantStatus(t, "deputy list", calls["list"](f.delegate), http.StatusOK)
	wantStatus(t, "deputy create", calls["create"](f.delegate), http.StatusCreated)
	wantStatus(t, "deputy update", calls["update"](f.delegate), http.StatusOK)
	wantStatus(t, "owner delete", f.admin.delete("/api/admin/automod/rules/"+id.String()), http.StatusNoContent)
	wantStatus(t, "owner delete again", f.admin.delete("/api/admin/automod/rules/"+id.String()), http.StatusNotFound)
	wantStatus(t, "owner update gone", f.admin.put("/api/admin/automod/rules/"+id.String(), rule), http.StatusNotFound)
}

func TestAutomodRuleInputIsValidatedAndLimited(t *testing.T) {
	f := newAutomodFixture(t)
	raid := map[string]any{"kind": "raid", "name": "raid", "enabled": true, "action": "flag", "config": map[string]any{"joins": 5, "window_seconds": 10, "restrict_seconds": 300}}
	f.addRule(raid)
	wantStatus(t, "second raid rule", f.admin.post("/api/admin/automod/rules", raid), http.StatusConflict)

	for name, body := range map[string]map[string]any{
		"unknown kind":     {"kind": "x", "name": "n", "enabled": true, "action": "block", "config": map[string]any{}},
		"no name":          {"kind": "mentions", "name": " ", "enabled": true, "action": "block", "config": map[string]any{"max_mentions": 5}},
		"bad action":       {"kind": "mentions", "name": "n", "enabled": true, "action": "ban", "config": map[string]any{"max_mentions": 5}},
		"bad config":       {"kind": "mentions", "name": "n", "enabled": true, "action": "block", "config": map[string]any{"max_mentions": 0}},
		"unknown field":    {"kind": "mentions", "name": "n", "enabled": true, "action": "block", "config": map[string]any{"max_mentions": 5}, "extra": 1},
		"unknown role":     {"kind": "mentions", "name": "n", "enabled": true, "action": "block", "config": map[string]any{"max_mentions": 5}, "exempt_roles": []string{uuid.NewString()}},
		"unknown channel":  {"kind": "mentions", "name": "n", "enabled": true, "action": "block", "config": map[string]any{"max_mentions": 5}, "exempt_channels": []string{uuid.NewString()}},
		"timeout too long": {"kind": "mentions", "name": "n", "enabled": true, "action": "block", "timeout_seconds": 99999999, "config": map[string]any{"max_mentions": 5}},
	} {
		wantStatus(t, name, f.admin.post("/api/admin/automod/rules", body), http.StatusBadRequest)
	}

	// The kind of a rule cannot change.
	id := f.addRule(wordRule("w", "block", "x"))
	wantStatus(t, "change kind", f.admin.put("/api/admin/automod/rules/"+id.String(), raid), http.StatusBadRequest)

	// 25 rules at most (the raid rule and the words rule above count).
	for i := 0; i < 23; i++ {
		f.addRule(wordRule(fmt.Sprintf("w%d", i), "block", "x"))
	}
	wantStatus(t, "26th rule", f.admin.post("/api/admin/automod/rules", wordRule("one more", "block", "x")), http.StatusConflict)
}

func TestABlockedMessageIsRefusedExplainedAndQueuedWithoutDuplicatingTheQueue(t *testing.T) {
	f := newAutomodFixture(t)
	f.addRule(wordRule("No spam", "block", "spam", "buy n*"))

	res := f.say(f.member, f.general, "this is SPAM, really")
	wantStatus(t, "blocked", res, http.StatusBadRequest)
	if msg := res.errorBody(t).Error.Message; !strings.Contains(msg, "No spam") || !strings.Contains(msg, "automod") {
		t.Fatalf("the author must be told which rule: %q", msg)
	}
	if strings.Contains(f.messageTexts(f.general), "SPAM") {
		t.Fatal("a blocked message must never be stored")
	}
	wantStatus(t, "whole words only", f.say(f.member, f.general, "spammer is fine"), http.StatusCreated)

	q := f.queue()
	if len(q) != 1 {
		t.Fatalf("queue: %+v", q)
	}
	e := q[0]
	if e.Source != "automod" || e.AutomodRule != "No spam" || e.AutomodOutcome != "blocked" || e.Category != "automod" || e.Kind != "channel" ||
		e.HitCount != 1 || e.Reporter != nil || e.Reported == nil || e.Reported.Username != "chatty" || e.MessageID != nil || e.ChannelID == nil {
		t.Fatalf("entry: %+v", e)
	}

	// More hits of the same member and rule count up on the one open entry.
	wantStatus(t, "again", f.say(f.member, f.general, "buy now"), http.StatusBadRequest)
	wantStatus(t, "and again", f.say(f.member, f.general, "SPAM"), http.StatusBadRequest)
	if q = f.queue(); len(q) != 1 || q[0].HitCount != 3 {
		t.Fatalf("hits merge while the entry is open: %+v", q)
	}
	// The copy is the blocked text, no context: moderators see why.
	d := (&reportsFixture{a: f.a}).detail(f.mod, q[0].ID)
	if d.Snapshot.Message.Content != "this is SPAM, really" || len(d.Snapshot.Context) != 0 {
		t.Fatalf("snapshot: %+v", d.Snapshot)
	}
	// Once closed, a new hit opens a new entry.
	wantStatus(t, "close", f.mod.post("/api/admin/reports/"+q[0].ID.String()+"/resolve", map[string]any{"status": "resolved"}), http.StatusOK)
	wantStatus(t, "after close", f.say(f.member, f.general, "spam"), http.StatusBadRequest)
	if q = f.queue(); len(q) != 1 || q[0].HitCount != 1 {
		t.Fatalf("a new entry after closing: %+v", q)
	}
}

func TestAFlaggedMessageIsDeliveredAndQueuedWithItsId(t *testing.T) {
	f := newAutomodFixture(t)
	f.addRule(wordRule("Watch list", "flag", "hmm"))
	id := f.a.send(f.member, f.general, "well hmm okay")
	q := f.queue()
	if len(q) != 1 || q[0].AutomodOutcome != "flagged" || q[0].MessageID == nil || *q[0].MessageID != id {
		t.Fatalf("queue: %+v", q)
	}
	if !strings.Contains(f.messageTexts(f.general), "well hmm okay") {
		t.Fatal("a flagged message stays")
	}
}

func TestARuleCanTimeTheAuthorOutAndAdministratorsAreNeverHit(t *testing.T) {
	f := newAutomodFixture(t)
	rule := wordRule("Slurs", "block", "badword")
	rule["timeout_seconds"] = 600
	f.addRule(rule)

	wantStatus(t, "blocked", f.say(f.member, f.general, "badword"), http.StatusBadRequest)
	var until *time.Time
	var reason string
	if err := f.a.db.QueryRow(context.Background(), `SELECT timeout_until, timeout_reason FROM users WHERE id = $1`, f.member.user.ID).Scan(&until, &reason); err != nil {
		t.Fatal(err)
	}
	if until == nil || time.Until(*until) < 9*time.Minute || !strings.Contains(reason, "Slurs") {
		t.Fatalf("timeout: %v %q", until, reason)
	}
	wantStatus(t, "now silenced", f.say(f.member, f.general, "hello"), http.StatusForbidden)
	notified := false
	for _, ev := range f.a.events.Snapshot() {
		if ev.Type == events.ModerationNotice && len(ev.Recipients) == 1 && ev.Recipients[0] == f.member.user.ID {
			notified = true
		}
	}
	if !notified {
		t.Fatal("the member must be told through a moderation_notice")
	}
	var audits int
	if err := f.a.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM audit_log WHERE action = 'automod.timeout' AND actor_name = 'Automod'`).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("audit entries %d %v", audits, err)
	}

	// The Owner and holders of Administrator pass every rule.
	wantStatus(t, "owner", f.say(f.admin, f.general, "badword"), http.StatusCreated)
	wantStatus(t, "deputy", f.say(f.delegate, f.general, "badword"), http.StatusCreated)
	if until, _ := f.timeoutOf(f.delegate); until != nil {
		t.Fatal("an administrator is never timed out by a rule")
	}
}

func (f *automodFixture) timeoutOf(c *client) (*time.Time, error) {
	var until *time.Time
	err := f.a.db.QueryRow(context.Background(), `SELECT timeout_until FROM users WHERE id = $1`, c.user.ID).Scan(&until)
	return until, err
}

func TestEditsAreCheckedLikeNewMessages(t *testing.T) {
	f := newAutomodFixture(t)
	id := f.a.send(f.member, f.general, "all fine here")
	f.addRule(wordRule("No spam", "block", "spam"))
	path := "/api/channels/" + f.general.String() + "/messages/" + id.String()

	res := f.member.put(path, map[string]any{"content": "now it is spam"})
	wantStatus(t, "blocked edit", res, http.StatusBadRequest)
	if !strings.Contains(res.errorBody(t).Error.Message, "edit") {
		t.Fatalf("the notice names the edit: %s", res.body)
	}
	if body := f.messageTexts(f.general); !strings.Contains(body, "all fine here") || strings.Contains(body, "spam") {
		t.Fatal("a blocked edit leaves the message as it was")
	}
	if q := f.queue(); len(q) != 1 || q[0].AutomodOutcome != "blocked" || q[0].MessageID == nil || *q[0].MessageID != id {
		t.Fatalf("the hit names the edited message: %+v", q)
	}
	wantStatus(t, "clean edit", f.member.put(path, map[string]any{"content": "still fine"}), http.StatusOK)

	// Someone else's edit is refused as before and makes no entry for anybody.
	other := f.a.send(f.mod, f.general, "mod text")
	wantStatus(t, "not the author", f.member.put("/api/channels/"+f.general.String()+"/messages/"+other.String(), map[string]any{"content": "spam"}), http.StatusForbidden)
	if q := f.queue(); len(q) != 1 || q[0].HitCount != 1 {
		t.Fatalf("queue: %+v", q)
	}

	// A flagging rule keeps the edit and queues it.
	f.addRule(wordRule("Watch", "flag", "peek"))
	wantStatus(t, "flagged edit", f.member.put(path, map[string]any{"content": "a peek"}), http.StatusOK)
	var flagged bool
	for _, e := range f.queue() {
		flagged = flagged || (e.AutomodOutcome == "flagged" && e.MessageID != nil && *e.MessageID == id)
	}
	if !flagged {
		t.Fatalf("flagged edit missing: %+v", f.queue())
	}
}

func TestLinkFilterAndMentionSpam(t *testing.T) {
	f := newAutomodFixture(t)
	links := f.addRule(map[string]any{"kind": "links", "name": "Links", "enabled": true, "action": "block",
		"config": map[string]any{"mode": "allow_list", "domains": []string{"example.com"}}})
	f.addRule(map[string]any{"kind": "mentions", "name": "Mention spam", "enabled": true, "action": "block", "config": map[string]any{"max_mentions": 2}})

	wantStatus(t, "allowed domain", f.say(f.member, f.general, "docs at https://docs.example.com/x"), http.StatusCreated)
	wantStatus(t, "other domain", f.say(f.member, f.general, "see https://evil.test/x"), http.StatusBadRequest)
	wantStatus(t, "two mentions", f.say(f.member, f.general, "@moddy @deputy hi"), http.StatusCreated)
	wantStatus(t, "same mention repeated", f.say(f.member, f.general, "@moddy @moddy @moddy"), http.StatusCreated)
	wantStatus(t, "three distinct mentions", f.say(f.member, f.general, "@moddy @deputy @Herzog hi"), http.StatusBadRequest)

	// Switching the mode takes effect at once (the compiled rules reload).
	body := map[string]any{"kind": "links", "name": "Links", "enabled": true, "action": "block", "config": map[string]any{"mode": "block_all"}}
	wantStatus(t, "update", f.admin.put("/api/admin/automod/rules/"+links.String(), body), http.StatusOK)
	wantStatus(t, "every link now", f.say(f.member, f.general, "https://docs.example.com/x"), http.StatusBadRequest)
	body["enabled"] = false
	wantStatus(t, "disable", f.admin.put("/api/admin/automod/rules/"+links.String(), body), http.StatusOK)
	wantStatus(t, "disabled rule", f.say(f.member, f.general, "https://evil.test/x"), http.StatusCreated)
	wantStatus(t, "delete", f.admin.delete("/api/admin/automod/rules/"+links.String()), http.StatusNoContent)
}

func TestNewAccountProtection(t *testing.T) {
	f := newAutomodFixture(t)
	f.age(f.mod)
	cfg := func(restrict string) map[string]any {
		return map[string]any{"kind": "new_account", "name": "New accounts", "enabled": true, "action": "block", "config": map[string]any{"max_age_seconds": 600, "restrict": restrict}}
	}
	id := f.addRule(cfg("links"))
	put := func(restrict string) {
		wantStatus(t, "update "+restrict, f.admin.put("/api/admin/automod/rules/"+id.String(), cfg(restrict)), http.StatusOK)
	}

	wantStatus(t, "plain text is fine", f.say(f.member, f.general, "hello everyone"), http.StatusCreated)
	wantStatus(t, "new account link", f.say(f.member, f.general, "https://example.com"), http.StatusBadRequest)
	wantStatus(t, "old account link", f.say(f.mod, f.general, "https://example.com"), http.StatusCreated)

	upload := func(c *client) response {
		return c.upload("/api/channels/"+f.general.String()+"/upload", "file", "cat.png", "image/png", pngBytes, nil)
	}
	wantStatus(t, "links only: a file is fine", upload(f.member), http.StatusCreated)
	put("attachments")
	wantStatus(t, "files restricted", upload(f.member), http.StatusBadRequest)
	wantStatus(t, "files restricted: link is fine", f.say(f.member, f.general, "https://example.com"), http.StatusCreated)
	wantStatus(t, "old account file", upload(f.mod), http.StatusCreated)
	put("everything")
	wantStatus(t, "cannot post at all", f.say(f.member, f.general, "hello"), http.StatusBadRequest)
	wantStatus(t, "old accounts still can", f.say(f.mod, f.general, "hello"), http.StatusCreated)
}

func TestExemptRolesAndChannelsSkipTheRule(t *testing.T) {
	f := newAutomodFixture(t)
	trusted := createRole(t, f.admin, "trusted", 0)
	vip := f.a.register(f.admin, "vippy")
	assignRoles(t, f.admin, vip, trusted)
	offtopic := f.a.createChannel(f.admin, "off-topic", "text")
	rule := wordRule("No spam", "block", "spam")
	rule["exempt_roles"] = []uuid.UUID{trusted}
	rule["exempt_channels"] = []uuid.UUID{offtopic}
	f.addRule(rule)

	wantStatus(t, "member in general", f.say(f.member, f.general, "spam"), http.StatusBadRequest)
	wantStatus(t, "member in the exempt channel", f.say(f.member, offtopic, "spam"), http.StatusCreated)
	wantStatus(t, "exempt role", f.say(vip, f.general, "spam"), http.StatusCreated)
	wantStatus(t, "exempt role, edit", vip.put("/api/channels/"+f.general.String()+"/messages/"+f.a.send(vip, f.general, "fine").String(), map[string]any{"content": "spam"}), http.StatusOK)
}

func TestInADirectMessageOnlyLinksAndMentionSpamApplyAndNoTextIsKept(t *testing.T) {
	f := newAutomodFixture(t)
	buddy := f.a.register(f.admin, "buddy")
	f.addRule(wordRule("No spam", "block", "spam"))
	f.addRule(map[string]any{"kind": "links", "name": "Links", "enabled": true, "action": "block", "config": map[string]any{"mode": "block_all"}})
	dm := f.member.mustDM(t, buddy)

	wantStatus(t, "word rules stay out of conversations", f.member.dmPost(dm.ID, "spam"), http.StatusCreated)
	res := f.member.dmPost(dm.ID, "secret link https://private.test/path")
	wantStatus(t, "link rule applies", res, http.StatusBadRequest)

	q := f.queue()
	if len(q) != 1 {
		t.Fatalf("queue: %+v", q)
	}
	if q[0].Kind != "dm" || q[0].ChannelID != nil || q[0].AutomodRule != "Links" {
		t.Fatalf("entry: %+v", q[0])
	}
	d := (&reportsFixture{a: f.a}).detail(f.mod, q[0].ID)
	if d.Snapshot.Message.Content != "" || len(d.Snapshot.Context) != 0 {
		t.Fatalf("a conversation's text never reaches the queue: %+v", d.Snapshot)
	}
	if strings.Contains(string(f.mod.get("/api/admin/reports/"+q[0].ID.String()).body), "private.test") {
		t.Fatal("the blocked text leaked")
	}
	// A flagged conversation message is delivered and queued without its text.
	f.addRule(map[string]any{"kind": "mentions", "name": "Mentions", "enabled": true, "action": "flag", "config": map[string]any{"max_mentions": 1}})
	wantStatus(t, "flagged", f.member.dmPost(dm.ID, "@buddy @moddy @deputy"), http.StatusCreated)
	var flagged queueEntry
	for _, e := range f.queue() {
		if e.AutomodRule == "Mentions" {
			flagged = e
		}
	}
	if flagged.Kind != "dm" || flagged.AutomodOutcome != "flagged" {
		t.Fatalf("flagged entry: %+v", flagged)
	}
	if d := (&reportsFixture{a: f.a}).detail(f.mod, flagged.ID); d.Snapshot.Message.Content != "" {
		t.Fatalf("no text for conversation entries: %+v", d.Snapshot.Message)
	}
}

func TestRaidProtectionRestrictsNewcomersAndAlertsTheQueue(t *testing.T) {
	f := newAutomodFixture(t)
	f.addRule(map[string]any{"kind": "raid", "name": "Raid protection", "enabled": true, "action": "flag",
		"config": map[string]any{"joins": 3, "window_seconds": 60, "restrict_seconds": 300}})
	// The fixture's members joined before the rule existed. Move them out of the window.
	if _, err := f.a.db.Exec(context.Background(), `UPDATE users SET created_at = NOW() - INTERVAL '1 day'`); err != nil {
		t.Fatal(err)
	}
	var joiners []*client
	for i := 0; i < 3; i++ {
		c := f.a.register(f.admin, fmt.Sprintf("joiner%d", i))
		joiners = append(joiners, c)
		wantStatus(t, "within the allowance", f.say(c, f.general, "hi"), http.StatusCreated)
	}
	if n := len(f.queue()); n != 0 {
		t.Fatalf("three joins are no raid: %d entries", n)
	}

	fourth := f.a.register(f.admin, "joiner3")
	for _, c := range append(joiners, fourth) {
		wantStatus(t, "restricted: "+c.user.Username, f.say(c, f.general, "hello"), http.StatusForbidden)
	}
	q := f.queue()
	if len(q) != 1 || q[0].Kind != "alert" || q[0].Source != "automod" || q[0].AutomodOutcome != "alert" || q[0].AutomodRule != "Raid protection" ||
		q[0].Reported != nil || q[0].MessageID != nil || q[0].ChannelID != nil || q[0].HitCount != 4 {
		t.Fatalf("alert: %+v", q)
	}
	// Whoever joins while it lasts is restricted too, on the same alert.
	fifth := f.a.register(f.admin, "joiner4")
	wantStatus(t, "joined during the raid", f.say(fifth, f.general, "hello"), http.StatusForbidden)
	if q = f.queue(); len(q) != 1 || q[0].HitCount != 5 {
		t.Fatalf("one alert per raid: %+v", q)
	}
	d := (&reportsFixture{a: f.a}).detail(f.mod, q[0].ID)
	if d.Snapshot.Message.Content != "" {
		t.Fatalf("alert snapshot: %+v", d.Snapshot)
	}

	// The established members were not touched, and a moderator lifts a restriction like any timeout.
	wantStatus(t, "old member", f.say(f.member, f.general, "still here"), http.StatusCreated)
	wantStatus(t, "lift", f.admin.delete("/api/admin/users/"+fourth.user.ID.String()+"/timeout"), http.StatusNoContent)
	wantStatus(t, "lifted", f.say(fourth, f.general, "thanks"), http.StatusCreated)
	var raids int
	if err := f.a.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM audit_log WHERE action = 'automod.raid'`).Scan(&raids); err != nil || raids != 1 {
		t.Fatalf("audit: %d %v", raids, err)
	}
	st := f.admin.get("/api/admin/automod")
	wantStatus(t, "state", st, http.StatusOK)
	if !strings.Contains(string(st.body), "raid_until") {
		t.Fatalf("the config shows the running raid: %s", st.body)
	}
}

func TestOnlyTheQueueReadersSeeAutomodEntries(t *testing.T) {
	f := newAutomodFixture(t)
	f.addRule(wordRule("No spam", "block", "spam"))
	wantStatus(t, "blocked", f.say(f.member, f.general, "spam"), http.StatusBadRequest)
	id := f.queue()[0].ID
	wantStatus(t, "member list", f.member.get("/api/admin/reports"), http.StatusForbidden)
	wantStatus(t, "member detail", f.member.get("/api/admin/reports/"+id.String()), http.StatusForbidden)
	wantStatus(t, "owner detail", f.admin.get("/api/admin/reports/"+id.String()), http.StatusOK)
}

func TestWebhookMessagesAreNotChecked(t *testing.T) {
	f := newAutomodFixture(t)
	f.addRule(wordRule("No spam", "block", "spam"))
	h := makeHook(t, f.admin, f.general, "ci")
	res := f.a.exec(h.path, map[string]any{"content": "spam from a build"}, nil)
	if res.status >= 300 {
		t.Fatalf("webhook: %d %s", res.status, res.body)
	}
	if len(f.queue()) != 0 {
		t.Fatal("a webhook is configured by the Owner and never hits a rule")
	}
}

func TestErasingAnAccountBlanksTheRefusedTextInItsAutomodEntries(t *testing.T) {
	f := newAutomodFixture(t)
	f.addRule(wordRule("No spam", "block", "spam"))
	wantStatus(t, "blocked", f.say(f.member, f.general, "spam from a person who will leave"), http.StatusBadRequest)
	id := f.queue()[0].ID
	if res := f.a.deleteAccount(f.member, memberPassword, ""); res.status != http.StatusNoContent {
		t.Fatalf("delete: %d %s", res.status, res.body)
	}
	res := f.mod.get("/api/admin/reports/" + id.String())
	wantStatus(t, "entry stays", res, http.StatusOK)
	if strings.Contains(string(res.body), "person who will leave") {
		t.Fatalf("the refused text must go with the account: %s", res.body)
	}
	if !strings.Contains(string(res.body), "No spam") {
		t.Fatal("the entry keeps the rule that hit")
	}
}
