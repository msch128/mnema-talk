//go:build integration

package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/bot"
)

// The follow tap holds a thread reply for its lookup. Taps run in the order
// they were added, so a tap added before it (the bot gateway) would hand the
// reply out up to that long before the web clients get it, and a bot's
// delete or edit of it could reach them before the reply itself. With the
// lookup slowed down on purpose, the bot must not get the reply before the
// follower's session has the follow update.
func TestABotGetsAThreadReplyNoEarlierThanTheFollowersOfItsThread(t *testing.T) {
	f := gwFixture(t)
	root := postMessage(t, f.owner, f.open, map[string]any{"content": "question"})
	web := f.owner.dialLiveWS(f.a.origin())
	g := f.dialGW("Bot " + f.token.Secret)
	g.identify("", bot.IntentGuildMessages)

	// Every read of thread_follows now takes 800 ms; inserts (the reply's own follow) do not.
	ctx := context.Background()
	for _, s := range []string{
		`ALTER TABLE thread_follows RENAME TO thread_follows_real`,
		`CREATE VIEW thread_follows AS SELECT * FROM thread_follows_real WHERE (SELECT count(*) FROM pg_sleep(0.8)) = 1`,
	} {
		if _, err := f.a.db.Exec(ctx, s); err != nil {
			t.Fatal(s, err)
		}
	}
	t.Cleanup(func() {
		_, _ = f.a.db.Exec(ctx, `DROP VIEW thread_follows`)
		_, _ = f.a.db.Exec(ctx, `ALTER TABLE thread_follows_real RENAME TO thread_follows`)
	})

	start := time.Now()
	followed := make(chan time.Duration, 1)
	go func() {
		if raw, ok := web.expect("thread_follow_updated", 5*time.Second); ok && strings.Contains(string(raw), root.String()) {
			followed <- time.Since(start)
		}
		close(followed)
	}()
	posted := make(chan struct{})
	go func() {
		defer close(posted)
		f.owner.post("/api/channels/"+f.open.String()+"/messages", map[string]any{"content": "an answer", "parent_id": root})
	}()
	g.expect("MESSAGE_CREATE")
	botAt := time.Since(start)
	<-posted
	followAt, ok := <-followed
	if !ok {
		t.Fatal("the follower was not told")
	}
	if botAt < followAt-300*time.Millisecond {
		t.Fatalf("the bot had the reply at %v, the follower's follow update came at %v", botAt, followAt)
	}
}
