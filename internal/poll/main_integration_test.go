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
	"github.com/msch128/mnema-talk/internal/chat/channels"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
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
	c, err := channels.CreateChannelWithStage(context.Background(), p, nil, "polls", core.ChannelTypeText, "", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	return fixture{p: p, user: u, channel: c.ID}
}

func (f fixture) service(p *db.Pool) *Service {
	return NewService(Service{DB: p, Perms: perm.NewService(f.p), Events: &events.Recorder{}})
}

func (f fixture) newPoll(t *testing.T) *core.Message {
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
