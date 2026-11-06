//go:build integration

package db

import (
	"context"
	"strings"
	"testing"
)

// Deleting a message makes Postgres look up its rows in every referencing
// table. Without an index on the referencing column that lookup scans the whole
// table once per deleted message, so retention and channel deletes slow down
// quadratically as saved messages, follows and reminders pile up.
func TestEveryTableReferencingMessagesIsIndexedOnTheReference(t *testing.T) {
	p := scratchPool(t)
	ctx := context.Background()
	if err := p.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	// An index is usable for the foreign key check when the column leads it and
	// it is either complete or limited to the non-null values the check asks for.
	rows, err := p.Query(ctx, `
		SELECT c.conrelid::regclass::text, a.attname
		FROM pg_constraint c
		JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = c.conkey[1]
		WHERE c.contype = 'f' AND c.confrelid = 'messages'::regclass
		  AND NOT EXISTS (
		    SELECT 1 FROM pg_index i
		    WHERE i.indrelid = c.conrelid AND i.indkey[0] = c.conkey[1]
		      AND (i.indpred IS NULL OR pg_get_expr(i.indpred, i.indrelid) = '(' || a.attname || ' IS NOT NULL)'))
		ORDER BY 1, 2`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var table, column string
		if err := rows.Scan(&table, &column); err != nil {
			t.Fatal(err)
		}
		t.Errorf("%s.%s references messages but no index leads with it", table, column)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

// Deleting a user, channel, poll or media row cascades or nulls the rows that
// reference it, and Postgres finds them through the referencing column. These
// columns had no index leading with them, so each such delete scanned the whole
// referencing table.
func TestCascadingForeignKeysAreIndexedOnTheReferencingColumn(t *testing.T) {
	p := scratchPool(t)
	ctx := context.Background()
	if err := p.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, c := range [][2]string{
		{"poll_anon_ballots", "poll_id"},
		{"channel_reads", "channel_id"},
		{"message_reactions", "user_id"},
		{"messages", "webhook_id"},
		{"messages", "pinned_by"},
		{"messages", "user_id"},
		{"events", "cover_media_id"},
		{"scheduled_messages", "channel_id"},
	} {
		var ok bool
		err := p.QueryRow(ctx, `
			SELECT EXISTS (
			  SELECT 1 FROM pg_index i
			  WHERE i.indrelid = $1::regclass
			    AND i.indkey[0] = (SELECT attnum FROM pg_attribute WHERE attrelid = $1::regclass AND attname = $2)
			    AND (i.indpred IS NULL OR pg_get_expr(i.indpred, i.indrelid) = '(' || $2 || ' IS NOT NULL)'))`,
			c[0], c[1]).Scan(&ok)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Errorf("%s.%s has no index that leads with it", c[0], c[1])
		}
	}
}

// messages.user_id is looked up by the inbox, the from: search filter, the
// account export and erasure and every user delete. It must lead its own index,
// plain: a (user_id, created_at) index tempts the direct message list into a
// merge anti join that takes seconds.
func TestMessagesUserIdLeadsAPlainIndex(t *testing.T) {
	p := scratchPool(t)
	ctx := context.Background()
	if err := p.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var def string
	err := p.QueryRow(ctx, `SELECT indexdef FROM pg_indexes WHERE tablename = 'messages' AND indexname = 'messages_user_idx'`).Scan(&def)
	if err != nil {
		t.Fatalf("messages_user_idx missing: %v", err)
	}
	if !strings.HasSuffix(def, "(user_id)") {
		t.Errorf("messages_user_idx = %s, want a plain (user_id) index", def)
	}
}
