//go:build integration

package db

import (
	"context"
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
