//go:build integration

package server

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/testutil"
)

// Lane 8, inventory I6 row 47: the audit log's Talk presence queue against a
// burst. doc/moderation.md:517 lists voice.join and voice.leave as recorded
// when "a member enters or leaves a Talk"; auditPresence drops an event when
// 1024 wait, with a log line and no metric. This fires a burst of joins at the
// real writer and database, then counts the rows. It measures the writer's
// rate, so the size of a burst that loses rows can be stated.
//
// Inverse assertion (known loss): it asserts that a burst of AUDIT_BURST (3000)
// joins loses rows, and turns red the day none are lost.
func TestAuditPresenceBurstLosesJoinsPastTheQueue(t *testing.T) {
	pool := testutil.DB(t)
	testutil.Reset(t, pool)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	var channel uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO channels (name, type) VALUES ('stage', 'voice') RETURNING id`).Scan(&channel); err != nil {
		t.Fatal(err)
	}
	burst := 3000
	if v, err := strconv.Atoi(os.Getenv("AUDIT_BURST")); err == nil && v > 0 {
		burst = v
	}
	users := make([]uuid.UUID, burst)
	for i := range users {
		users[i] = uuid.New()
	}
	record := auditPresence(ctx, &audit.Log{DB: pool}, pool)

	start := time.Now()
	for _, u := range users {
		record(u, channel, true)
	}
	fired := time.Since(start)

	count := func() int {
		var n int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_log WHERE action = 'voice.join' AND channel_id = $1`, channel).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	// Wait until the writer stops making progress.
	last, stable := -1, 0
	for deadline := time.Now().Add(2 * time.Minute); stable < 10 && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if n := count(); n == last {
			stable++
		} else {
			last, stable = n, 0
		}
	}
	drained := time.Since(start) - time.Second // the stable window
	rate := float64(last) / drained.Seconds()
	t.Logf("burst of %d joins fired in %v; %d recorded, %d lost; writer about %.0f events/s, so a burst loses rows once it outruns 1024 + %.0f per second it lasts",
		burst, fired.Round(time.Millisecond), last, burst-last, rate, rate)
	if last == burst {
		t.Fatalf("no join of a %d-join burst was lost: the queue no longer drops; invert this test", burst)
	}
	if last < 1024 {
		t.Fatalf("only %d rows: fewer than the queue holds", last)
	}
}
