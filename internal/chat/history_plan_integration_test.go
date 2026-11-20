//go:build integration

package chat

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/mnema-xyz/talk/internal/chat/messages"
	"github.com/mnema-xyz/talk/internal/testutil"
)

// unboundedScans lists the scans of relation that read it without an index
// condition: the whole table, or the whole of one of its indexes.
func (n planNode) unboundedScans(relation string) []string {
	var out []string
	if n.Relation == relation && n.Cond == "" {
		out = append(out, n.NodeType)
	}
	for _, c := range n.Plans {
		out = append(out, c.unboundedScans(relation)...)
	}
	return out
}

// A history page tells for each quoted message whether it had attachments.
// As EXISTS the planner hashed the whole media table for that once per page
// (it plans for every message of the channel, not the page's 50), so every
// page read every attachment row of the community. It must look up the
// quoted message's attachments by message id instead.
func TestHistoryPageLooksUpTheQuotedMessagesAttachmentsByID(t *testing.T) {
	p := testutil.DB(t)
	testutil.Reset(t, p)
	ctx := context.Background()
	var me, ch uuid.UUID
	if err := p.QueryRow(ctx, `INSERT INTO users (username, display_name, password_hash, role) VALUES ('me', 'me', 'x', 'user') RETURNING id`).Scan(&me); err != nil {
		t.Fatal(err)
	}
	if err := p.QueryRow(ctx, `INSERT INTO channels (name, type) VALUES ('big', 'text') RETURNING id`).Scan(&ch); err != nil {
		t.Fatal(err)
	}
	// 30,000 messages, every tenth quoting the one before it, every second
	// with an attachment.
	for _, q := range []string{
		`INSERT INTO messages (channel_id, user_id, content, created_at)
		 SELECT $1, $2, 'm' || g, NOW() - ((30000 - g) || ' seconds')::interval FROM generate_series(1, 30000) g`,
		`UPDATE messages m SET reply_to_id = prev.pid
		 FROM (SELECT id, lag(id) OVER (ORDER BY created_at) pid, row_number() OVER (ORDER BY created_at) n FROM messages WHERE channel_id = $1 AND user_id = $2) prev
		 WHERE prev.id = m.id AND prev.n % 10 = 0`,
		`INSERT INTO media (uploader_id, channel_id, message_id, s3_bucket, s3_key, original_filename, mime_type, size_bytes)
		 SELECT $2, $1, id, 'b', 'k/' || id, 'f.png', 'image/png', 1 FROM messages WHERE channel_id = $1 AND number % 2 = 0`,
	} {
		if _, err := p.Exec(ctx, q, ch, me); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.Exec(ctx, `VACUUM ANALYZE`); err != nil {
		t.Fatal(err)
	}
	page := messages.MessageSelect + `
		WHERE m.channel_id = $1 AND m.parent_id IS NULL
		ORDER BY m.created_at DESC, m.id DESC
		LIMIT $2`
	for _, mode := range []string{"force_custom_plan", "force_generic_plan"} {
		t.Run(mode, func(t *testing.T) {
			conn, err := p.Acquire(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Release()
			for _, stmt := range []string{"SET plan_cache_mode = " + mode, "PREPARE history_page(uuid, int) AS " + page} {
				if _, err := conn.Exec(ctx, stmt); err != nil {
					t.Fatalf("%s: %v", stmt, err)
				}
			}
			defer conn.Exec(ctx, "DEALLOCATE history_page")
			var raw []byte
			if err := conn.QueryRow(ctx, "EXPLAIN (FORMAT JSON) EXECUTE history_page('"+ch.String()+"', 50)").Scan(&raw); err != nil {
				t.Fatal(err)
			}
			var plan []struct{ Plan planNode }
			if err := json.Unmarshal(raw, &plan); err != nil {
				t.Fatal(err)
			}
			if scans := plan[0].Plan.unboundedScans("media"); len(scans) > 0 {
				t.Fatalf("a history page reads all of media (%v):\n%s", scans, raw)
			}
		})
	}
}
