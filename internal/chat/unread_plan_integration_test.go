//go:build integration

package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/dm"
	"github.com/msch128/mnema-talk/internal/chat/readstate"
	"github.com/msch128/mnema-talk/internal/testutil"
)

// seedUnreadWorld fills 24 channels and 6 conversations with replies so the
// planner has the statistics it has on a real community.
func seedUnreadWorld(t *testing.T) (me uuid.UUID) {
	t.Helper()
	p := testutil.DB(t)
	testutil.Reset(t, p)
	ctx := context.Background()
	var other uuid.UUID
	mustScan := func(dest *uuid.UUID, q string, args ...any) {
		t.Helper()
		if err := p.QueryRow(ctx, q, args...).Scan(dest); err != nil {
			t.Fatal(err)
		}
	}
	mustScan(&me, `INSERT INTO users (username, display_name, password_hash, role, created_at) VALUES ('me', 'me', 'x', 'user', NOW() - interval '365 days') RETURNING id`)
	mustScan(&other, `INSERT INTO users (username, display_name, password_hash, role) VALUES ('other', 'other', 'x', 'user') RETURNING id`)
	for i := 0; i < 30; i++ {
		typ, key := "text", any(nil)
		if i >= 24 {
			typ, key = "dm", fmt.Sprintf("dm-%d", i)
		}
		var ch uuid.UUID
		mustScan(&ch, `INSERT INTO channels (name, type, dm_key) VALUES ($1, $2, $3) RETURNING id`, fmt.Sprintf("c%d", i), typ, key)
		if typ == "dm" {
			if _, err := p.Exec(ctx, `INSERT INTO dm_members (channel_id, user_id, joined_at) VALUES ($1, $2, NOW() - interval '30 days'), ($1, $3, NOW() - interval '30 days')`, ch, me, other); err != nil {
				t.Fatal(err)
			}
		}
		// Messages alternate between the two members; every third one replies
		// to the message 3 positions before it.
		if _, err := p.Exec(ctx, `
			INSERT INTO messages (id, channel_id, user_id, content, created_at)
			SELECT gen_random_uuid(), $1, CASE WHEN g % 2 = 0 THEN $2::uuid ELSE $3::uuid END, 'm' || g,
			       NOW() - ((4000 - g) || ' minutes')::interval
			FROM generate_series(1, 4000) g`, ch, me, other); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Exec(ctx, `
			UPDATE messages m SET reply_to_id = cur.prev_id
			FROM (SELECT id, lag(id, 3) OVER (ORDER BY created_at) AS prev_id, row_number() OVER (ORDER BY created_at) AS n
			      FROM messages WHERE channel_id = $1) cur
			WHERE m.id = cur.id AND cur.prev_id IS NOT NULL AND cur.n % 3 = 0`, ch); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.Exec(ctx, `ANALYZE`); err != nil {
		t.Fatal(err)
	}
	return me
}

type planNode struct {
	NodeType string     `json:"Node Type"`
	Relation string     `json:"Relation Name"`
	Alias    string     `json:"Alias"`
	Plans    []planNode `json:"Plans"`
}

func (n planNode) seqScansOnMessages() []string {
	var out []string
	if n.NodeType == "Seq Scan" && n.Relation == "messages" {
		out = append(out, n.Alias)
	}
	for _, c := range n.Plans {
		out = append(out, c.seqScansOnMessages()...)
	}
	return out
}

// The unread summary of every channel and the conversation list look up the
// message a reply answers. When the planner turned that lookup into a scan of
// the whole messages table (24 times, once per channel), loading the page took
// about a second on a community with a few hundred thousand messages. Under
// both the custom and the generic plan it must stay a lookup by primary key.
func TestUnreadSummariesNeverScanAllMessagesToFindARepliedToMessage(t *testing.T) {
	me := seedUnreadWorld(t)
	p := testutil.DB(t)
	ctx := context.Background()
	queries := map[string]struct {
		sql  string
		decl string
		call string
	}{
		"read states":   {readstate.ReadStateSQL(), "uuid, smallint", fmt.Sprintf("('%s', %d)", me, core.MentionKindGroup)},
		"conversations": {dm.DmSelect, "uuid, uuid", fmt.Sprintf("('%s', NULL)", me)},
	}
	for name, q := range queries {
		for _, mode := range []string{"force_custom_plan", "force_generic_plan"} {
			t.Run(name+"/"+mode, func(t *testing.T) {
				conn, err := p.Acquire(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Release()
				for _, stmt := range []string{"SET plan_cache_mode = " + mode, "SET random_page_cost = 100", "PREPARE unread_plan(" + q.decl + ") AS " + q.sql} {
					if _, err := conn.Exec(ctx, stmt); err != nil {
						t.Fatalf("%s: %v", stmt, err)
					}
				}
				defer conn.Exec(ctx, "DEALLOCATE unread_plan")
				var raw []byte
				if err := conn.QueryRow(ctx, "EXPLAIN (FORMAT JSON) EXECUTE unread_plan"+q.call).Scan(&raw); err != nil {
					t.Fatal(err)
				}
				var plan []struct{ Plan planNode }
				if err := json.Unmarshal(raw, &plan); err != nil {
					t.Fatal(err)
				}
				for _, alias := range plan[0].Plan.seqScansOnMessages() {
					if alias == "orig" {
						t.Errorf("the replied-to message is found by a Seq Scan on messages:\n%s", raw)
					}
				}
			})
		}
	}
}
