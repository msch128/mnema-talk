//go:build integration

package account

import (
	"context"
	"encoding/json"
	"testing"
)

func TestExportedEarlierVersionsAreBoundedAndSayWhenCut(t *testing.T) {
	h, pool, member, store, channel := exportFixture(t)
	seedMessages(t, pool, member.ID, channel, 1)
	if _, err := pool.Exec(context.Background(), `INSERT INTO message_edits (message_id, editor_id, content, edited_at)
		SELECT m.id, m.user_id, 'version ' || g, NOW() FROM messages m, generate_series(1, $1) g WHERE m.user_id = $2`,
		exportMaxEdits+1, member.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := runExport(t, h, member.ID); err != nil {
		t.Fatal(err)
	}
	raw, ok := archiveFile(t, store, "edits.json")
	if !ok {
		t.Fatal("no edits.json")
	}
	var doc struct {
		Edits     []struct{ Content string } `json:"edits"`
		Truncated bool                       `json:"truncated"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if !doc.Truncated || len(doc.Edits) != exportMaxEdits {
		t.Fatalf("truncated=%v count=%d, want true and %d", doc.Truncated, len(doc.Edits), exportMaxEdits)
	}
}
