//go:build integration

package server

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/communityevents"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Automod screens the text of an event like a message (doc/moderation.md,
// Automod): a comment and its edit, and the title, description and location a
// member with Manage events writes.
func TestABlockedWordIsRefusedAtEveryDoorOfAnEvent(t *testing.T) {
	f := newAutomodFixture(t)
	host := f.a.register(f.admin, "host")
	assignRoles(t, f.admin, host, createRole(t, f.admin, "hosts", perm.ManageEvents))
	f.age(f.member, host)
	ev := f.a.coverEventAt(f.admin, time.Now().Add(48*time.Hour), nil)
	evPath := "/api/events/" + ev.ID.String()
	comment := msgID(t, f.member.post(evPath+"/comments", map[string]string{"content": "clean words"}))
	f.addRule(wordRule("No spam", "block", "spam"))
	start := time.Now().Add(72 * time.Hour)
	newEvent := func(title, desc, loc string) response {
		return host.post("/api/events", map[string]any{"title": title, "description": desc, "start_time": start, "location": loc})
	}
	runCells(t, []doorCell{
		{"event comment", func() response {
			return f.member.post(evPath+"/comments", map[string]string{"content": "buy spam"})
		}, http.StatusBadRequest},
		{"event comment edit", func() response {
			return f.member.patch(evPath+"/comments/"+comment.String(), map[string]string{"content": "now spam"})
		}, http.StatusBadRequest},
		{"event title", func() response { return newEvent("spam night", "", "Hall") }, http.StatusBadRequest},
		{"event description", func() response { return newEvent("Game night", "bring spam", "Hall") }, http.StatusBadRequest},
		{"event location", func() response { return newEvent("Game night", "", "spam corner") }, http.StatusBadRequest},
		{"event title edit", func() response { return host.patch(evPath, map[string]any{"title": "spam"}) }, http.StatusBadRequest},
		{"event description edit", func() response {
			return host.patch(evPath, map[string]any{"description": "spam"})
		}, http.StatusBadRequest},
		{"event edit that leaves the words alone", func() response {
			return host.patch(evPath, map[string]any{"location": "Hall B"})
		}, http.StatusOK},
	})
	res := f.member.get(evPath + "/comments")
	var list struct{ Comments []struct{ Content string } }
	res.decode(t, &list)
	if len(list.Comments) != 1 || list.Comments[0].Content != "clean words" {
		t.Errorf("comments after the refusals: %s", res.body)
	}
}

// A comment gets what a message gets: the same refusal (status, code and
// text), the same members and channels exempt, and a flag and a rule's timeout
// applied the same way.
func TestAnEventCommentGetsTheRefusalExemptionsAndActionsOfAMessage(t *testing.T) {
	f := newAutomodFixture(t)
	f.age(f.member, f.mod)
	talk := f.a.createChannel(f.admin, "lounge", "voice")
	ev := f.a.coverEventAt(f.admin, time.Now().Add(48*time.Hour), nil)
	inTalk := f.a.talkEvent(f.admin, talk)
	comments := "/api/events/" + ev.ID.String() + "/comments"
	mine := f.a.send(f.member, f.general, "my clean words")
	comment := msgID(t, f.member.post(comments, map[string]string{"content": "clean words"}))
	block := wordRule("No spam", "block", "spam")
	block["exempt_roles"] = []uuid.UUID{f.modRole}
	block["exempt_channels"] = []uuid.UUID{talk}
	f.addRule(block)

	sameRefusal(t, "new", f.say(f.member, f.general, "spam"), f.member.post(comments, map[string]string{"content": "spam"}))
	sameRefusal(t, "edit",
		f.member.put("/api/channels/"+f.general.String()+"/messages/"+mine.String(), map[string]any{"content": "spam"}),
		f.member.patch(comments+"/"+comment.String(), map[string]string{"content": "spam"}))
	for name, c := range map[string]*client{"owner": f.admin, "administrator": f.delegate, "exempt role": f.mod} {
		wantStatus(t, name, c.post(comments, map[string]string{"content": "spam from " + name}), http.StatusCreated)
	}
	wantStatus(t, "exempt Talk", f.member.post("/api/events/"+inTalk.ID.String()+"/comments", map[string]string{"content": "spam"}), http.StatusCreated)

	flag := wordRule("Watch eggs", "flag", "eggs")
	flag["timeout_seconds"] = 60
	f.addRule(flag)
	quiet := f.a.register(f.admin, "quiet")
	f.age(quiet)
	wantStatus(t, "blocked comment", quiet.post(comments, map[string]string{"content": "spam"}), http.StatusBadRequest)
	wantStatus(t, "flagged comment", quiet.post(comments, map[string]string{"content": "eggs"}), http.StatusCreated)
	if until, err := f.timeoutOf(quiet); err != nil || until == nil {
		t.Fatalf("the flag rule's timeout was not applied: %v %v", until, err)
	}
	for rule, outcome := range map[string]string{"No spam": "blocked", "Watch eggs": "flagged"} {
		var where, content string
		var message *uuid.UUID
		err := f.a.db.QueryRow(context.Background(), `
			SELECT channel_name, message_id, snapshot->'message'->>'content' FROM message_reports
			WHERE automod_rule = $1 AND automod_outcome = $2 AND reported_user_id = $3`,
			rule, outcome, quiet.user.ID).Scan(&where, &message, &content)
		if err != nil || where != ev.Title || message != nil || content == "" {
			t.Errorf("queue entry of %q: place %q, message %v, text %q, err %v; want the event's title, the text and no message",
				rule, where, message, content, err)
		}
	}
}

// sameRefusal checks that two refusals carry the same status and error.
func sameRefusal(t *testing.T, what string, message, comment response) {
	t.Helper()
	if message.status != http.StatusBadRequest || comment.status != message.status || string(comment.body) != string(message.body) {
		t.Errorf("%s: comment %d %s, message %d %s", what, comment.status, comment.body, message.status, message.body)
	}
}

// talkEvent stores an event in a Talk.
func (a *app) talkEvent(admin *client, talk uuid.UUID) communityevents.CommunityEvent {
	a.t.Helper()
	ev, err := a.router.Events.Store.Create(context.Background(), communityevents.CreateEventRequest{
		Title: "In the lounge", StartTime: time.Now().Add(48 * time.Hour), ChannelID: &talk,
	}, admin.user.ID)
	if err != nil {
		a.t.Fatal(err)
	}
	return *ev
}
