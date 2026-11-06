//go:build integration

package later

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// breakMessageInserts makes every message insert fail with an error of our own
// (not a refusal), as a database fault would.
func breakMessageInserts(t *testing.T, f fixture) {
	t.Helper()
	ctx := context.Background()
	for _, q := range []string{
		`CREATE FUNCTION later_test_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected fault'; END $$`,
		`CREATE TRIGGER later_test_fail BEFORE INSERT ON messages FOR EACH ROW EXECUTE FUNCTION later_test_fail()`,
	} {
		if _, err := f.p.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = f.p.Exec(context.Background(), `DROP TRIGGER IF EXISTS later_test_fail ON messages`)
		_, _ = f.p.Exec(context.Background(), `DROP FUNCTION IF EXISTS later_test_fail()`)
	})
}

func TestAMessageThatKeepsFailingToSendBacksOffAndEndsAsAFailedDraft(t *testing.T) {
	f := newFixture(t)
	s := f.service(f.p)
	ctx := context.Background()
	breakMessageInserts(t, f)
	var id uuid.UUID
	if err := f.p.QueryRow(ctx, `INSERT INTO scheduled_messages (user_id, channel_id, content, send_at) VALUES ($1, $2, 'x', NOW() - INTERVAL '1 minute') RETURNING id`, f.user, f.channel).Scan(&id); err != nil {
		t.Fatal(err)
	}
	state := func() (st, reason string, attempts int) {
		t.Helper()
		if err := f.p.QueryRow(ctx, `SELECT state, failure_reason, attempts FROM scheduled_messages`).Scan(&st, &reason, &attempts); err != nil {
			t.Fatal(err)
		}
		return
	}

	_ = s.sendScheduled(ctx)
	if st, _, n := state(); st != StatePending || n != 1 {
		t.Fatalf("after the first failure: state %s, attempts %d", st, n)
	}
	// The next sweep is inside the pause: nothing is tried.
	_ = s.sendScheduled(ctx)
	if _, _, n := state(); n != 1 {
		t.Fatalf("a sweep inside the pause tried again: attempts %d", n)
	}
	for i := 1; i < maxSendAttempts; i++ {
		if _, err := f.p.Exec(ctx, `UPDATE scheduled_messages SET retry_at = NOW() - INTERVAL '1 second'`); err != nil {
			t.Fatal(err)
		}
		_ = s.sendScheduled(ctx)
	}
	if st, reason, _ := state(); st != StateFailed || reason != ReasonRefused {
		t.Fatalf("after %d failures: state %s, reason %q", maxSendAttempts, st, reason)
	}

	// A new time gives the draft a fresh start.
	at := time.Now().Add(time.Hour)
	if _, err := s.UpdateScheduled(ctx, f.user, id, ScheduledPatch{SendAt: &at}); err != nil {
		t.Fatal(err)
	}
	if st, _, n := state(); st != StatePending || n != 0 {
		t.Fatalf("after rescheduling: state %s, attempts %d", st, n)
	}
}
