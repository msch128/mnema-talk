//go:build integration

package server

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestMessagesAreScopedAndValidated(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	max := a.register(admin, "max")
	general := a.createChannel(admin, "general", "text")
	other := a.createChannel(admin, "other", "text")
	voice := a.createChannel(admin, "Lounge", "voice")

	root := a.send(admin, general, "root message")

	long := max.post("/api/channels/"+general.String()+"/messages", map[string]string{"content": strings.Repeat("x", 4001)})
	if long.status != http.StatusBadRequest {
		t.Errorf("over-long message: %d", long.status)
	}
	cross := max.post("/api/channels/"+other.String()+"/messages", map[string]any{"content": "reply", "parent_id": root})
	if cross.status != http.StatusBadRequest {
		t.Errorf("thread reply across channels: %d", cross.status)
	}
	// A voice channel has its own chat; a thread root of another channel is still refused there.
	if res := max.post("/api/channels/"+voice.String()+"/messages", map[string]string{"content": "hi"}); res.status != http.StatusCreated {
		t.Errorf("text in voice channel: %d %s", res.status, res.body)
	}
	if res := max.post("/api/channels/"+voice.String()+"/messages", map[string]any{"content": "reply", "parent_id": root}); res.status != http.StatusBadRequest {
		t.Errorf("thread reply from a voice channel into a text channel's thread: %d", res.status)
	}
	// The channel in the URL must match the message's channel.
	if res := admin.delete("/api/channels/" + other.String() + "/messages/" + root.String()); res.status != http.StatusNotFound {
		t.Errorf("delete through the wrong channel path: %d", res.status)
	}
	if res := max.put("/api/channels/"+general.String()+"/messages/"+root.String(), map[string]string{"content": "hijack"}); res.status != http.StatusForbidden {
		t.Errorf("editing someone else's message: %d", res.status)
	}
	if res := max.delete("/api/channels/" + general.String() + "/messages/" + root.String()); res.status != http.StatusForbidden {
		t.Errorf("member deleting someone else's message: %d", res.status)
	}

	reply := max.post("/api/channels/"+general.String()+"/messages", map[string]any{"content": "reply", "parent_id": root})
	if reply.status != http.StatusCreated {
		t.Fatalf("valid reply: %d %s", reply.status, reply.body)
	}
	reply2 := max.post("/api/channels/"+general.String()+"/messages", map[string]any{"content": "reply 2", "parent_id": root})
	if reply2.status != http.StatusCreated {
		t.Fatalf("valid reply2: %d %s", reply2.status, reply2.body)
	}

	var thread struct {
		Root struct {
			ReplyCount int `json:"reply_count"`
		}
		Replies []struct {
			ID      string `json:"id"`
			Content string `json:"content"`
		}
	}
	admin.get("/api/messages/"+root.String()+"/thread").decode(t, &thread)
	if thread.Root.ReplyCount != 2 || len(thread.Replies) != 2 {
		t.Fatalf("thread %+v", thread)
	}

	// Paged: limit=1
	var paged struct {
		Replies []struct {
			ID      string `json:"id"`
			Content string `json:"content"`
		}
	}
	admin.get("/api/messages/"+root.String()+"/thread?limit=1").decode(t, &paged)
	if len(paged.Replies) != 1 || paged.Replies[0].Content != "reply" {
		t.Fatalf("expected 1st page to have 1 reply 'reply', got: %+v", paged.Replies)
	}
	firstID := paged.Replies[0].ID

	// Paged: after firstID
	admin.get("/api/messages/"+root.String()+"/thread?limit=1&after="+firstID).decode(t, &paged)
	if len(paged.Replies) != 1 || paged.Replies[0].Content != "reply 2" {
		t.Fatalf("expected 2nd page to have 'reply 2', got: %+v", paged.Replies)
	}

	if res := admin.delete("/api/channels/" + general.String() + "/messages/" + root.String()); res.status != http.StatusNoContent {
		t.Errorf("admin moderation delete: %d", res.status)
	}
}

