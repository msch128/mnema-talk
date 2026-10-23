//go:build integration

package server

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

func (f *messageModerationFixture) reply(c *client, root uuid.UUID, content string) uuid.UUID {
	f.a.t.Helper()
	res := c.post(f.path("/messages"), map[string]any{"content": content, "parent_id": root})
	if res.status != http.StatusCreated {
		f.a.t.Fatalf("reply: %d %s", res.status, res.body)
	}
	var m struct{ ID uuid.UUID }
	res.decode(f.a.t, &m)
	return m.ID
}

type followState struct {
	ThreadID  uuid.UUID `json:"thread_id"`
	Following bool      `json:"following"`
}

func following(t *testing.T, c *client, root uuid.UUID) bool {
	t.Helper()
	var th struct {
		Following bool `json:"following"`
	}
	res := c.get("/api/messages/" + root.String() + "/thread")
	if res.status != http.StatusOK {
		t.Fatalf("thread: %d %s", res.status, res.body)
	}
	res.decode(t, &th)
	return th.Following
}

func TestRepliesFollowTheThreadForTheRootsAuthorTheReplierAndTheMentioned(t *testing.T) {
	f := newMessageModerationFixture(t, false)
	root := f.a.send(f.user, f.channel, "question")
	if following(t, f.user, root) || following(t, f.peer, root) {
		t.Fatal("nobody follows a thread without replies")
	}
	f.reply(f.peer, root, "hey @msgmod, any idea?")
	for name, c := range map[string]*client{"the root's author": f.user, "the replier": f.peer, "the mentioned": f.mod} {
		if !following(t, c, root) {
			t.Errorf("%s does not follow", name)
		}
	}
	if following(t, f.admin, root) {
		t.Error("an uninvolved member follows")
	}
}

func TestFollowAndUnfollowAreKeptAcrossLaterReplies(t *testing.T) {
	f := newMessageModerationFixture(t, false)
	root := f.a.send(f.user, f.channel, "question")
	url := "/api/threads/" + root.String() + "/follow"

	var st followState
	res := f.admin.put(url, nil)
	res.decode(t, &st)
	if res.status != http.StatusOK || !st.Following || st.ThreadID != root || !following(t, f.admin, root) {
		t.Fatalf("follow: %d %s", res.status, res.body)
	}
	if res := f.admin.put(url, nil); res.status != http.StatusOK {
		t.Errorf("following twice: %d", res.status)
	}

	if res := f.user.delete(url); res.status != http.StatusOK {
		t.Fatalf("unfollow: %d %s", res.status, res.body)
	}
	f.reply(f.peer, root, "an answer")
	f.reply(f.user, root, "thanks")
	if following(t, f.user, root) {
		t.Error("a later reply undid the explicit unfollow")
	}
	if !following(t, f.peer, root) {
		t.Error("the other replier still follows")
	}

	var list struct {
		ThreadIDs []uuid.UUID `json:"thread_ids"`
	}
	f.admin.get("/api/threads/following").decode(t, &list)
	if len(list.ThreadIDs) != 1 || list.ThreadIDs[0] != root {
		t.Errorf("followed threads: %v", list.ThreadIDs)
	}
	f.user.get("/api/threads/following").decode(t, &list)
	if len(list.ThreadIDs) != 0 {
		t.Errorf("an unfollowed thread is listed: %v", list.ThreadIDs)
	}
}

