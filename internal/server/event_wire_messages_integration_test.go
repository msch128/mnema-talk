//go:build integration

package server

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

func TestWireFormatOfMessageEvents(t *testing.T) {
	w := newWireWorld(t, "event_wire_messages")
	w.step("message", w.alice.post(w.msgPath(w.ch, ""), map[string]any{"content": "hello **world**"}))
	root := w.say(w.alice, w.ch, map[string]any{"content": "a thread starts here"})
	w.step("thread reply", w.bob.post(w.msgPath(w.ch, ""), map[string]any{"content": "a reply", "parent_id": root}))
	w.step("edit", w.alice.put(w.msgPath(w.ch, "/"+root.String()), map[string]any{"content": "a thread starts here, edited"}))
	w.step("reaction", w.bob.post("/api/messages/"+root.String()+"/reactions", map[string]string{"emoji": "🔥"}))
	w.step("pin", w.admin.put(w.msgPath(w.ch, "/"+root.String()+"/pin"), nil))
	w.step("unpin", w.admin.delete(w.msgPath(w.ch, "/"+root.String()+"/pin")))
	w.step("follow a thread", w.alice.put("/api/threads/"+root.String()+"/follow", nil))
	w.step("unfollow a thread", w.alice.delete("/api/threads/"+root.String()+"/follow"))
	w.step("forward", w.bob.post("/api/messages/"+root.String()+"/forward", map[string]any{"channel_id": w.ch}))
	w.step("save", w.bob.put("/api/me/saved/"+root.String(), nil))
	w.step("unsave", w.bob.delete("/api/me/saved/"+root.String()))
	w.readStateSteps(root)
	w.deleteSteps()
}

func (w *wireWorld) readStateSteps(msg uuid.UUID) {
	w.t.Helper()
	base := "/api/channels/" + w.ch.String()
	w.step("mark read", w.bob.post(base+"/read", map[string]any{"message_id": msg}))
	w.say(w.alice, w.ch, map[string]any{"content": "something new for bob"})
	w.step("own message after unread ones of others", w.bob.post(w.msgPath(w.ch, ""), map[string]any{"content": "I answer"}))
	w.step("mark unread", w.bob.post(base+"/unread", map[string]any{"message_id": msg}))
	w.step("mark everything read", w.bob.post("/api/read-state/read-all", nil))
	w.step("channel notification level", w.bob.put(base+"/notifications", map[string]any{"level": "mentions"}))
	w.step("server notification level", w.bob.put("/api/notification-settings/server", map[string]any{"level": "all", "suppress_everyone": true}))
}

func (w *wireWorld) deleteSteps() {
	w.t.Helper()
	own := w.say(w.alice, w.ch, map[string]any{"content": "oops"})
	w.step("author deletes", w.alice.delete(w.msgPath(w.ch, "/"+own.String())))
	bad := w.say(w.alice, w.ch, map[string]any{"content": "rude"})
	w.step("moderator deletes with a reason", w.admin.do(http.MethodDelete, w.msgPath(w.ch, "/"+bad.String()), map[string]any{"reason": "be kind"}, nil))
	w.say(w.alice, w.ch, map[string]any{"content": "spam 1"})
	w.say(w.alice, w.ch, map[string]any{"content": "spam 2"})
	w.step("bulk delete", w.admin.post(w.msgPath(w.ch, "/bulk-delete"), map[string]any{"user_id": w.alice.user.ID, "count": 2, "reason": "spam"}))
}