func TestReactionsToggle(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "general", "text")
	msg := a.send(admin, ch, "react to me")
	path := "/api/messages/" + msg.String() + "/reactions"

	var out struct {
		Reactions []struct {
			Emoji string
			Count int
		}
	}
	admin.post(path, map[string]string{"emoji": "🔥"}).decode(t, &out)
	if len(out.Reactions) != 1 || out.Reactions[0].Count != 1 {
		t.Fatalf("add: %+v", out)
	}
	admin.post(path, map[string]string{"emoji": "🔥"}).decode(t, &out)
	if len(out.Reactions) != 0 {
		t.Fatalf("toggle off: %+v", out)
	}
	if res := admin.post(path, map[string]string{"emoji": "<script>"}); res.status != http.StatusBadRequest {
		t.Fatalf("invalid emoji: %d", res.status)
	}
	if res := admin.post("/api/messages/"+uuid.NewString()+"/reactions", map[string]string{"emoji": "🔥"}); res.status != http.StatusNotFound {
		t.Fatalf("unknown message: %d", res.status)
	}
}

type pageMsg struct {
	ID        uuid.UUID  `json:"id"`
	Content   string     `json:"content"`
	ReplyToID *uuid.UUID `json:"reply_to_id"`
	ReplyTo   *struct {
		ID          uuid.UUID `json:"id"`
		Deleted     bool      `json:"deleted"`
		DisplayName string    `json:"display_name"`
		Content     string    `json:"content"`
	} `json:"reply_to"`
}

func page(t *testing.T, c *client, channel uuid.UUID, query string) []pageMsg {
	t.Helper()
	res := c.get("/api/channels/" + channel.String() + "/messages" + query)
	if res.status != http.StatusOK {
		t.Fatalf("page %s: %d %s", query, res.status, res.body)
	}
	var msgs []pageMsg
	res.decode(t, &msgs)
	return msgs
}

