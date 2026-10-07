//go:build integration

package server

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestOrphanedMediaIsFoundAndOnlyRemovedOnRequest(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	max := a.register(admin, "max")
	ch := a.createChannel(admin, "general", "text")

	// A real, referenced upload ...
	if res := admin.upload("/api/channels/"+ch.String()+"/upload", "file", "a.png", "image/png", pngBytes, nil); res.status != http.StatusCreated {
		t.Fatalf("upload: %d %s", res.status, res.body)
	}
	var referenced string
	if err := a.db.QueryRow(context.Background(), `SELECT s3_key FROM media LIMIT 1`).Scan(&referenced); err != nil {
		t.Fatal(err)
	}
	// ... made old, so only the missing row decides.
	old := time.Now().Add(-48 * time.Hour)
	a.store.Put(referenced, pngBytes, old)
	// Old orphans in both key spaces, a fresh one (an upload in flight),
	// and an old object outside the app's prefixes.
	a.store.Put("uploads/2020/01/orphan.png", make([]byte, 100), old)
	a.store.Put("avatars/2020/01/orphan.png", make([]byte, 50), old)
	a.store.Put("uploads/2026/10/in-flight.png", make([]byte, 10), time.Now())
	a.store.Put("backups/other-app.bin", make([]byte, 10), old)

	if res := max.get("/api/admin/media/orphans"); res.status != http.StatusForbidden {
		t.Errorf("member reading orphans: %d, want 403", res.status)
	}
	if res := max.post("/api/admin/media/orphans/cleanup", nil); res.status != http.StatusForbidden {
		t.Errorf("member cleaning up: %d, want 403", res.status)
	}

	var found struct {
		Count, Bytes, Deleted int64
	}
	admin.get("/api/admin/media/orphans").decode(t, &found)
	if found.Count != 2 || found.Bytes != 150 || found.Deleted != 0 {
		t.Fatalf("found %+v, want 2 orphans of 150 bytes, none deleted", found)
	}
	if !a.store.Has("uploads/2020/01/orphan.png") {
		t.Fatal("counting deleted an orphan")
	}

	admin.post("/api/admin/media/orphans/cleanup", nil).decode(t, &found)
	if found.Deleted != 2 {
		t.Fatalf("cleanup %+v, want 2 deleted", found)
	}
	for key, want := range map[string]bool{
		referenced:                      true,
		"uploads/2026/10/in-flight.png": true,
		"backups/other-app.bin":         true,
		"uploads/2020/01/orphan.png":    false,
		"avatars/2020/01/orphan.png":    false,
	} {
		if a.store.Has(key) != want {
			t.Errorf("%s stored=%v, want %v", key, !want, want)
		}
	}
	// The upload still serves.
	var url string
	if err := a.db.QueryRow(context.Background(), `SELECT '/api/media/' || id FROM media LIMIT 1`).Scan(&url); err != nil {
		t.Fatal(err)
	}
	if res := admin.get(url); res.status != http.StatusOK {
		t.Errorf("referenced media after cleanup: %d", res.status)
	}
}
