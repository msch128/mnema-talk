//go:build integration

package server

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/chat"
)

// The channel in the path of an edit must be the message's channel, even for
// the message's own author.
func TestEditThroughTheWrongChannelPathIsRejected(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	general := a.createChannel(admin, "lobby", "text")
	other := a.createChannel(admin, "anderes", "text")
	id := postMessage(t, admin, general, map[string]any{"content": "original"})

	res := admin.put("/api/channels/"+other.String()+"/messages/"+id.String(), map[string]string{"content": "moved"})
	if res.status != http.StatusNotFound {
		t.Fatalf("edit through the wrong channel path: %d %s", res.status, res.body)
	}
	var content string
	if err := a.db.QueryRow(context.Background(), `SELECT content FROM messages WHERE id = $1`, id).Scan(&content); err != nil || content != "original" {
		t.Fatalf("message changed: %q %v", content, err)
	}
	if res := admin.put("/api/channels/"+general.String()+"/messages/"+id.String(), map[string]string{"content": "edited"}); res.status != http.StatusOK {
		t.Fatalf("edit through the right path: %d %s", res.status, res.body)
	}
}

// Attachments are revalidated on every use; avatars stay cacheable.
func TestMediaCacheHeaders(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "lobby", "text")

	up := admin.upload("/api/channels/"+ch.String()+"/upload", "file", "a.png", "image/png", pngBytes, nil)
	if up.status != http.StatusCreated {
		t.Fatalf("upload: %d %s", up.status, up.body)
	}
	var list []struct {
		URL string `json:"url"`
	}
	admin.get("/api/admin/media").decode(t, &list)
	if len(list) != 1 {
		t.Fatalf("media list: %+v", list)
	}
	res := admin.get(list[0].URL)
	if res.status != http.StatusOK || res.header.Get("Cache-Control") != "private, no-cache" || res.header.Get("ETag") == "" {
		t.Fatalf("attachment: %d cache=%q etag=%q", res.status, res.header.Get("Cache-Control"), res.header.Get("ETag"))
	}
	cond := admin.do(http.MethodGet, list[0].URL, nil, map[string]string{"If-None-Match": res.header.Get("ETag")})
	if cond.status != http.StatusNotModified {
		t.Fatalf("revalidation: %d", cond.status)
	}

	av := admin.upload("/api/users/me/avatar", "avatar", "me.png", "image/png", pngBytes, nil)
	if av.status != http.StatusOK {
		t.Fatalf("avatar: %d %s", av.status, av.body)
	}
	var me struct {
		AvatarURL string `json:"avatar_url"`
	}
	av.decode(t, &me)
	if me.AvatarURL == "" {
		t.Fatalf("no avatar url: %s", av.body)
	}
	res = admin.get(me.AvatarURL)
	if res.status != http.StatusOK || res.header.Get("Cache-Control") != "private, max-age=86400, immutable" {
		t.Fatalf("avatar: %d cache=%q", res.status, res.header.Get("Cache-Control"))
	}
}

// The substring search must be answerable from the trigram index.
func TestSearchUsesTheTrigramIndex(t *testing.T) {
	a := newApp(t, false)
	ctx := context.Background()
	err := pgx.BeginFunc(ctx, a.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL enable_seqscan = off`); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `EXPLAIN SELECT id FROM messages WHERE content ILIKE $1`, "%needle%")
		if err != nil {
			return err
		}
		defer rows.Close()
		var plan strings.Builder
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				return err
			}
			plan.WriteString(line + "\n")
		}
		if !strings.Contains(plan.String(), "messages_content_trgm_idx") {
			t.Errorf("plan does not use the trigram index:\n%s", plan.String())
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
}

// One user can run at most two searches at once.
func TestSearchConcurrencyIsCappedPerUser(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	max := a.register(admin, "max")
	general := a.createChannel(admin, "lobby", "text")
	postMessage(t, admin, general, map[string]any{"content": "park"})

	// Hold the user's slots the way two slow searches would.
	var releases []func()
	for i := 0; i < 2; i++ {
		release, ok := chat.AcquireSearchSlot(max.user.ID)
		if !ok {
			t.Fatal("slot unavailable")
		}
		releases = append(releases, release)
	}
	if _, status := max.search(t, map[string]string{"q": "park"}); status != http.StatusTooManyRequests {
		t.Fatalf("third concurrent search: %d, want 429", status)
	}
	if got, status := admin.search(t, map[string]string{"q": "park"}); status != http.StatusOK || len(got.Messages) != 1 {
		t.Fatalf("another user must not be affected: %d %+v", status, got)
	}
	releases[0]()
	if _, status := max.search(t, map[string]string{"q": "park"}); status != http.StatusOK {
		t.Fatalf("after a search finished: %d", status)
	}
	releases[1]()
}
