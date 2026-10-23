//go:build integration

package server

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat"
)

// The unread design must not cost the server more as the community grows:
// sending a message writes no per-recipient row, and a read marker is written
// only when the member really read something new. These tests prove it with
// 10,000 members and print the numbers quoted in doc/architecture.md#capacity
// (run with -v).

const scaleMembers = 10000

// writesDuring runs fn and returns, per table, how many row versions it
// created (inserts and updates; every write stamps its transaction id on the
// row). Exact, unlike the statistics views, which arrive late.
func (a *app) writesDuring(fn func()) map[string]int64 {
	a.t.Helper()
	ctx := context.Background()
	var before int64
	if err := a.db.QueryRow(ctx, `SELECT pg_current_xact_id()::text::bigint`).Scan(&before); err != nil {
		a.t.Fatal(err)
	}
	fn()
	rows, err := a.db.Query(ctx, `SELECT tablename FROM pg_tables WHERE schemaname = 'public'`)
	if err != nil {
		a.t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			a.t.Fatal(err)
		}
		tables = append(tables, name)
	}
	rows.Close()
	written := map[string]int64{}
	for _, table := range tables {
		var n int64
		q := fmt.Sprintf(`SELECT count(*) FROM %q WHERE xmin::text::bigint > $1`, table)
		if err := a.db.QueryRow(ctx, q, before).Scan(&n); err != nil {
			a.t.Fatal(err)
		}
		if n > 0 {
			written[table] = n
		}
	}
	return written
}

func (a *app) runSQL(sql string, args ...any) {
	a.t.Helper()
	if _, err := a.db.Exec(context.Background(), sql, args...); err != nil {
		a.t.Fatal(err)
	}
}

// seedMembers adds n members who signed up a day ago (so everything posted
// now is unread to them), straight into the table.
func (a *app) seedMembers(n int, prefix string) {
	a.t.Helper()
	a.runSQL(`INSERT INTO users (username, display_name, password_hash, created_at)
	        SELECT $1 || g, $1 || g, 'x', NOW() - interval '1 day' FROM generate_series(1, $2) g`, prefix, n)
	a.runSQL(`ANALYZE users`)
}

func median(d []time.Duration) time.Duration {
	slices.Sort(d)
	return d[len(d)/2]
}

func (a *app) timedSends(c *client, ch uuid.UUID, n int) time.Duration {
	a.t.Helper()
	var took []time.Duration
	for i := 0; i < n; i++ {
		start := time.Now()
		a.send(c, ch, fmt.Sprintf("message %d", i))
		took = append(took, time.Since(start))
	}
	return median(took)
}

func TestSendingAMessageCostsTheSameForTenMembersAndTenThousand(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "lobby", "text")
	a.register(admin, "max")
	a.send(admin, ch, "warm-up")

	small := a.writesDuring(func() { a.send(admin, ch, "to a small community") })
	smallLatency := a.timedSends(admin, ch, 30)

	a.seedMembers(scaleMembers, "member")
	large := a.writesDuring(func() { a.send(admin, ch, "to ten thousand members") })
	largeLatency := a.timedSends(admin, ch, 30)

	t.Logf("table writes per send, 2 members: %v", small)
	t.Logf("table writes per send, %d members: %v", scaleMembers+2, large)
	t.Logf("median send latency (REST, create to response), 2 members: %v, %d members: %v", smallLatency, scaleMembers+2, largeLatency)
	if fmt.Sprint(small) != fmt.Sprint(large) {
		t.Fatalf("a send writes different rows with more members: %v vs %v", small, large)
	}
	if large["channel_reads"] != 0 {
		t.Fatalf("a send wrote %d read markers", large["channel_reads"])
	}
	var total int64
	for _, n := range large {
		total += n
	}
	if total > 8 {
		t.Fatalf("a send wrote %d rows, want a handful: %v", total, large)
	}
}

