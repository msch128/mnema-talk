//go:build integration

package poll

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/testutil"
)

func TestMain(m *testing.M) { os.Exit(testutil.Main(m)) }

type fixture struct {
	p       *db.Pool
	user    *auth.User
	channel uuid.UUID
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	p := testutil.DB(t)
	testutil.Reset(t, p)
	u := &auth.User{Username: "pollster", DisplayName: "Pollster", Role: auth.RoleUser}
	if err := p.QueryRow(context.Background(), `INSERT INTO users (username, display_name, password_hash) VALUES ($1, $2, 'test-only-unused') RETURNING id, created_at`, u.Username, u.DisplayName).Scan(&u.ID, &u.CreatedAt); err != nil {
		t.Fatal(err)
	}
	c, err := chat.CreateChannel(context.Background(), p, nil, "polls", chat.ChannelTypeText, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	return fixture{p: p, user: u, channel: c.ID}
}

func (f fixture) service(p *db.Pool) *Service {
	return NewService(Service{DB: p, Events: &events.Recorder{}})
}

func (f fixture) newPoll(t *testing.T) *chat.Message {
	t.Helper()
	msg, err := f.service(f.p).Create(context.Background(), f.user, f.channel, Input{Question: "Pizza?", Options: []string{"Yes", "No"}})
	if err != nil {
		t.Fatal(err)
	}
	return msg
}

func (f fixture) messages(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.p.QueryRow(context.Background(), `SELECT COUNT(*) FROM messages`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// drift returns a pool that sees the given objects instead of the real tables:
// a database whose poll tables are broken in one way or another, with real
// PostgreSQL errors travelling through the real protocol.
func drift(t *testing.T, p *db.Pool, ddl ...string) *db.Pool {
	t.Helper()
	schema := "poll_drift_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	ctx := context.Background()
	if _, err := p.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := p.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	for _, stmt := range ddl {
		if _, err := p.Exec(ctx, strings.ReplaceAll(stmt, "SCHEMA.", schema+".")); err != nil {
			t.Fatal(err)
		}
	}
	cfg := p.Config().Copy()
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return &db.Pool{Pool: pool}
}

const (
	// A relation that cannot be written to, or lacks the columns the code reads.
	brokenPolls = `CREATE VIEW SCHEMA.polls AS SELECT NULL::uuid AS message_id WHERE false`
	// Even the key column is missing.
	keylessPolls  = `CREATE VIEW SCHEMA.polls AS SELECT 1 AS other`
	brokenOptions = `CREATE VIEW SCHEMA.poll_options AS SELECT NULL::uuid AS id WHERE false`
	brokenVotes   = `CREATE VIEW SCHEMA.poll_votes AS SELECT NULL::uuid AS option_id WHERE false`
	// A table that takes no vote.
	refusingVotes = `CREATE TABLE SCHEMA.poll_votes (option_id uuid, user_id uuid, created_at timestamptz DEFAULT now(), CHECK (false))`
)

func notAPollError(err error) bool {
	var api *httpx.APIError
	return err != nil && !errors.As(err, &api)
}

func TestAPollThatCannotBeStoredLeavesNoMessageBehind(t *testing.T) {
	f := newFixture(t)
	for name, broken := range map[string]string{"poll row": brokenPolls, "options": brokenOptions} {
		p := drift(t, f.p, broken)
		_, err := f.service(p).Create(context.Background(), f.user, f.channel, Input{Question: "Pizza?", Options: []string{"Yes", "No"}})
		if !notAPollError(err) {
			t.Fatalf("%s: error = %v", name, err)
		}
		if n := f.messages(t); n != 0 {
			t.Fatalf("%s: %d messages left behind", name, n)
		}
	}
}

func TestCreateRefusesBadInputAndMissingChannels(t *testing.T) {
	f := newFixture(t)
	s := f.service(f.p)
	if _, err := s.Create(context.Background(), f.user, f.channel, Input{Question: "only a question"}); err == nil {
		t.Fatal("a poll without answers was accepted")
	}
	if _, err := s.Create(context.Background(), f.user, uuid.New(), Input{Question: "Pizza?", Options: []string{"Yes", "No"}}); err == nil {
		t.Fatal("a poll in a channel that does not exist was accepted")
	}
	if n := f.messages(t); n != 0 {
		t.Fatalf("%d messages left behind", n)
	}
}

func TestVotingReportsDatabaseFailuresInsteadOfHidingThem(t *testing.T) {
	f := newFixture(t)
	m := f.newPoll(t)
	option := m.Poll.Options[0].ID
	ctx := context.Background()
	cases := map[string]struct {
		ddl []string
		run func(p *db.Pool) error
	}{
		"poll lookup":     {[]string{brokenPolls}, func(p *db.Pool) error { return Vote(ctx, p, m.ID, f.user.ID, []uuid.UUID{option}) }},
		"option check":    {[]string{brokenOptions}, func(p *db.Pool) error { return Vote(ctx, p, m.ID, f.user.ID, []uuid.UUID{option}) }},
		"clearing votes":  {[]string{brokenVotes}, func(p *db.Pool) error { return Vote(ctx, p, m.ID, f.user.ID, []uuid.UUID{option}) }},
		"storing a vote":  {[]string{refusingVotes}, func(p *db.Pool) error { return Vote(ctx, p, m.ID, f.user.ID, []uuid.UUID{option}) }},
		"reading votes":   {[]string{brokenVotes}, func(p *db.Pool) error { _, err := MyVotes(ctx, p, m.ID, f.user.ID); return err }},
		"finding a poll":  {[]string{keylessPolls}, func(p *db.Pool) error { _, err := loadRef(ctx, p, m.ID); return err }},
		"ending a poll":   {[]string{brokenPolls}, func(p *db.Pool) error { _, err := end(ctx, p, m.ID); return err }},
		"announcing":      {[]string{brokenPolls}, func(p *db.Pool) error { _, err := f.service(p).announce(ctx, f.channel, m.ID); return err }},
		"reading details": {[]string{brokenPolls}, func(p *db.Pool) error { _, err := f.service(p).detail(ctx, m.ID, f.user.ID); return err }},
		"reading my part": {[]string{brokenVotes}, func(p *db.Pool) error { _, err := f.service(p).detail(ctx, m.ID, f.user.ID); return err }},
	}
	for name, c := range cases {
		if err := c.run(drift(t, f.p, c.ddl...)); !notAPollError(err) {
			t.Errorf("%s: error = %v", name, err)
		}
	}
	// None of it changed the poll.
	got, err := chat.LoadPoll(ctx, f.p, m.ID)
	if err != nil || got.TotalVoters != 0 {
		t.Fatalf("poll after the failures: %+v, %v", got, err)
	}
}

func TestMessagesWithoutAPollHaveNoPollToAnnounceOrShow(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id, err := chat.CreateMessage(ctx, f.p, chat.NewMessage{ChannelID: f.channel, UserID: f.user.ID, Content: "just words"})
	if err != nil {
		t.Fatal(err)
	}
	s := f.service(f.p)
	if _, err := s.announce(ctx, f.channel, id); err == nil {
		t.Fatal("announced a poll that does not exist")
	}
	if _, err := s.detail(ctx, id, f.user.ID); err == nil {
		t.Fatal("showed a poll that does not exist")
	}
	if err := Vote(ctx, f.p, id, f.user.ID, nil); err == nil {
		t.Fatal("voted in a poll that does not exist")
	}
	if _, err := loadRef(ctx, f.p, id); err == nil {
		t.Fatal("found a poll that does not exist")
	}
	if changed, err := end(ctx, f.p, id); err != nil || changed {
		t.Fatalf("ending a poll that does not exist: %v, %v", changed, err)
	}
}
