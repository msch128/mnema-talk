//go:build integration

package server

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

type messageModerationFixture struct {
	a                *app
	admin, mod, user *client
	peer             *client
	channel          uuid.UUID
	modRole          uuid.UUID
}

func newMessageModerationFixture(t *testing.T, withHub bool) *messageModerationFixture {
	a := newApp(t, withHub)
	admin := a.seedAdmin()
	f := &messageModerationFixture{a: a, admin: admin}
	f.mod = a.register(admin, "msgmod")
	f.user = a.register(admin, "talker")
	f.peer = a.register(admin, "peerx")
	f.modRole = createRole(t, admin, "message mods", perm.ManageMessages)
	assignRoles(t, admin, f.mod, f.modRole)
	f.channel = a.createChannel(admin, "general", "text")
	return f
}

func (f *messageModerationFixture) path(suffix string) string {
	return "/api/channels/" + f.channel.String() + suffix
}

func (f *messageModerationFixture) setSlowMode(t *testing.T, seconds int) {
	t.Helper()
	res := f.admin.patch("/api/admin/channels/"+f.channel.String(), map[string]any{"slow_mode_seconds": seconds})
	if res.status != http.StatusOK {
		t.Fatalf("slow mode: %d %s", res.status, res.body)
	}
}

func TestSlowModeHoldsBackMembersButNotModerators(t *testing.T) {
	f := newMessageModerationFixture(t, false)
	var ch struct {
		SlowModeSeconds int `json:"slow_mode_seconds"`
	}
	res := f.admin.patch("/api/admin/channels/"+f.channel.String(), map[string]any{"slow_mode_seconds": 2})
	res.decode(t, &ch)
	if ch.SlowModeSeconds != 2 {
		t.Fatalf("channel %+v", ch)
	}
	for _, bad := range []int{-1, 21601} {
		if r := f.admin.patch("/api/admin/channels/"+f.channel.String(), map[string]any{"slow_mode_seconds": bad}); r.status != http.StatusBadRequest {
			t.Errorf("slow mode %d: %d", bad, r.status)
		}
	}

	post := f.path("/messages")
	first := f.user.post(post, map[string]any{"content": "one"})
	if first.status != http.StatusCreated {
		t.Fatalf("first: %d %s", first.status, first.body)
	}
	again := f.user.post(post, map[string]any{"content": "two"})
	if again.status != http.StatusTooManyRequests {
		t.Fatalf("second: %d %s", again.status, again.body)
	}
	if wait, _ := strconv.Atoi(again.header.Get("Retry-After")); wait < 1 || wait > 2 {
		t.Errorf("Retry-After %q", again.header.Get("Retry-After"))
	}
	if code := again.code(t); code != "RATE_LIMITED" || !strings.Contains(string(again.body), "slow mode") {
		t.Errorf("error %s", again.body)
	}
	// Replies and uploads count, others and other channels do not.
	var posted struct{ ID uuid.UUID }
	first.decode(t, &posted)
	if r := f.user.post(post, map[string]any{"content": "reply", "parent_id": posted.ID}); r.status != http.StatusTooManyRequests {
		t.Errorf("reply: %d", r.status)
	}
	if r := f.user.upload(f.path("/upload"), "file", "a.png", "image/png", pngBytes, nil); r.status != http.StatusTooManyRequests {
		t.Errorf("upload: %d", r.status)
	}
	if r := f.peer.post(post, map[string]any{"content": "me too"}); r.status != http.StatusCreated {
		t.Errorf("another member: %d", r.status)
	}
	other := f.a.createChannel(f.admin, "elsewhere", "text")
	if r := f.user.post("/api/channels/"+other.String()+"/messages", map[string]any{"content": "x"}); r.status != http.StatusCreated {
		t.Errorf("another channel: %d", r.status)
	}
	// Moderators (Manage messages) and the Owner are exempt.
	for name, c := range map[string]*client{"moderator": f.mod, "owner": f.admin} {
		for i := 0; i < 3; i++ {
			if r := c.post(post, map[string]any{"content": name}); r.status != http.StatusCreated {
				t.Errorf("%s post %d: %d", name, i, r.status)
			}
		}
	}

	time.Sleep(2100 * time.Millisecond)
	if r := f.user.post(post, map[string]any{"content": "later"}); r.status != http.StatusCreated {
		t.Errorf("after the wait: %d %s", r.status, r.body)
	}
	f.setSlowMode(t, 0)
	for i := 0; i < 2; i++ {
		if r := f.user.post(post, map[string]any{"content": "free"}); r.status != http.StatusCreated {
			t.Errorf("slow mode off: %d", r.status)
		}
	}
}

