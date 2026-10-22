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
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/perm"
)

type editVersions struct {
	Edits []struct {
		Content  string    `json:"content"`
		EditorID uuid.UUID `json:"editor_id"`
	} `json:"edits"`
}

func (f *messageModerationFixture) edit(c *client, msg uuid.UUID, content string) {
	f.a.t.Helper()
	res := c.put(f.path("/messages/"+msg.String()), map[string]any{"content": content})
	if res.status != http.StatusOK {
		f.a.t.Fatalf("edit: %d %s", res.status, res.body)
	}
}

func (f *messageModerationFixture) history(c *client, msg uuid.UUID) response {
	return c.get(f.path("/messages/" + msg.String() + "/edits"))
}

func TestModeratorsReadEarlierVersionsNewestFirst(t *testing.T) {
	f := newMessageModerationFixture(t, false)
	msg := f.a.send(f.user, f.channel, "first draft")
	f.edit(f.user, msg, "second draft")
	f.edit(f.user, msg, "final text")

	res := f.history(f.mod, msg)
	if res.status != http.StatusOK {
		t.Fatalf("history: %d %s", res.status, res.body)
	}
	var got editVersions
	res.decode(t, &got)
	if len(got.Edits) != 2 || got.Edits[0].Content != "second draft" || got.Edits[1].Content != "first draft" {
		t.Fatalf("versions: %+v", got)
	}
	if got.Edits[0].EditorID != f.user.user.ID {
		t.Errorf("the editor is the author: %+v", got.Edits[0])
	}
	if own := f.history(f.admin, msg); own.status != http.StatusOK {
		t.Errorf("the Owner reads it: %d", own.status)
	}

	untouched := f.a.send(f.user, f.channel, "never edited")
	var none editVersions
	f.history(f.mod, untouched).decode(t, &none)
	if none.Edits == nil || len(none.Edits) != 0 {
		t.Errorf("an unedited message has an empty list: %+v", none)
	}
}

func TestEditHistoryIsForModeratorsOnly(t *testing.T) {
	f := newMessageModerationFixture(t, false)
	msg := f.a.send(f.user, f.channel, "before")
	f.edit(f.user, msg, "after")

	for name, c := range map[string]*client{"the author": f.user, "another member": f.peer} {
		if res := f.history(c, msg); res.status != http.StatusForbidden {
			t.Errorf("%s: %d %s", name, res.status, res.body)
		}
	}
	// The channel is not what the path says: no history through another channel.
	other := f.a.createChannel(f.admin, "elsewhere", "text")
	if res := f.mod.get("/api/channels/" + other.String() + "/messages/" + msg.String() + "/edits"); res.status != http.StatusNotFound {
		t.Errorf("wrong channel: %d", res.status)
	}
	// A moderator who loses sight of the channel loses the history too.
	everyone := everyoneID(t, f.admin)
	hide := putOverwrites(f.admin, "channels", f.channel, overwrite("role", everyone, 0, perm.ViewChannel), overwrite("role", f.modRole, 0, perm.ViewChannel))
	if hide.status != http.StatusNoContent {
		t.Fatalf("hide: %d", hide.status)
	}
	if res := f.history(f.mod, msg); res.status == http.StatusOK {
		t.Errorf("a moderator without View channel read the history: %s", res.body)
	}
	if res := f.history(f.admin, msg); res.status != http.StatusOK {
		t.Errorf("the Owner always may: %d", res.status)
	}
}

func TestEarlierVersionsAreNotSearchableNorInMessages(t *testing.T) {
	f := newMessageModerationFixture(t, false)
	msg := f.a.send(f.user, f.channel, "pineapple original")
	f.edit(f.user, msg, "walnut replacement")

	var search struct{ Messages []struct{ Content string } }
	f.mod.get("/api/search?q=pineapple").decode(t, &search)
	if len(search.Messages) != 0 {
		t.Fatalf("search finds a replaced version: %+v", search)
	}
	list := f.mod.get(f.path("/messages"))
	if list.status != http.StatusOK || strings.Contains(string(list.body), "pineapple") {
		t.Fatalf("the message list carries the history: %d %s", list.status, list.body)
	}
}

