//go:build integration

package db

import (
	"context"
	"testing"
)

func TestMigrationsKeepOneChannelTimeIndexOnMessages(t *testing.T) {
	p := scratchPool(t)
	ctx := context.Background()
	if err := p.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err := p.Query(ctx, `
		SELECT indexname FROM pg_indexes
		WHERE schemaname = current_schema() AND tablename = 'messages'
		  AND indexdef LIKE '%(channel_id, created_at%'
		  AND indexdef NOT LIKE '%WHERE%'
		ORDER BY indexname`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	if len(names) != 1 || names[0] != "messages_channel_unread_idx" {
		t.Fatalf("channel+time indexes on messages = %v, want only messages_channel_unread_idx", names)
	}
}