func TestOnlyARootMessageInAVisibleChannelCanBeFollowed(t *testing.T) {
	f := newMessageModerationFixture(t, false)
	root := f.a.send(f.user, f.channel, "question")
	reply := f.reply(f.peer, root, "answer")
	if res := f.peer.put("/api/threads/"+reply.String()+"/follow", nil); res.status != http.StatusNotFound {
		t.Errorf("a reply has no thread: %d", res.status)
	}
	if res := f.peer.put("/api/threads/"+uuid.NewString()+"/follow", nil); res.status != http.StatusNotFound {
		t.Errorf("unknown message: %d", res.status)
	}
	if res := f.peer.put("/api/threads/nope/follow", nil); res.status != http.StatusBadRequest {
		t.Errorf("bad id: %d", res.status)
	}

	everyone := everyoneID(t, f.admin)
	if res := putOverwrites(f.admin, "channels", f.channel, overwrite("role", everyone, 0, perm.ViewChannel)); res.status != http.StatusNoContent {
		t.Fatalf("hide: %d", res.status)
	}
	if res := f.peer.put("/api/threads/"+root.String()+"/follow", nil); res.status != http.StatusNotFound {
		t.Errorf("a hidden channel's thread: %d", res.status)
	}
	var list struct {
		ThreadIDs []uuid.UUID `json:"thread_ids"`
	}
	f.peer.get("/api/threads/following").decode(t, &list)
	if len(list.ThreadIDs) != 0 {
		t.Errorf("a thread of a hidden channel is listed: %v", list.ThreadIDs)
	}
}

func TestSearchFiltersPinnedPollAndEdited(t *testing.T) {
	f := newMessageModerationFixture(t, false)
	plain := f.a.send(f.user, f.channel, "plain lemon")
	pinned := f.a.send(f.user, f.channel, "pinned lemon")
	edited := f.a.send(f.user, f.channel, "edited lemon")
	f.edit(f.user, edited, "edited lemon again")
	if res := f.mod.put(f.path("/messages/"+pinned.String()+"/pin"), nil); res.status != http.StatusOK {
		t.Fatalf("pin: %d %s", res.status, res.body)
	}
	pollRes := f.user.post(f.path("/polls"), map[string]any{"question": "lemon or lime?", "options": []string{"lemon", "lime"}})
	if pollRes.status != http.StatusCreated {
		t.Fatalf("poll: %d %s", pollRes.status, pollRes.body)
	}
	var poll struct{ ID uuid.UUID }
	pollRes.decode(t, &poll)

	ids := func(query string) map[uuid.UUID]bool {
		var out struct{ Messages []struct{ ID uuid.UUID } }
		res := f.peer.get("/api/search?" + query)
		if res.status != http.StatusOK {
			t.Fatalf("%s: %d %s", query, res.status, res.body)
		}
		res.decode(t, &out)
		got := map[uuid.UUID]bool{}
		for _, m := range out.Messages {
			got[m.ID] = true
		}
		return got
	}
	if got := ids("is=pinned"); len(got) != 1 || !got[pinned] {
		t.Errorf("is=pinned: %v", got)
	}
	if got := ids("has=poll"); len(got) != 1 || !got[poll.ID] {
		t.Errorf("has=poll: %v", got)
	}
	if got := ids("has=edit"); len(got) != 1 || !got[edited] {
		t.Errorf("has=edit: %v", got)
	}
	if got := ids("q=lemon&is=pinned"); len(got) != 1 || !got[pinned] {
		t.Errorf("text with is=pinned: %v", got)
	}
	if got := ids("q=lemon"); !got[plain] || len(got) < 4 {
		t.Errorf("unfiltered: %v", got)
	}
	for _, bad := range []string{"is=bogus", "has=bogus"} {
		if res := f.peer.get("/api/search?" + bad); res.status != http.StatusBadRequest {
			t.Errorf("%s: %d", bad, res.status)
		}
	}
}

func TestOpenSessionsLearnAboutTheThreadsAReplyMakesThemFollow(t *testing.T) {
	f := newMessageModerationFixture(t, true)
	root := f.a.send(f.user, f.channel, "question")
	author, _, err := f.user.dialWS(f.a.origin())
	if err != nil {
		t.Fatal(err)
	}
	bystander, _, err := f.admin.dialWS(f.a.origin())
	if err != nil {
		t.Fatal(err)
	}
	f.reply(f.peer, root, "an answer for @msgmod")

	raw, ok := author.expect("thread_follow_updated", 2*time.Second)
	if !ok || !strings.Contains(string(raw), root.String()) || !strings.Contains(string(raw), `"following":true`) {
		t.Fatalf("the root's author is not told: %s", raw)
	}
	if raw, ok := bystander.expect("thread_follow_updated", 300*time.Millisecond); ok {
		t.Errorf("an uninvolved member was told: %s", raw)
	}
}