// TestHistoryPagingAt15kMessages seeds a large channel directly in SQL and
// walks it with the cursor API: no gaps, no duplicates, correct order, fast.
func TestHistoryPagingAt15kMessages(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "general", "text")

	const total = 15000
	_, err := a.db.Exec(context.Background(), `
		INSERT INTO messages (channel_id, user_id, content, created_at)
		SELECT $1, $2, 'msg ' || g, NOW() - make_interval(secs => $3 - g)
		FROM generate_series(1, $3) AS g`, ch, admin.user.ID, total)
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	newest := page(t, admin, ch, "?limit=100")
	if len(newest) != 100 || newest[99].Content != "msg 15000" || newest[0].Content != "msg 14901" {
		t.Fatalf("newest page wrong: first=%q last=%q len=%d", newest[0].Content, newest[len(newest)-1].Content, len(newest))
	}

	// Walk all the way back with before=<oldest id of the current page>.
	seen := map[uuid.UUID]bool{}
	for _, m := range newest {
		seen[m.ID] = true
	}
	cursor := newest[0].ID
	pages := 1
	for {
		older := page(t, admin, ch, "?limit=100&before="+cursor.String())
		if len(older) == 0 {
			break
		}
		for _, m := range older {
			if seen[m.ID] {
				t.Fatalf("duplicate message %s while paging back", m.Content)
			}
			seen[m.ID] = true
		}
		cursor = older[0].ID
		pages++
	}
	elapsed := time.Since(start)
	if len(seen) != total {
		t.Fatalf("paged %d messages, want %d", len(seen), total)
	}
	perPage := elapsed / time.Duration(pages)
	t.Logf("%d pages over %d messages, %v per page", pages, total, perPage)
	if perPage > 250*time.Millisecond {
		t.Fatalf("history paging too slow: %v per page", perPage)
	}

	// Jump to a message deep in the history.
	var target uuid.UUID
	if err := a.db.QueryRow(context.Background(), `SELECT id FROM messages WHERE content = 'msg 777'`).Scan(&target); err != nil {
		t.Fatal(err)
	}
	around := page(t, admin, ch, "?limit=50&around="+target.String())
	found := -1
	for i, m := range around {
		if m.ID == target {
			found = i
		}
	}
	if len(around) != 50 || found < 20 || found > 30 {
		t.Fatalf("around page: len=%d anchor index=%d", len(around), found)
	}
	after := page(t, admin, ch, "?limit=10&after="+target.String())
	if len(after) != 10 || after[0].Content != "msg 778" {
		t.Fatalf("after page starts at %q", after[0].Content)
	}

	// The Discord case: quote a message far back in history from the newest end.
	// The quote arrives inside the newest page (no extra request), and jumping
	// back is one bounded "around" query regardless of the distance.
	var ancient uuid.UUID
	if err := a.db.QueryRow(context.Background(), `SELECT id FROM messages WHERE content = 'msg 3'`).Scan(&ancient); err != nil {
		t.Fatal(err)
	}
	res := admin.post("/api/channels/"+ch.String()+"/messages", map[string]any{"content": "guck mal", "reply_to_id": ancient})
	if res.status != http.StatusCreated {
		t.Fatalf("reply to ancient message: %d %s", res.status, res.body)
	}
	latest := page(t, admin, ch, "?limit=50")
	last := latest[len(latest)-1]
	if last.Content != "guck mal" || last.ReplyTo == nil || last.ReplyTo.Content != "msg 3" {
		t.Fatalf("newest page must carry the quoted preview: %+v", last.ReplyTo)
	}
	jumpStart := time.Now()
	jump := page(t, admin, ch, "?limit=50&around="+ancient.String())
	jumpTook := time.Since(jumpStart)
	if len(jump) == 0 || jump[0].Content != "msg 1" {
		t.Fatalf("jump to msg 3 must land at the start of history, got %q", jump[0].Content)
	}
	t.Logf("jump 15k messages back took %v", jumpTook)
	if jumpTook > 250*time.Millisecond {
		t.Fatalf("jump too slow: %v", jumpTook)
	}

	if res := admin.get("/api/channels/" + ch.String() + "/messages?before=" + target.String() + "&after=" + target.String()); res.status != http.StatusBadRequest {
		t.Fatalf("two anchors: %d", res.status)
	}
	other := a.createChannel(admin, "other", "text")
	if res := admin.get("/api/channels/" + other.String() + "/messages?around=" + target.String()); res.status != http.StatusNotFound {
		t.Fatalf("anchor from another channel: %d", res.status)
	}
}

func TestRepliesQuoteTheOriginal(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	max := a.register(admin, "max")
	ch := a.createChannel(admin, "general", "text")
	other := a.createChannel(admin, "other", "text")

	original := a.send(admin, ch, "the original message")
	res := max.post("/api/channels/"+ch.String()+"/messages", map[string]any{"content": "my answer", "reply_to_id": original})
	if res.status != http.StatusCreated {
		t.Fatalf("reply: %d %s", res.status, res.body)
	}
	var reply pageMsg
	res.decode(t, &reply)
	if reply.ReplyTo == nil || reply.ReplyTo.ID != original || reply.ReplyTo.Content != "the original message" || reply.ReplyTo.Deleted {
		t.Fatalf("reply preview: %+v", reply.ReplyTo)
	}

	if res := max.post("/api/channels/"+other.String()+"/messages", map[string]any{"content": "x", "reply_to_id": original}); res.status != http.StatusBadRequest {
		t.Fatalf("cross-channel reply: %d", res.status)
	}
	if res := max.post("/api/channels/"+ch.String()+"/messages", map[string]any{"content": "x", "reply_to_id": uuid.New()}); res.status != http.StatusBadRequest {
		t.Fatalf("reply to unknown message: %d", res.status)
	}

	if res := admin.delete("/api/channels/" + ch.String() + "/messages/" + original.String()); res.status != http.StatusNoContent {
		t.Fatalf("delete original: %d", res.status)
	}
	for _, m := range page(t, admin, ch, "") {
		if m.ID == reply.ID {
			if m.ReplyTo == nil || !m.ReplyTo.Deleted {
				t.Fatalf("reply to a deleted message must say so: %+v", m.ReplyTo)
			}
			return
		}
	}
	t.Fatal("reply vanished with its original")
}
