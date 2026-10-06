//go:build integration

package server

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/media"
)

func TestAdminMediaDashboard(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	member := a.register(admin, "max")
	ch := a.createChannel(admin, "medien", "text")

	var ids []uuid.UUID
	for _, name := range []string{"one.png", "two.png", "three.png"} {
		res := admin.upload("/api/channels/"+ch.String()+"/upload", "file", name, "image/png", pngBytes, nil)
		if res.status != http.StatusCreated {
			t.Fatalf("upload %s: %d %s", name, res.status, res.body)
		}
		var msg uploaded
		res.decode(t, &msg)
		ids = append(ids, msg.Attachments[0].ID)
	}
	if r := admin.upload("/api/users/me/avatar", "avatar", "me.png", "image/png", pngBytes, nil); r.status != http.StatusOK {
		t.Fatalf("avatar: %d %s", r.status, r.body)
	}

	if res := member.get("/api/admin/media"); res.status != http.StatusForbidden {
		t.Fatalf("member reads dashboard: %d", res.status)
	}

	var stats media.Stats
	admin.get("/api/admin/media/stats").decode(t, &stats)
	if stats.TotalFiles != 4 || stats.TotalSizeBytes != 4*int64(len(pngBytes)) || stats.DeletedFiles != 0 {
		t.Fatalf("stats: %+v", stats)
	}

	var all []media.DashboardItem
	admin.get("/api/admin/media").decode(t, &all)
	if len(all) != 4 {
		t.Fatalf("list: %d items", len(all))
	}
	var page []media.DashboardItem
	admin.get("/api/admin/media?limit=2&offset=1").decode(t, &page)
	if len(page) != 2 || page[0].ID != all[1].ID || page[1].ID != all[2].ID {
		t.Fatalf("page 2 of 2 does not continue the list: %+v", page)
	}
	admin.get("/api/admin/media?offset=-5&limit=1").decode(t, &page)
	if len(page) != 1 || page[0].ID != all[0].ID {
		t.Fatalf("negative offset not clamped: %+v", page)
	}
	for _, it := range all {
		if it.UploaderName == "" || it.URL != "/api/media/"+it.ID.String() {
			t.Fatalf("item missing uploader or url: %+v", it)
		}
		if it.ChannelID != nil && it.ChannelName != "medien" {
			t.Fatalf("attachment without channel name: %+v", it)
		}
	}

	victim := ids[0].String()
	if res := admin.delete("/api/admin/media/" + victim); res.status != http.StatusNoContent {
		t.Fatalf("delete: %d %s", res.status, res.body)
	}
	if res := admin.delete("/api/admin/media/" + victim); res.status != http.StatusNoContent {
		t.Fatalf("deleting again is a no-op: %d %s", res.status, res.body)
	}
	if res := admin.delete("/api/admin/media/" + uuid.NewString()); res.status != http.StatusNotFound {
		t.Fatalf("delete unknown: %d", res.status)
	}
	if res := admin.delete("/api/admin/media/nope"); res.status != http.StatusBadRequest {
		t.Fatalf("delete bad id: %d", res.status)
	}
	if res := admin.get("/api/media/" + victim); res.status != http.StatusGone {
		t.Fatalf("deleted media still served: %d", res.status)
	}
	if a.store.Len() != 3 {
		t.Fatalf("object not removed from storage: %d left", a.store.Len())
	}
	admin.get("/api/admin/media/stats").decode(t, &stats)
	if stats.TotalFiles != 3 || stats.DeletedFiles != 1 {
		t.Fatalf("stats after delete: %+v", stats)
	}
}

func TestPruneRemovesOldAttachmentsButNeverAvatars(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "medien", "text")
	ctx := context.Background()

	for _, name := range []string{"old.png", "new.png"} {
		if r := admin.upload("/api/channels/"+ch.String()+"/upload", "file", name, "image/png", pngBytes, nil); r.status != http.StatusCreated {
			t.Fatalf("upload: %d %s", r.status, r.body)
		}
	}
	if r := admin.upload("/api/users/me/avatar", "avatar", "me.png", "image/png", pngBytes, nil); r.status != http.StatusOK {
		t.Fatalf("avatar: %d %s", r.status, r.body)
	}
	if _, err := a.db.Exec(ctx, `UPDATE media SET created_at = NOW() - INTERVAL '40 days' WHERE original_filename IN ('old.png', 'me.png')`); err != nil {
		t.Fatal(err)
	}

	for _, q := range []string{"?days=0", "?days=3651", "?days=soon"} {
		if r := admin.post("/api/admin/media/prune"+q, nil); r.status != http.StatusBadRequest {
			t.Fatalf("prune%s: %d", q, r.status)
		}
	}
	var res media.PruneResult
	r := admin.post("/api/admin/media/prune?days=30", nil)
	r.decode(t, &res)
	if r.status != http.StatusOK || res.PrunedCount != 1 || res.CutoffDays != 30 {
		t.Fatalf("prune: %d %+v", r.status, res)
	}
	var left []string
	rows, err := a.db.Query(ctx, `SELECT original_filename FROM media WHERE NOT is_deleted ORDER BY original_filename`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		left = append(left, s)
	}
	if len(left) != 2 || left[0] != "me.png" || left[1] != "new.png" {
		t.Fatalf("kept %v, want the avatar and the fresh attachment", left)
	}
	if a.store.Len() != 2 {
		t.Fatalf("storage holds %d objects, want 2", a.store.Len())
	}
	if _, err := media.PruneOlderThan(ctx, a.db, a.store, 0); err == nil {
		t.Fatal("prune with 0 days must be refused")
	}
}

func TestConfiguredRetentionAppliesToManualPrune(t *testing.T) {
	a := newAppWithConfig(t, func(c *config.Config) { c.MediaRetentionDays = 30 })
	admin := a.seedAdmin()
	var res media.PruneResult
	r := admin.post("/api/admin/media/prune", nil)
	r.decode(t, &res)
	if r.status != http.StatusOK || res.CutoffDays != 30 {
		t.Fatalf("prune with configured retention: %d %s", r.status, r.body)
	}
}
