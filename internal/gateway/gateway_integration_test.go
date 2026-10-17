//go:build integration

package gateway

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/testutil"
)

func TestMain(m *testing.M) { os.Exit(testutil.Main(m)) }

// A token check that cannot read the database keeps every session: it is a
// housekeeping pass, not a reason to throw bots out.
func TestTokenCheckKeepsSessionsWhenTheDatabaseFails(t *testing.T) {
	p := testutil.DB(t)
	g := New(Config{DB: p})
	s := &session{id: "s", botID: uuid.New(), tokenID: uuid.New(), conn: newConnection(nil)}
	g.register(s)
	dead, cancel := context.WithCancel(context.Background())
	cancel()
	g.checkTokens(dead)
	if s.conn.closed() {
		t.Fatal("a failed check closed a connection")
	}
	// A token the database does not know is gone: its connection closes.
	g.checkTokens(context.Background())
	if !s.conn.closed() || s.conn.closeCode != CloseAuthFailed {
		t.Fatalf("unknown token kept its connection: %d", s.conn.closeCode)
	}
	// A token that is still good keeps its connection.
	var botUser, botID, tokenID uuid.UUID
	ctx := context.Background()
	if err := p.QueryRow(ctx, `INSERT INTO users (username, display_name, password_hash, role, is_bot) VALUES ('bot-gw', 'gw', '!', 'user', TRUE) RETURNING id`).Scan(&botUser); err != nil {
		t.Fatal(err)
	}
	if err := p.QueryRow(ctx, `INSERT INTO bots (user_id, name) VALUES ($1, 'gw') RETURNING id`, botUser).Scan(&botID); err != nil {
		t.Fatal(err)
	}
	if err := p.QueryRow(ctx, `INSERT INTO bot_tokens (bot_id, token_hash, hint) VALUES ($1, '\x01', 'abcd') RETURNING id`, botID).Scan(&tokenID); err != nil {
		t.Fatal(err)
	}
	good := &session{id: "good", botID: botID, tokenID: tokenID, conn: newConnection(nil)}
	g.register(good)
	g.checkTokens(ctx)
	if good.conn.closed() {
		t.Fatal("a valid token lost its connection")
	}
	// Without sessions or database there is nothing to check.
	New(Config{DB: p}).checkTokens(context.Background())
	New(Config{}).checkTokens(context.Background())
}

func newBotWithTokens(t *testing.T, n int) (botID uuid.UUID, tokens []uuid.UUID) {
	t.Helper()
	p := testutil.DB(t)
	ctx := context.Background()
	var user uuid.UUID
	if err := p.QueryRow(ctx, `INSERT INTO users (username, display_name, password_hash, role, is_bot) VALUES ($1, 'gw', '!', 'user', TRUE) RETURNING id`, "bot-"+uuid.NewString()[:8]).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if err := p.QueryRow(ctx, `INSERT INTO bots (user_id, name) VALUES ($1, 'gw') RETURNING id`, user).Scan(&botID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		var id uuid.UUID
		if err := p.QueryRow(ctx, `INSERT INTO bot_tokens (bot_id, token_hash, hint) VALUES ($1, $2, 'abcd') RETURNING id`, botID, []byte(uuid.NewString())).Scan(&id); err != nil {
			t.Fatal(err)
		}
		tokens = append(tokens, id)
	}
	return botID, tokens
}

func revokeInDB(t *testing.T, id uuid.UUID) {
	t.Helper()
	if _, err := testutil.DB(t).Exec(context.Background(), `UPDATE bot_tokens SET revoked_at = NOW() WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
}

// A resume that raced a revocation ends the session, whichever came first.
func TestAdoptEndsSessionsOfRevokedTokens(t *testing.T) {
	g := New(Config{DB: testutil.DB(t)})
	ctx := context.Background()
	botID, tok := newBotWithTokens(t, 3)
	mk := func(token uuid.UUID) *session {
		s := &session{id: newSessionID(), botID: botID, tokenID: token, conn: newConnection(nil)}
		if !g.register(s) {
			t.Fatal("register")
		}
		return s
	}

	// The token that resumes was revoked before the swap: Revoke could not find
	// the session under it yet.
	s := mk(tok[0])
	revokeInDB(t, tok[1])
	if g.adopt(ctx, s, tok[1]) {
		t.Fatal("adopted a revoked token")
	}
	g.mu.Lock()
	_, still := g.sessions[s.id]
	g.mu.Unlock()
	if still || !s.conn.closed() || s.conn.closeCode != CloseAuthFailed {
		t.Fatal("the session must end")
	}

	// The session was revoked between the lookup and the swap.
	s = mk(tok[0])
	g.Revoke(botID, &tok[0])
	if g.adopt(ctx, s, tok[2]) {
		t.Fatal("adopted a session that Revoke ended")
	}

	// A good token takes the session over.
	s = mk(tok[0])
	if !g.adopt(ctx, s, tok[2]) || s.tokenID != tok[2] {
		t.Fatal("a good token resumes")
	}
	// A database that cannot answer ends the session too (fail closed).
	dead, cancel := context.WithCancel(ctx)
	cancel()
	if g.adopt(dead, s, tok[2]) || !s.conn.closed() {
		t.Fatal("an unverifiable token must not keep a session")
	}
	if (&Gateway{}).stillGood(ctx, &session{id: "x", conn: newConnection(nil)}) {
		t.Fatal("without a database nothing can be verified")
	}
}

// Revoking while the session is being resumed: whatever the interleaving, the
// session ends. Run with -race.
func TestRevokeWhileResumingEndsTheSession(t *testing.T) {
	g := New(Config{DB: testutil.DB(t)})
	botID, tok := newBotWithTokens(t, 2)
	for i := 0; i < 100; i++ {
		s := &session{id: newSessionID(), botID: botID, tokenID: tok[0], conn: newConnection(nil)}
		if !g.register(s) {
			t.Fatal("register")
		}
		revokeInDB(t, tok[1])
		done := make(chan bool)
		go func() { done <- g.adopt(context.Background(), s, tok[1]) }()
		g.Revoke(botID, &tok[1])
		g.Revoke(botID, &tok[0])
		<-done
		g.mu.Lock()
		_, still := g.sessions[s.id]
		g.mu.Unlock()
		if still || !s.conn.closed() {
			t.Fatalf("round %d: the session survived", i)
		}
		if _, err := testutil.DB(t).Exec(context.Background(), `UPDATE bot_tokens SET revoked_at = NULL WHERE id = $1`, tok[1]); err != nil {
			t.Fatal(err)
		}
	}
}
