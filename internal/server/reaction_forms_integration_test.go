//go:build integration

package server

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/core"
)

// One emoji with and without U+FE0F is one reaction (doc/bots.md and the
// reaction routes' descriptions), on the member and on the bot routes: the
// quick reactions send a thumbs up without it, the full picker with it.

const (
	thumbs      = "\U0001F44D"
	thumbsVS    = "\U0001F44D\uFE0F"
	heart       = "\u2764"
	heartVS     = "\u2764\uFE0F"
	partyPopper = "\U0001F389"
)

type reactionList struct{ Reactions []core.ReactionSummary }

func TestTheFormsOfOneEmojiAreOneReaction(t *testing.T) {
	f := newBotFixture(t, false)
	msg := postMessage(t, f.owner, f.open, map[string]any{"content": "react to me"})
	toggle := func(emoji string) []core.ReactionSummary {
		res := f.owner.post("/api/messages/"+msg.String()+"/reactions", map[string]string{"emoji": emoji})
		wantStatus(t, "toggle "+emoji, res, http.StatusOK)
		var out reactionList
		res.decode(t, &out)
		return out.Reactions
	}
	if got := toggle(thumbs); len(got) != 1 || got[0].Emoji != thumbs {
		t.Fatalf("quick thumbs up: %+v", got)
	}
	if got := toggle(thumbsVS); len(got) != 0 {
		t.Fatalf("the picker's thumbs up is the same reaction and takes it back, got %+v", got)
	}
	if got := toggle(heart); len(got) != 1 || got[0].Emoji != heartVS || got[0].Count != 1 {
		t.Fatalf("a heart without U+FE0F is stored as the picker sends it, got %+v", got)
	}
	reactions := botPath(f.open, "/messages/"+msg.String()+"/reactions/")
	wantStatus(t, "bot adds the bare heart", f.client.put(reactions+url.PathEscape(heart)+"/@me"), http.StatusNoContent)
	wantStatus(t, "bot adds the full heart", f.client.put(reactions+url.PathEscape(heartVS)+"/@me"), http.StatusNoContent)
	if n := reactionRows(t, f.a, msg, f.bot.UserID); n != 1 {
		t.Fatalf("the bot holds %d rows of one heart", n)
	}
}

// Rows written before the rule may hold one member's emoji in both forms. They
// are one reaction: shown once and counted once, adding again changes nothing,
// taking it back removes both, and the limits count it once. No migration
// touches them.
func TestOldRowsInBothFormsAreOneReaction(t *testing.T) {
	f := newBotFixture(t, false)
	msg := postMessage(t, f.owner, f.open, map[string]any{"content": "old reactions"})
	seedReactions(t, f.a, msg, f.bot.UserID, heart, heartVS)
	var list []struct {
		ID        uuid.UUID
		Reactions []core.ReactionSummary
	}
	f.owner.get("/api/channels/"+f.open.String()+"/messages").decode(t, &list)
	for _, m := range list {
		if m.ID == msg && (len(m.Reactions) != 1 || m.Reactions[0].Emoji != heartVS || m.Reactions[0].Count != 1 || len(m.Reactions[0].Users) != 1) {
			t.Errorf("two rows of one heart show as %+v", m.Reactions)
		}
	}
	reactions := botPath(f.open, "/messages/"+msg.String()+"/reactions/")
	for _, form := range []string{heart, heartVS} {
		wantStatus(t, "add again "+form, f.client.put(reactions+url.PathEscape(form)+"/@me"), http.StatusNoContent)
	}
	if n := reactionRows(t, f.a, msg, f.bot.UserID); n != 2 {
		t.Fatalf("adding a held reaction again changed the rows to %d", n)
	}
	wantStatus(t, "take back", f.client.delete(reactions+url.PathEscape(heartVS)+"/@me"), http.StatusNoContent)
	if n := reactionRows(t, f.a, msg, f.bot.UserID); n != 0 {
		t.Fatalf("taking the heart back left %d rows", n)
	}

	// 18 others and the heart twice: 19 reactions of the bot, one more fits.
	own := postMessage(t, f.owner, f.open, map[string]any{"content": "the bot's limit"})
	seedReactions(t, f.a, own, f.bot.UserID, append(fakeEmoji("b", 18), heart, heartVS)...)
	wantStatus(t, "20th reaction of the bot", f.client.put(botPath(f.open, "/messages/"+own.String()+"/reactions/"+url.PathEscape(partyPopper)+"/@me")), http.StatusNoContent)

	// 48 emoji of the Owner and the bot's heart twice: 49 emoji, one more fits.
	full := postMessage(t, f.owner, f.open, map[string]any{"content": "the message's limit"})
	seedReactions(t, f.a, full, f.owner.user.ID, fakeEmoji("o", 48)...)
	seedReactions(t, f.a, full, f.bot.UserID, heart, heartVS)
	wantStatus(t, "50th emoji of the message", f.client.put(botPath(f.open, "/messages/"+full.String()+"/reactions/"+url.PathEscape(partyPopper)+"/@me")), http.StatusNoContent)
}

// seedReactions writes rows as an older server could have, one second apart.
func seedReactions(t *testing.T, a *app, msg, user uuid.UUID, emoji ...string) {
	t.Helper()
	at := time.Now().Add(-time.Hour)
	for i, e := range emoji {
		if _, err := a.db.Exec(context.Background(), `INSERT INTO message_reactions (message_id, user_id, emoji, created_at) VALUES ($1, $2, $3, $4)`,
			msg, user, e, at.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
}

func reactionRows(t *testing.T, a *app, msg, user uuid.UUID) (n int) {
	t.Helper()
	if err := a.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM message_reactions WHERE message_id = $1 AND user_id = $2`, msg, user).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func fakeEmoji(prefix string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("%s%02d", prefix, i)
	}
	return out
}
