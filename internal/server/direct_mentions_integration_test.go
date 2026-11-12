//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/core"
)

// directMentionsFrame is the part of a message_create payload a client reads
// to decide whether a muted channel counts the message. A pointer tells a
// missing field from an empty list.
type directMentionsFrame struct {
	ID             uuid.UUID    `json:"id"`
	Mentions       []uuid.UUID  `json:"mentions"`
	DirectMentions *[]uuid.UUID `json:"direct_mentions"`
}

func expectMessageFrame(t *testing.T, ws *wsConn, id uuid.UUID) directMentionsFrame {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		raw, ok := ws.expect("message_create", time.Until(deadline))
		if !ok {
			break
		}
		var f directMentionsFrame
		if err := json.Unmarshal(raw, &f); err != nil {
			t.Fatal(err)
		}
		if f.ID == id {
			return f
		}
	}
	t.Fatalf("no message_create for %s", id)
	return directMentionsFrame{}
}

// TestMessageCreateSaysWhoIsAddressedDirectly: the message_create frame lists,
// next to everyone the message mentions, those it addresses by name or role
// (direct_mentions), the ones a mute lets through. A users-off webhook's
// "@all @namedmuted" reaches the member by @all alone, so a client that
// matches the text cannot tell; the frame must. The same frame shows that a
// bot named next to @all by such a webhook is not mentioned at all, as for
// "@bot" alone (doc/bots.md: @all never reaches a bot).
func TestMessageCreateSaysWhoIsAddressedDirectly(t *testing.T) {
	f := newBotFixture(t, true)
	f.a.router.Webhooks.SetLimits(1000, 1000)
	dev := mentionableRole(t, f.owner, "Dev", true)
	named := f.a.quickMember(f.owner, "namedmuted")
	holder := f.a.quickMember(f.owner, "devholder")
	assignRoles(t, f.owner, holder, dev)
	wantStatus(t, "mute", named.put("/api/channels/"+f.open.String()+"/notifications", map[string]any{"level": "mute"}), http.StatusNoContent)
	ws := named.dialLiveWS(f.a.origin())
	var botName string
	if err := f.a.db.QueryRow(t.Context(), `SELECT username FROM users WHERE id = $1`, f.bot.UserID).Scan(&botName); err != nil {
		t.Fatal(err)
	}
	send := f.a.kindsHook(t, f.owner, f.open)

	frame := expectMessageFrame(t, ws, send("@all @namedmuted @Dev @"+botName+" ping"))
	if !slices.Contains(frame.Mentions, named.user.ID) || !slices.Contains(frame.Mentions, holder.user.ID) {
		t.Fatalf("@all reaches both members: mentions %v", frame.Mentions)
	}
	if frame.DirectMentions == nil {
		t.Fatal("message_create has no direct_mentions")
	}
	if direct := *frame.DirectMentions; slices.Contains(direct, named.user.ID) || !slices.Contains(direct, holder.user.ID) {
		t.Fatalf("direct_mentions %v: want the role holder and not the plain-text name", direct)
	}
	if slices.Contains(frame.Mentions, f.bot.UserID) {
		t.Fatalf("a users-off webhook mentions the bot it names next to @all: mentions %v", frame.Mentions)
	}

	frame = expectMessageFrame(t, ws, send("@"+botName+" ping"))
	if slices.Contains(frame.Mentions, f.bot.UserID) || frame.DirectMentions == nil || len(*frame.DirectMentions) != 0 {
		t.Fatalf("control, @bot alone: mentions %v, direct %v", frame.Mentions, frame.DirectMentions)
	}

	// A member may name members: the name addresses them, and history says
	// the same as the live frame.
	msg := postedMessage(t, f.owner, f.open, "@all @namedmuted").ID
	frame = expectMessageFrame(t, ws, msg)
	if frame.DirectMentions == nil || !slices.Equal(*frame.DirectMentions, []uuid.UUID{named.user.ID}) {
		t.Fatalf("member's name mention: direct_mentions %v", frame.DirectMentions)
	}
	res := named.get("/api/channels/" + f.open.String() + "/messages")
	wantStatus(t, "history", res, http.StatusOK)
	var page []core.Message
	res.decode(t, &page)
	i := slices.IndexFunc(page, func(m core.Message) bool { return m.ID == msg })
	if i < 0 || !slices.Equal(page[i].DirectMentions, []uuid.UUID{named.user.ID}) {
		t.Fatalf("history of %s: %+v", msg, page)
	}
}

// kindsHook makes a webhook in ch that may use @all and roles but not name
// members, and returns a sender that waits for the stored message.
func (a *app) kindsHook(t *testing.T, owner *client, ch uuid.UUID) func(string) uuid.UUID {
	t.Helper()
	h := makeHook(t, owner, ch, "users-off")
	wantStatus(t, "settings", owner.patch("/api/admin/webhooks/"+h.ID.String(),
		map[string]any{"allow_user_mentions": false, "allow_everyone": true}), http.StatusOK)
	return func(content string) uuid.UUID {
		t.Helper()
		res := a.exec(h.path+"?wait=true", map[string]any{"content": content, "allowed_mentions": map[string]any{"parse": []string{"everyone", "roles"}}}, nil)
		wantStatus(t, "execute", res, http.StatusOK)
		var m core.Message
		res.decode(t, &m)
		return m.ID
	}
}
