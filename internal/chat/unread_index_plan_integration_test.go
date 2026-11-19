//go:build integration

package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/mnema-xyz/talk/internal/chat/core"
	"github.com/mnema-xyz/talk/internal/chat/readstate"
	"github.com/mnema-xyz/talk/internal/testutil"
)

// messageScans lists the scans of the messages table in a plan by alias.
func (n planNode) messageScans(alias string) []planNode {
	var out []planNode
	if n.Relation == "messages" && n.Alias == alias {
		out = append(out, n)
	}
	for _, c := range n.Plans {
		out = append(out, c.messageScans(alias)...)
	}
	return out
}

// The unread summary read every unread message from the table for its
// mention counts (the index lacked messages.id): 0.66 s for a member away
// from most channels of a million-message community, 0.11 s from the index. Under both the custom and the generic plan the
// messages since the marker must come from messages_channel_unread_id_idx
// alone (migration 0112), in one pass for all three counts.
func TestUnreadSummaryReadsTheBacklogFromTheIndexAlone(t *testing.T) {
	me := seedUnreadWorld(t)
	p := testutil.DB(t)
	ctx := context.Background()
	// A few mentions, and a vacuum as autovacuum would have run on a
	// community this size (index-only scans need the visibility map).
	if _, err := p.Exec(ctx, `
		INSERT INTO message_mentions (message_id, user_id, kind)
		SELECT id, $1, 0 FROM messages WHERE content LIKE '%7' AND user_id <> $1`, me); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Exec(ctx, `VACUUM ANALYZE messages`); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"force_custom_plan", "force_generic_plan"} {
		t.Run(mode, func(t *testing.T) {
			conn, err := p.Acquire(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Release()
			for _, stmt := range []string{"SET plan_cache_mode = " + mode, "PREPARE unread_index(uuid, smallint) AS " + readstate.ReadStateSQL()} {
				if _, err := conn.Exec(ctx, stmt); err != nil {
					t.Fatalf("%s: %v", stmt, err)
				}
			}
			defer conn.Exec(ctx, "DEALLOCATE unread_index")
			var raw []byte
			if err := conn.QueryRow(ctx, fmt.Sprintf("EXPLAIN (FORMAT JSON) EXECUTE unread_index('%s', %d)", me, core.MentionKindGroup)).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			var plan []struct{ Plan planNode }
			if err := json.Unmarshal(raw, &plan); err != nil {
				t.Fatal(err)
			}
			scans := plan[0].Plan.messageScans("msg")
			if len(scans) != 1 {
				t.Fatalf("the backlog is read %d times, want once:\n%s", len(scans), raw)
			}
			if s := scans[0]; s.NodeType != "Index Only Scan" || s.Index != "messages_channel_unread_id_idx" {
				t.Fatalf("the backlog is read by %s on %q, want an Index Only Scan on messages_channel_unread_id_idx:\n%s", s.NodeType, s.Index, raw)
			}
		})
	}
}

// The member list leaves out what a member wrote in channels the viewer
// cannot see. That count read every message of those channels from the table
// (40 to 60 ms for 300,000 messages on each load of the list); the index of
// migration 0112 holds user_id and counted, so it reads the index alone.
func TestHiddenMessageTotalsAreCountedFromTheIndexAlone(t *testing.T) {
	seedUnreadWorld(t)
	p := testutil.DB(t)
	ctx := context.Background()
	if _, err := p.Exec(ctx, `VACUUM ANALYZE messages`); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err := p.QueryRow(ctx, `EXPLAIN (FORMAT JSON) `+messageCountsByAuthorSQL,
		`{`+strings.Join(channelIDs(t, 3), ",")+`}`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var plan []struct{ Plan planNode }
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	scans := plan[0].Plan.messageScans("messages")
	if len(scans) != 1 || scans[0].NodeType != "Index Only Scan" || scans[0].Index != "messages_channel_unread_id_idx" {
		t.Fatalf("hidden totals read messages by %+v, want one Index Only Scan on messages_channel_unread_id_idx:\n%s", scans, raw)
	}
}

// channelIDs returns n text channels of seedUnreadWorld.
func channelIDs(t *testing.T, n int) []string {
	t.Helper()
	rows, err := testutil.DB(t).Query(context.Background(), `SELECT id::text FROM channels WHERE type = 'text' ORDER BY name LIMIT $1`, n)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return ids
}
