package server

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/metrics"
)

// With the writer gone the queue fills up, and further events are dropped
// instead of blocking the hub.
func TestAuditPresenceNeverBlocksTheHub(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	record := auditPresence(ctx, nil, nil)
	writePresence(ctx, nil, nil, presenceEvent{})
	before := metrics.WSDropped.With("audit_presence").Load()
	for i := 0; i < 3000; i++ {
		record(uuid.New(), uuid.New(), i%2 == 0)
	}
	// At most 1024 wait in the queue (the stopped writer may take a few
	// before it sees the cancelled context); the rest is counted as dropped.
	got := metrics.WSDropped.With("audit_presence").Load() - before
	if got == 0 || got > 3000-1024 {
		t.Errorf("dropped presence events counted = %d, want 1 to %d", got, 3000-1024)
	}
}

// presenceQuerier fails the DM lookup but answers everything else and counts
// the audit inserts.
type presenceQuerier struct {
	db.Querier
	inserts int
}

type stringRow struct{}

func (r stringRow) Scan(dest ...any) error {
	for i := range dest {
		switch d := dest[i].(type) {
		case *string:
			*d = "x"
		}
	}
	return nil
}

func (q *presenceQuerier) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	if strings.Contains(sql, "dm_id") {
		return failingRow{}
	}
	return stringRow{}
}

func (q *presenceQuerier) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	q.inserts++
	return pgconn.CommandTag{}, nil
}

// When the lookup that tells a conversation's Talk from a public one fails,
// nothing is audited: who called whom stays private.
func TestWritePresenceFailsClosedWhenTheDMLookupFails(t *testing.T) {
	q := &presenceQuerier{}
	l := &audit.Log{DB: q}
	writePresence(context.Background(), l, q, presenceEvent{user: uuid.New(), channel: uuid.New(), joined: true})
	if q.inserts != 0 {
		t.Fatalf("audited %d entries despite the failed lookup", q.inserts)
	}
}