func TestSlowModeAllowsOnlyOneOfManySimultaneousPosts(t *testing.T) {
	f := newMessageModerationFixture(t, false)
	f.setSlowMode(t, 30)
	var wg sync.WaitGroup
	var mu sync.Mutex
	created := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if r := f.user.post(f.path("/messages"), map[string]any{"content": "burst"}); r.status == http.StatusCreated {
				mu.Lock()
				created++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if created != 1 {
		t.Fatalf("%d of 8 simultaneous posts went through, want 1", created)
	}
}

// Deleting the last message does not shorten the wait: the time of the last
// post is kept apart from the messages.
func TestSlowModeSurvivesDeletingTheLastMessage(t *testing.T) {
	f := newMessageModerationFixture(t, false)
	f.setSlowMode(t, 30)
	id := f.a.send(f.user, f.channel, "first")
	if r := f.user.delete(f.path("/messages/" + id.String())); r.status != http.StatusNoContent {
		t.Fatal(r.status)
	}
	again := f.user.post(f.path("/messages"), map[string]any{"content": "second"})
	if again.status != http.StatusTooManyRequests {
		t.Fatalf("post after deleting the last message: %d %s", again.status, again.body)
	}
	if wait, _ := strconv.Atoi(again.header.Get("Retry-After")); wait < 20 || wait > 30 {
		t.Errorf("Retry-After %q", again.header.Get("Retry-After"))
	}
	// A moderator deleting it is no way out either.
	other := f.a.send(f.peer, f.channel, "peer first")
	if r := f.mod.delete(f.path("/messages/" + other.String())); r.status != http.StatusNoContent {
		t.Fatal(r.status)
	}
	if r := f.peer.post(f.path("/messages"), map[string]any{"content": "peer second"}); r.status != http.StatusTooManyRequests {
		t.Errorf("after a moderator deleted the message: %d", r.status)
	}
	// Turning slow mode off lets everyone through again.
	f.setSlowMode(t, 0)
	if r := f.user.post(f.path("/messages"), map[string]any{"content": "free"}); r.status != http.StatusCreated {
		t.Errorf("slow mode off: %d", r.status)
	}
}

func TestPinAndUnpinNeedManageMessages(t *testing.T) {
	f := newMessageModerationFixture(t, false)
	id := f.a.send(f.user, f.channel, "pin me")
	base := f.path("/messages/" + id.String() + "/pin")
	if r := f.user.do(http.MethodPut, base, nil, nil); r.status != http.StatusForbidden {
		t.Errorf("author pins: %d", r.status)
	}
	var msg struct {
		IsPinned bool `json:"is_pinned"`
	}
	r := f.mod.do(http.MethodPut, base, nil, nil)
	if r.status != http.StatusOK {
		t.Fatalf("pin: %d %s", r.status, r.body)
	}
	r.decode(t, &msg)
	if !msg.IsPinned {
		t.Error("not pinned")
	}
	r = f.mod.do(http.MethodDelete, base, nil, nil)
	r.decode(t, &msg)
	if r.status != http.StatusOK || msg.IsPinned {
		t.Errorf("unpin: %d %+v", r.status, msg)
	}
	other := f.a.createChannel(f.admin, "other", "text")
	if r := f.mod.do(http.MethodPut, "/api/channels/"+other.String()+"/messages/"+id.String()+"/pin", nil, nil); r.status != http.StatusNotFound {
		t.Errorf("wrong channel: %d", r.status)
	}
	if r := f.mod.do(http.MethodPut, f.path("/messages/"+uuid.NewString()+"/pin"), nil, nil); r.status != http.StatusNotFound {
		t.Errorf("unknown message: %d", r.status)
	}
	if r := f.mod.do(http.MethodPut, "/api/channels/"+uuid.NewString()+"/messages/"+id.String()+"/pin", nil, nil); r.status < 400 {
		t.Errorf("unknown channel: %d", r.status)
	}
	if r := f.mod.do(http.MethodPut, f.path("/messages/nope/pin"), nil, nil); r.status != http.StatusBadRequest {
		t.Errorf("bad message id: %d", r.status)
	}
	if r := f.mod.do(http.MethodPut, "/api/channels/nope/messages/"+id.String()+"/pin", nil, nil); r.status != http.StatusBadRequest {
		t.Errorf("bad channel id: %d", r.status)
	}
	seen := map[string]bool{}
	for _, e := range auditEntries(t, f.admin, "action=message&limit=50").Entries {
		seen[e.Action] = e.ActorName == "msgmod" && e.TargetID == id.String()
	}
	if !seen["message.pin"] || !seen["message.unpin"] {
		t.Errorf("audit %v", seen)
	}
}

func TestModeratorDeleteKeepsTheReasonAndTellsTheAuthor(t *testing.T) {
	f := newMessageModerationFixture(t, true)
	id := f.a.send(f.user, f.channel, "rude words")
	conn, _, err := f.user.dialWS(f.a.origin())
	if err != nil {
		t.Fatal(err)
	}
	url := f.path("/messages/" + id.String())
	if r := f.mod.do(http.MethodDelete, url, strings.NewReader(`{"reason":"`+strings.Repeat("x", 201)+`"}`), map[string]string{"Content-Type": "application/json"}); r.status != http.StatusBadRequest {
		t.Fatalf("long reason: %d", r.status)
	}
	if r := f.mod.do(http.MethodDelete, url, strings.NewReader(`{"reason":"insult","other":1}`), map[string]string{"Content-Type": "application/json"}); r.status != http.StatusBadRequest {
		t.Fatalf("unknown field: %d", r.status)
	}
	if r := f.mod.do(http.MethodDelete, url, strings.NewReader(`{"reason":"insult"}`), map[string]string{"Content-Type": "application/json"}); r.status != http.StatusNoContent {
		t.Fatalf("delete: %d", r.status)
	}
	raw, ok := conn.expect("moderation_notice", 2*time.Second)
	if !ok || !strings.Contains(string(raw), "insult") || !strings.Contains(string(raw), "message_deleted") || !strings.Contains(string(raw), "general") {
		t.Fatalf("author not told: %s", raw)
	}
	var found bool
	for _, e := range auditEntries(t, f.admin, "action=message.delete").Entries {
		if e.TargetID == id.String() {
			found = e.Details["reason"] == "insult" && e.Details["moderator"] == true && e.Details["content"] == "rude words"
		}
	}
	if !found {
		t.Error("audit entry lacks reason, moderator flag or text")
	}

	// A plain delete needs no body, and a self-delete tells nobody.
	own := f.a.send(f.user, f.channel, "mine")
	if r := f.user.delete(f.path("/messages/" + own.String())); r.status != http.StatusNoContent {
		t.Fatalf("self delete: %d", r.status)
	}
	if raw, ok := conn.expect("moderation_notice", 300*time.Millisecond); ok {
		t.Errorf("self delete sent a notice: %s", raw)
	}
}

func TestBulkDeleteRemovesTheLatestMessagesOfOneMember(t *testing.T) {
	f := newMessageModerationFixture(t, true)
	var ids []uuid.UUID
	for i := 0; i < 5; i++ {
		ids = append(ids, f.a.send(f.user, f.channel, "spam "+strconv.Itoa(i)))
		time.Sleep(5 * time.Millisecond)
	}
	keep := f.a.send(f.peer, f.channel, "innocent")
	// An attachment and a thread reply go through the same bulk delete.
	up := f.user.upload(f.path("/upload"), "file", "evidence.png", "image/png", pngBytes, map[string]string{"content": "with file"})
	if up.status != http.StatusCreated {
		t.Fatalf("upload: %d %s", up.status, up.body)
	}
	var uploaded struct{ ID uuid.UUID }
	up.decode(t, &uploaded)
	replyRes := f.user.post(f.path("/messages"), map[string]any{"content": "a reply", "parent_id": keep})
	if replyRes.status != http.StatusCreated {
		t.Fatalf("reply: %d %s", replyRes.status, replyRes.body)
	}
	var replied struct{ ID uuid.UUID }
	replyRes.decode(t, &replied)
	conn, _, err := f.user.dialWS(f.a.origin())
	if err != nil {
		t.Fatal(err)
	}
	url := f.path("/messages/bulk-delete")
	body := func(user uuid.UUID, count int, reason string) map[string]any {
		return map[string]any{"user_id": user, "count": count, "reason": reason}
	}

	if r := f.user.post(url, body(f.user.user.ID, 2, "")); r.status != http.StatusForbidden {
		t.Errorf("without the permission: %d", r.status)
	}
	for name, count := range map[string]int{"zero": 0, "too many": 101} {
		if r := f.mod.post(url, body(f.user.user.ID, count, "")); r.status != http.StatusBadRequest {
			t.Errorf("%s: %d", name, r.status)
		}
	}
	if r := f.mod.post(url, body(f.user.user.ID, 2, strings.Repeat("x", 201))); r.status != http.StatusBadRequest {
		t.Errorf("long reason: %d", r.status)
	}
	if r := f.mod.post(url, body(uuid.New(), 2, "")); r.status != http.StatusNotFound {
		t.Errorf("unknown member: %d", r.status)
	}
	if r := f.mod.post(url, map[string]any{"user_id": "nope", "count": 1}); r.status != http.StatusBadRequest {
		t.Errorf("bad member: %d", r.status)
	}
	if r := f.mod.post("/api/channels/nope/messages/bulk-delete", body(f.user.user.ID, 1, "")); r.status != http.StatusBadRequest {
		t.Errorf("bad channel: %d", r.status)
	}

	// Count 2 takes the reply and the upload, the two latest.
	res := f.mod.post(url, body(f.user.user.ID, 2, "attachments"))
	if res.status != http.StatusOK {
		t.Fatalf("bulk delete of attachments: %d %s", res.status, res.body)
	}
	var first struct{ Deleted int }
	res.decode(t, &first)
	if first.Deleted != 2 {
		t.Fatalf("deleted %d, want 2", first.Deleted)
	}
	if raw, ok := conn.expect("moderation_notice", 2*time.Second); !ok || !strings.Contains(string(raw), "attachments") {
		t.Errorf("no notice for the first bulk delete: %s", raw)
	}
	attachmentEntry, replyEntry := false, false
	for _, e := range auditEntries(t, f.admin, "action=message.delete&limit=50").Entries {
		if e.TargetID == uploaded.ID.String() {
			list, _ := e.Details["attachments"].([]any)
			attachmentEntry = len(list) == 1 && strings.Contains(string(mustJSON(list[0])), "evidence.png")
		}
		if e.TargetID == replied.ID.String() {
			replyEntry = e.Details["parent_id"] == keep.String()
		}
	}
	if !attachmentEntry || !replyEntry {
		t.Errorf("audit lacks the attachment (%v) or the reply's parent (%v)", attachmentEntry, replyEntry)
	}

	res = f.mod.post(url, body(f.user.user.ID, 3, "flooding"))
	if res.status != http.StatusOK {
		t.Fatalf("bulk delete: %d %s", res.status, res.body)
	}
	var out struct{ Deleted int }
	res.decode(t, &out)
	if out.Deleted != 3 {
		t.Fatalf("deleted %d", out.Deleted)
	}
	var listed []struct {
		ID uuid.UUID `json:"id"`
	}
	f.admin.get(f.path("/messages?limit=50")).decode(t, &listed)
	left := map[uuid.UUID]bool{}
	for _, m := range listed {
		left[m.ID] = true
	}
	if left[ids[4]] || left[ids[3]] || left[ids[2]] || !left[ids[1]] || !left[ids[0]] || !left[keep] {
		t.Errorf("wrong messages left: %v", left)
	}
	if raw, ok := conn.expect("moderation_notice", 2*time.Second); !ok || !strings.Contains(string(raw), "messages_deleted") || !strings.Contains(string(raw), "flooding") {
		t.Errorf("author not told: %s", raw)
	}

	deleted, summary := 0, false
	for _, e := range auditEntries(t, f.admin, "limit=100").Entries {
		switch e.Action {
		case "message.delete":
			if e.Details["bulk"] == true && e.Details["reason"] == "flooding" && strings.HasPrefix(e.Details["content"].(string), "spam ") && e.ActorName == "msgmod" {
				deleted++
			}
		case "moderation.bulk_delete":
			summary = summary || (e.TargetName == "talker" && e.Details["deleted"] == float64(3) && e.Details["reason"] == "flooding")
		}
	}
	if deleted != 3 || !summary {
		t.Errorf("audit: %d message entries, summary %v", deleted, summary)
	}
}

func TestBulkDeleteRespectsHierarchyAndTheOwner(t *testing.T) {
	f := newMessageModerationFixture(t, false)
	url := f.path("/messages/bulk-delete")
	ownerMsg := f.a.send(f.admin, f.channel, "owner words")
	if r := f.mod.post(url, map[string]any{"user_id": f.admin.user.ID, "count": 5}); r.status != http.StatusForbidden {
		t.Errorf("the Owner's messages: %d", r.status)
	}
	// A peer of equal rank is out of reach.
	peerMod := f.a.register(f.admin, "peermod")
	assignRoles(t, f.admin, peerMod, f.modRole)
	f.a.send(peerMod, f.channel, "peer words")
	if r := f.mod.post(url, map[string]any{"user_id": peerMod.user.ID, "count": 5}); r.status != http.StatusForbidden {
		t.Errorf("equal rank: %d", r.status)
	}
	// A thread reply of the Owner protects its parent from a moderator.
	parent := f.a.send(f.user, f.channel, "thread root")
	reply := f.admin.post(f.path("/messages"), map[string]any{"content": "owner reply", "parent_id": parent})
	if reply.status != http.StatusCreated {
		t.Fatal(reply.status, string(reply.body))
	}
	res := f.mod.post(url, map[string]any{"user_id": f.user.user.ID, "count": 5})
	var out struct{ Deleted int }
	res.decode(t, &out)
	if res.status != http.StatusOK || out.Deleted != 0 {
		t.Errorf("moderator removed a thread holding an Owner reply: %d %d", res.status, out.Deleted)
	}
	res = f.admin.post(url, map[string]any{"user_id": f.user.user.ID, "count": 5})
	res.decode(t, &out)
	if res.status != http.StatusOK || out.Deleted != 1 {
		t.Errorf("Owner bulk delete: %d %d", res.status, out.Deleted)
	}
	_ = ownerMsg
	// Nobody outside the channel's audience can moderate it.
	secret := f.a.createChannel(f.admin, "secret", "text")
	if r := putOverwrites(f.admin, "channels", secret, overwrite("role", everyoneID(t, f.admin), 0, perm.ViewChannel)); r.status != http.StatusNoContent {
		t.Fatal(r.status)
	}
	if r := f.mod.post("/api/channels/"+secret.String()+"/messages/bulk-delete", map[string]any{"user_id": f.user.user.ID, "count": 1}); r.status < 400 {
		t.Errorf("hidden channel: %d", r.status)
	}
}