func TestAMessageKeepsAtMostFiftyVersions(t *testing.T) {
	f := newMessageModerationFixture(t, false)
	msg := f.a.send(f.user, f.channel, "version 0")
	for i := 1; i <= chat.MaxEditVersions+5; i++ {
		f.edit(f.user, msg, fmt.Sprintf("version %d", i))
	}
	var got editVersions
	f.history(f.mod, msg).decode(t, &got)
	if len(got.Edits) != chat.MaxEditVersions {
		t.Fatalf("kept %d versions", len(got.Edits))
	}
	last := chat.MaxEditVersions + 4
	if got.Edits[0].Content != fmt.Sprintf("version %d", last) || got.Edits[len(got.Edits)-1].Content != "version 5" {
		t.Errorf("the oldest go first: newest %q oldest %q", got.Edits[0].Content, got.Edits[len(got.Edits)-1].Content)
	}
}

func TestDeletingAMessageDeletesItsHistory(t *testing.T) {
	f := newMessageModerationFixture(t, false)
	msg := f.a.send(f.user, f.channel, "one")
	f.edit(f.user, msg, "two")
	if n := f.a.countRows(`SELECT COUNT(*) FROM message_edits WHERE message_id = $1`, msg); n != 1 {
		t.Fatalf("setup: %d", n)
	}
	if res := f.user.delete(f.path("/messages/" + msg.String())); res.status != http.StatusNoContent {
		t.Fatalf("delete: %d", res.status)
	}
	if n := f.a.countRows(`SELECT COUNT(*) FROM message_edits WHERE message_id = $1`, msg); n != 0 {
		t.Errorf("%d versions outlived the message", n)
	}
}

func TestRetentionDropsOnlyVersionsReplacedLongAgo(t *testing.T) {
	f := newMessageModerationFixture(t, false)
	msg := f.a.send(f.user, f.channel, "one")
	f.edit(f.user, msg, "two")
	f.edit(f.user, msg, "three")
	if _, err := f.a.db.Exec(context.Background(),
		`UPDATE message_edits SET replaced_at = NOW() - INTERVAL '40 days' WHERE content = 'one'`); err != nil {
		t.Fatal(err)
	}
	n, err := chat.PruneEditHistory(context.Background(), f.a.db, 30)
	if err != nil || n != 1 {
		t.Fatalf("pruned %d, %v", n, err)
	}
	var got editVersions
	f.history(f.mod, msg).decode(t, &got)
	if len(got.Edits) != 1 || got.Edits[0].Content != "two" {
		t.Fatalf("remaining: %+v", got)
	}
	if again, _ := chat.PruneEditHistory(context.Background(), f.a.db, 30); again != 0 {
		t.Errorf("a second run deleted %d", again)
	}
}

func TestTheRetentionWorkerPrunesInTheBackgroundOnlyWhenEnabled(t *testing.T) {
	f := newMessageModerationFixture(t, false)
	msg := f.a.send(f.user, f.channel, "one")
	f.edit(f.user, msg, "two")
	if _, err := f.a.db.Exec(context.Background(), `UPDATE message_edits SET replaced_at = NOW() - INTERVAL '40 days'`); err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	chat.RunEditHistoryRetention(ctx, f.a.db, 0, time.Millisecond) // disabled: returns at once, deletes nothing
	if n := f.a.countRows(`SELECT COUNT(*) FROM message_edits`); n != 1 {
		t.Fatalf("a disabled worker deleted: %d", n)
	}
	done := make(chan struct{})
	go func() {
		chat.RunEditHistoryRetention(ctx, f.a.db, 30, time.Millisecond)
		close(done)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for f.a.countRows(`SELECT COUNT(*) FROM message_edits`) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("the worker never pruned")
		}
		time.Sleep(10 * time.Millisecond)
	}
	stop()
	<-done
}

func TestReadingEditHistoryIsRateLimitedPerMember(t *testing.T) {
	f := newMessageModerationFixture(t, false)
	msg := f.a.send(f.user, f.channel, "one")
	f.edit(f.user, msg, "two")
	deadline := time.Now().Add(time.Minute)
	for i := 0; i < chat.EditHistoryRate; i++ {
		if res := f.history(f.mod, msg); res.status != http.StatusOK {
			t.Fatalf("read %d: %d", i, res.status)
		}
	}
	res := f.history(f.mod, msg)
	if res.status != http.StatusTooManyRequests || res.header.Get("Retry-After") == "" {
		t.Fatalf("over the limit: %d %v", res.status, res.header)
	}
	if time.Now().After(deadline) {
		t.Skip("too slow to judge the window")
	}
	if other := f.history(f.admin, msg); other.status != http.StatusOK {
		t.Errorf("another member is not held back: %d", other.status)
	}
}
