//go:build integration

package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/media"
)

// A media row whose object is gone from storage answers 410 before any
// header is sent, instead of 200 with an empty body.
func TestServeMissingObjectIsGone(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "general", "text")
	res := admin.upload("/api/channels/"+ch.String()+"/upload", "file", "a.png", "image/png", pngBytes, nil)
	if res.status != http.StatusCreated {
		t.Fatalf("upload: %d %s", res.status, res.body)
	}
	var m chat.Message
	res.decode(t, &m)
	url := m.Attachments[0].URL

	whole := admin.get(url)
	if whole.status != http.StatusOK || string(whole.body) != string(pngBytes) {
		t.Fatalf("serve: %d, %d bytes", whole.status, len(whole.body))
	}
	part := admin.do(http.MethodGet, url, nil, map[string]string{"Range": "bytes=4-9"})
	if part.status != http.StatusPartialContent || string(part.body) != string(pngBytes[4:10]) {
		t.Fatalf("range: %d %q", part.status, part.body)
	}
	tail := admin.do(http.MethodGet, url, nil, map[string]string{"Range": "bytes=-3"})
	if tail.status != http.StatusPartialContent || string(tail.body) != string(pngBytes[len(pngBytes)-3:]) {
		t.Fatalf("suffix range: %d %q", tail.status, tail.body)
	}

	var key string
	if err := a.db.QueryRow(context.Background(), `SELECT s3_key FROM media WHERE id = $1`, m.Attachments[0].ID).Scan(&key); err != nil {
		t.Fatal(err)
	}
	if err := a.store.Delete(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	gone := admin.get(url)
	if gone.status != http.StatusGone || gone.code(t) != "NOT_FOUND" {
		t.Fatalf("missing object: %d %s", gone.status, gone.body)
	}
}

// flakyStore fails DeleteBatch from the given call on.
type flakyStore struct {
	*media.MemoryStore
	calls, failFrom int
	deleted         int
}

func (f *flakyStore) DeleteBatch(ctx context.Context, keys []string) error {
	f.calls++
	if f.calls >= f.failFrom {
		return errors.New("storage unavailable")
	}
	f.deleted += len(keys)
	return f.MemoryStore.DeleteBatch(ctx, keys)
}

// Pruning marks each batch right after its objects are deleted: a failure
// in a later batch leaves the earlier ones correctly marked.
func TestPruneMarksEachBatch(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "general", "text")
	msg := a.send(admin, ch, "carrier")
	ctx := context.Background()
	const n = 1500
	if _, err := a.db.Exec(ctx, `
		INSERT INTO media (uploader_id, channel_id, message_id, s3_bucket, s3_key, original_filename, mime_type, size_bytes, created_at)
		SELECT $1, $2, $3, 'b', 'old/' || g, 'f', 'image/png', 1, NOW() - INTERVAL '40 days' - g * INTERVAL '1 second'
		FROM generate_series(1, $4) g`, admin.user.ID, ch, msg, n); err != nil {
		t.Fatal(err)
	}
	marked := func() int {
		var k int
		if err := a.db.QueryRow(ctx, `SELECT COUNT(*) FROM media WHERE is_deleted`).Scan(&k); err != nil {
			t.Fatal(err)
		}
		return k
	}

	store := &flakyStore{MemoryStore: media.NewMemoryStore(), failFrom: 2}
	got, err := media.PruneOlderThan(ctx, a.db, store, 30)
	if err == nil || !strings.Contains(err.Error(), "storage unavailable") {
		t.Fatalf("prune with failing storage: %d, %v", got, err)
	}
	if got != store.deleted || marked() != store.deleted || store.deleted == 0 {
		t.Fatalf("pruned %d, deleted %d objects, marked %d rows", got, store.deleted, marked())
	}

	store.failFrom = 1 << 30
	rest, err := media.PruneOlderThan(ctx, a.db, store, 30)
	if err != nil || got+rest != n || marked() != n {
		t.Fatalf("second prune: %d, %v; marked %d of %d", rest, err, marked(), n)
	}
}