func TestWireFormatOfPollAndForumEvents(t *testing.T) {
	w := newWireWorld(t, "event_wire_polls_forum")
	base := "/api/channels/" + w.ch.String()
	res := w.alice.post(base+"/polls", map[string]any{"question": "Pizza or pasta?", "options": []string{"Pizza", "Pasta"}})
	var poll struct {
		ID   uuid.UUID
		Poll struct{ Options []struct{ ID string } }
	}
	res.decode(t, &poll)
	w.step("poll", res)
	pollPath := w.msgPath(w.ch, "/"+poll.ID.String()+"/poll")
	w.step("vote", w.bob.put(pollPath+"/votes", map[string]any{"option_ids": []string{poll.Poll.Options[0].ID}}))
	w.step("poll ends", w.alice.post(pollPath+"/end", nil))

	forum := w.a.createChannel(w.admin, "ideas", "forum")
	w.a.events.Take()
	fb := "/api/channels/" + forum.String()
	tagRes := w.admin.post(fb+"/tags", map[string]any{"name": "bug"})
	var tag struct{ ID uuid.UUID }
	tagRes.decode(t, &tag)
	w.step("forum tag created", tagRes)
	w.step("forum tag renamed", w.admin.patch(fb+"/tags/"+tag.ID.String(), map[string]any{"name": "defect"}))
	postRes := w.alice.post(fb+"/posts", map[string]any{"title": "An idea", "content": "body", "tag_ids": []uuid.UUID{tag.ID}})
	var post struct{ ID uuid.UUID }
	postRes.decode(t, &post)
	w.step("forum post", postRes)
	w.step("forum post edited", w.alice.patch(fb+"/posts/"+post.ID.String(), map[string]any{"title": "A better idea"}))
	w.step("forum tag deleted", w.admin.delete(fb+"/tags/"+tag.ID.String()))
}

func TestWireFormatOfAutomationEvents(t *testing.T) {
	w := newWireWorld(t, "event_wire_automation")
	hook := makeHook(t, w.admin, w.ch, "ci")
	w.step("incoming webhook", w.a.exec(hook.path, map[string]any{"content": "build passed"}, nil))
	w.step("slash command /shrug", w.alice.post("/api/channels/"+w.ch.String()+"/interactions", slashCall("shrug", map[string]any{"text": "well"})))
	w.step("slash command /nick", w.alice.post("/api/channels/"+w.ch.String()+"/interactions", slashCall("nick", map[string]any{"name": "Ally"})))
	w.laterSteps()
	w.autoDeleteStep()
}

func (w *wireWorld) laterSteps() {
	w.t.Helper()
	f := &laterFixture{a: w.a, push: &recordingPush{}}
	w.a.router.Later.Push = f.push
	msg := w.say(w.bob, w.ch, map[string]any{"content": "remember this"})
	in := func(d time.Duration) time.Time { return time.Now().Add(d) }
	res := w.alice.post("/api/me/reminders", map[string]any{"message_id": msg, "remind_at": in(time.Hour)})
	var reminder struct{ ID uuid.UUID }
	res.decode(w.t, &reminder)
	w.step("reminder created", res)
	w.step("reminder removed", w.alice.delete("/api/me/reminders/"+reminder.ID.String()))
	w.quiet(w.alice.post("/api/me/reminders", map[string]any{"message_id": msg, "remind_at": in(time.Hour)}))
	f.sweep(w.t)
	w.recorded("reminder due", 1)

	res = w.alice.post("/api/channels/"+w.ch.String()+"/scheduled", map[string]any{"content": "later", "send_at": in(time.Hour)})
	var sched struct{ ID uuid.UUID }
	res.decode(w.t, &sched)
	w.step("scheduled message created", res)
	w.step("scheduled message changed", w.alice.patch("/api/me/scheduled/"+sched.ID.String(), map[string]any{"content": "much later"}))
	w.step("scheduled message deleted", w.alice.delete("/api/me/scheduled/"+sched.ID.String()))
	w.quiet(w.alice.post("/api/channels/"+w.ch.String()+"/scheduled", map[string]any{"content": "sent by the sweep", "send_at": in(time.Hour)}))
	f.sweep(w.t)
	w.recorded("scheduled message sent", 1)
	w.quiet(w.alice.post("/api/channels/"+w.ch.String()+"/scheduled", map[string]any{"content": "never sent", "send_at": in(time.Hour)}))
	w.quiet(putOverwrites(w.admin, "channels", w.ch, overwrite("member", w.alice.user.ID, 0, perm.SendMessages)))
	f.sweep(w.t)
	w.recorded("scheduled message refused", 1)
}

func (w *wireWorld) autoDeleteStep() {
	w.t.Helper()
	f := autoDeleteFixture{pollFixture{a: w.a}}
	old := w.say(w.bob, w.ch, map[string]any{"content": "old news"})
	w.quiet(f.set(w.admin, w.ch, 1))
	f.age(w.t, old, 3)
	f.sweep(w.t)
	w.recorded("auto-delete sweep", 1)
}
