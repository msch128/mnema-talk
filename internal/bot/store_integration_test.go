//go:build integration

package bot

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func assertTokenStatesAndExpiry(t *testing.T, ctx context.Context, e *env, f fixtureBot) {
	t.Helper()
	if _, err := e.p.Exec(ctx, `INSERT INTO bot_tokens (bot_id, token_hash, hint, expires_at) VALUES ($1, $2, 'oldx', NOW() - INTERVAL '1 hour')`, f.bot.ID, HashToken("expired")); err != nil {
		t.Fatal(err)
	}
	toks, err := Tokens(ctx, e.p, f.bot.ID)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]bool{}
	for _, tk := range toks {
		states[tk.State] = true
	}
	if !states["expired"] || !states["active"] {
		t.Fatalf("token states: %v", states)
	}
	now := time.Now()
	if CheckExpiry(now.Add(-time.Second), now) == nil {
		t.Fatal("CheckExpiry accepted past expiry")
	}
	if CheckExpiry(now.Add(MaxTokenLifetime+time.Hour), now) == nil {
		t.Fatal("CheckExpiry accepted excessive expiry")
	}
	if CheckExpiry(now.Add(time.Hour), now) != nil {
		t.Fatal("CheckExpiry rejected valid 1-hour expiry")
	}
}

func assertCancelledContextFailsStatements(t *testing.T, ctx context.Context, e *env, f fixtureBot) {
	t.Helper()
	dead, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := Create(dead, e.p, "x", 0, e.owner.ID); err == nil {
		t.Fatal("create")
	}
	if _, err := List(dead, e.p); err == nil {
		t.Fatal("list")
	}
	if _, err := Tokens(dead, e.p, f.bot.ID); err == nil {
		t.Fatal("tokens")
	}
	if err := RevokeToken(dead, e.p, f.bot.ID, f.token.ID); err == nil {
		t.Fatal("revoke")
	}
}

func TestBotStoreEdges(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := Get(ctx, e.p, uuid.New()); err == nil {
		t.Fatal("unknown bot")
	}
	WithRoles([]Bot{{UserID: uuid.New()}}, map[uuid.UUID][]uuid.UUID{})
	f := e.newBot()
	bots, _ := List(ctx, e.p)
	WithRoles(bots, map[uuid.UUID][]uuid.UUID{f.bot.UserID: {e.role}})
	if len(bots[0].RoleIDs) != 1 {
		t.Fatal("roles not filled")
	}
	assertTokenStatesAndExpiry(t, ctx, e, f)
	if err := Update(ctx, e.p, uuid.New(), nil, nil, nil); err == nil {
		t.Fatal("update of an unknown bot")
	}
	if _, err := UpsertCommand(ctx, e.p, uuid.New(), CommandInput{Name: "x", Description: "x"}); err == nil {
		t.Fatal("command for an unknown bot")
	}
	if err := Delete(ctx, e.p, uuid.New()); err == nil {
		t.Fatal("delete of an unknown bot")
	}
	if _, _, err := IssueToken(ctx, e.p, uuid.New(), "x", nil, uuid.Nil); err == nil {
		t.Fatal("token for an unknown bot")
	}
	if err := RevokeToken(ctx, e.p, f.bot.ID, uuid.New()); err == nil {
		t.Fatal("unknown token")
	}
	assertCancelledContextFailsStatements(t, ctx, e, f)
}

func TestNewBotsGetReadableUniqueUsernames(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	username := func(b *Bot) string {
		var u string
		if err := e.p.QueryRow(ctx, `SELECT username FROM users WHERE id = $1`, b.UserID).Scan(&u); err != nil {
			t.Fatal(err)
		}
		return u
	}
	create := func(name string) string {
		b, err := Create(ctx, e.p, name, 0, e.owner.ID)
		if err != nil {
			t.Fatal(err)
		}
		return username(b)
	}
	if got := create("Mnemo"); got != "mnemo" {
		t.Fatalf("first: %q", got)
	}
	if got := create("MNEMO"); got != "mnemo_2" {
		t.Fatalf("second: %q", got)
	}
	// A member who holds the name counts as a collision, too.
	if _, err := e.p.Exec(ctx, `INSERT INTO users (username, display_name, password_hash) VALUES ('helper', 'h', 'x')`); err != nil {
		t.Fatal(err)
	}
	if got := create("Helper"); got != "helper_2" {
		t.Fatalf("against a member: %q", got)
	}
	// Reserved words and unusable names get the random fallback.
	for _, name := range []string{"all", "!?"} {
		if got := create(name); len(got) != len("bot-")+12 || got[:4] != "bot-" {
			t.Fatalf("%q: %q", name, got)
		}
	}
}

func TestConcurrentBotCreationWithSameName(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	const count = 5
	errs := make([]error, count)
	bots := make([]*Bot, count)
	var started, finished sync.WaitGroup
	start := make(chan struct{})
	started.Add(count)
	finished.Add(count)

	for i := 0; i < count; i++ {
		go func(idx int) {
			defer finished.Done()
			started.Done()
			<-start
			bots[idx], errs[idx] = Create(ctx, e.p, "Mnemo Bot", 0, e.owner.ID)
		}(i)
	}

	started.Wait()
	close(start)
	finished.Wait()

	base := "mnemo_bot"
	seen := make(map[string]bool, count)
	for i, b := range bots {
		if errs[i] != nil {
			t.Fatalf("bot %d creation failed: %v", i, errs[i])
		}
		var u string
		if err := e.p.QueryRow(ctx, `SELECT username FROM users WHERE id = $1`, b.UserID).Scan(&u); err != nil {
			t.Fatalf("query username %d: %v", i, err)
		}
		if !strings.HasPrefix(u, base) {
			t.Fatalf("bot %d username %q does not start with base %q", i, u, base)
		}
		if seen[u] {
			t.Fatalf("duplicate username %q", u)
		}
		seen[u] = true
	}
	if len(seen) != count {
		t.Fatalf("expected %d distinct usernames, got %d", count, len(seen))
	}
}