func TestReadMarkersAreWrittenOnceAndOnlyWhenThereIsSomethingNewToRead(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "lobby", "text")
	a.seedMembers(scaleMembers, "member")
	a.send(admin, ch, "news")

	ids := a.memberIDs()
	markAll := func() time.Duration {
		start := time.Now()
		var wg sync.WaitGroup
		work := make(chan uuid.UUID)
		for w := 0; w < 32; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for id := range work {
					if _, err := chat.MarkRead(context.Background(), a.db, id, ch, nil); err != nil {
						t.Error(err)
						return
					}
				}
			}()
		}
		for _, id := range ids {
			work <- id
		}
		close(work)
		wg.Wait()
		return time.Since(start)
	}

	var first time.Duration
	firstWrites := a.writesDuring(func() { first = markAll() })
	var again time.Duration
	againWrites := a.writesDuring(func() { again = markAll() })

	t.Logf("%d members read a channel: %v in total (%v per member), writes %v", len(ids), first, first/time.Duration(len(ids)), firstWrites)
	t.Logf("the same %d members read it again: %v in total, writes %v", len(ids), again, againWrites)
	if got := firstWrites["channel_reads"]; got != int64(len(ids)) {
		t.Fatalf("first read wrote %d markers, want one per member (%d)", got, len(ids))
	}
	if got := againWrites["channel_reads"]; got != 0 {
		t.Fatalf("reading an already read channel wrote %d markers", got)
	}
}

func (a *app) memberIDs() []uuid.UUID {
	a.t.Helper()
	rows, err := a.db.Query(context.Background(), `SELECT id FROM users WHERE username LIKE 'member%'`)
	if err != nil {
		a.t.Fatal(err)
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			a.t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

// explain returns the plan of a query with its real timings.
func (a *app) explain(sql string, args ...any) string {
	a.t.Helper()
	rows, err := a.db.Query(context.Background(), "EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, TIMING OFF) "+sql, args...)
	if err != nil {
		a.t.Fatal(err)
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			a.t.Fatal(err)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func TestUnreadLookupsUseTheChannelIndexInAHugeChannel(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "lobby", "text")
	max := a.register(admin, "max")
	const history = 50000
	a.runSQL(`INSERT INTO messages (channel_id, user_id, content, created_at)
	        SELECT $1, $2, 'm' || g, NOW() - interval '30 days' + g * interval '1 second' FROM generate_series(1, $3) g`,
		ch, admin.user.ID, history)
	a.runSQL(`ANALYZE messages`)

	// The member read up to 10 minutes before the end.
	a.runSQL(`INSERT INTO channel_reads (user_id, channel_id, last_read_at, notify_level)
	        SELECT $1, $2, max(created_at) - interval '10 minutes', 'inherit' FROM messages WHERE channel_id = $2`, max.user.ID, ch)

	start := time.Now()
	state := max.readState(t, ch)
	t.Logf("GET /api/read-state in a channel of %d messages, 600 unread: %v (unread=%d)", history, time.Since(start), state.UnreadCount)
	if state.UnreadCount != 600 {
		t.Fatalf("unread = %d, want 600", state.UnreadCount)
	}

	plan := a.explain(`SELECT id FROM messages
		WHERE channel_id = $1 AND parent_id IS NULL AND user_id <> $2
		  AND created_at > (SELECT last_read_at FROM channel_reads WHERE user_id = $2 AND channel_id = $1)
		ORDER BY created_at, id LIMIT 1`, ch, max.user.ID)
	t.Logf("first-unread plan:\n%s", plan)
	if strings.Contains(plan, "Seq Scan on messages") {
		t.Fatalf("first-unread scans the whole channel:\n%s", plan)
	}

	// A member who has been away for the whole history.
	a.runSQL(`DELETE FROM channel_reads WHERE user_id = $1`, max.user.ID)
	a.runSQL(`UPDATE users SET created_at = NOW() - interval '60 days' WHERE id = $1`, max.user.ID)
	start = time.Now()
	state = max.readState(t, ch)
	t.Logf("GET /api/read-state, %d unread: %v (unread=%d)", history, time.Since(start), state.UnreadCount)
	if state.UnreadCount != history {
		t.Fatalf("unread = %d, want %d", state.UnreadCount, history)
	}
}

// @all is the one send that writes per recipient: one mention row for every
// member, in a single statement, and only for members who may use @all.
func TestMentioningEveryoneIsOneStatementWithOneRowPerMember(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "lobby", "text")
	a.seedMembers(scaleMembers, "member")

	var took time.Duration
	written := a.writesDuring(func() {
		start := time.Now()
		a.send(admin, ch, "@all please read this")
		took = time.Since(start)
	})
	t.Logf("a message with @all to %d members: %v, writes %v", scaleMembers+1, took, written)
	if got := written["message_mentions"]; got != scaleMembers {
		t.Fatalf("mention rows = %d, want %d", got, scaleMembers)
	}
	if written["channel_reads"] != 0 {
		t.Fatalf("@all wrote read markers: %v", written)
	}
}
