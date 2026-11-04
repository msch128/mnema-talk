//go:build integration

package later

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/perm"
)

// statementCounter counts the statements that contain a fragment.
type statementCounter struct {
	fragment string
	n        atomic.Int32
}

func (c *statementCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if strings.Contains(d.SQL, c.fragment) {
		c.n.Add(1)
	}
	return ctx
}

func (*statementCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// The list shows what each reminder points at, but it reads the messages in one
// round trip, not one per reminder.
func TestListingRemindersReadsTheirMessagesInOneQuery(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	sub := perm.Subject{ID: f.user}
	svc := f.service(f.p)
	var gone uuid.UUID
	for i := 0; i < 5; i++ {
		var msg uuid.UUID
		if err := f.p.QueryRow(ctx, `INSERT INTO messages (channel_id, user_id, content) VALUES ($1, $2, $3) RETURNING id`,
			f.channel, f.user, strings.Repeat("m", i+1)).Scan(&msg); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.AddReminder(ctx, sub, msg, time.Now().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		gone = msg
	}
	// A reminder whose message was deleted stays in the list, without a message.
	if _, err := f.p.Exec(ctx, `DELETE FROM messages WHERE id = $1`, gone); err != nil {
		t.Fatal(err)
	}

	counter := &statementCounter{fragment: "FROM messages m"}
	config := f.p.Config()
	config.MaxConns, config.MinConns, config.MinIdleConns = 1, 0, 0
	config.ConnConfig.Tracer = counter
	traced, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(traced.Close)

	got, err := f.service(&db.Pool{Pool: traced}).ListReminders(ctx, sub)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("%d reminders listed, want 5", len(got))
	}
	available, snippets := 0, map[string]bool{}
	for _, r := range got {
		if r.Available {
			available++
			snippets[r.Message.Snippet] = true
		}
	}
	if available != 4 || len(snippets) != 4 {
		t.Errorf("%d reminders with a message and %d distinct texts, want 4 and 4", available, len(snippets))
	}
	if n := counter.n.Load(); n != 1 {
		t.Errorf("listing 5 reminders read messages in %d queries, want 1", n)
	}
}
