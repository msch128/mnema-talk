//go:build integration

package auth

import (
	"context"
	"encoding/json"
	"testing"
)

func TestExportedUploadsListEveryAttachmentOfAMessageButNotUnsentOnes(t *testing.T) {
	_, pool, member, _, channel := exportFixture(t)
	seedMessages(t, pool, member.ID, channel, 1)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		INSERT INTO media (uploader_id, channel_id, message_id, s3_bucket, s3_key, original_filename, mime_type, size_bytes, position)
		SELECT $1, $2, m.id, 'b', 'uploads/e' || g, 'pic' || g || '.png', 'image/png', 10, g
		FROM messages m, generate_series(1, 3) g WHERE m.user_id = $1`, member.ID, channel); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO media (uploader_id, channel_id, s3_bucket, s3_key, original_filename, mime_type, size_bytes, staged)
		VALUES ($1, $2, 'b', 'uploads/unsent', 'unsent.png', 'image/png', 10, TRUE)`, member.ID, channel); err != nil {
		t.Fatal(err)
	}
	got, err := exportUploads(ctx, pool, member.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(got.value)
	var uploads []struct {
		Filename  string  `json:"filename"`
		MessageID *string `json:"message_id"`
	}
	if err := json.Unmarshal(raw, &uploads); err != nil {
		t.Fatal(err)
	}
	if len(uploads) != 3 {
		t.Fatalf("want the 3 sent files and no unsent one: %s", raw)
	}
	for _, u := range uploads {
		if u.MessageID == nil {
			t.Errorf("%s lost its message", u.Filename)
		}
	}
}
