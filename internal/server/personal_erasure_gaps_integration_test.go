//go:build integration

package server

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

// knownGap is the Go form of an expected failure: holds reports whether the
// property holds today. The test passes while it does not and fails the day it
// does, so the entry has to be turned into a plain assertion when the gap is
// closed.
func knownGap(t *testing.T, finding, property string, holds bool) {
	t.Helper()
	if holds {
		t.Errorf("%s is fixed (%s holds now): replace this known gap with the plain assertion", finding, property)
	}
}

// The erasure of an account leaves two tables that point at the person:
// message_nonces (one row per message sent with a retry id, kept a day) and
// upload_usage (one row per upload, kept 24 hours). SECURITY.md says
// everything else that refers to the person is removed in the same
// transaction, and that a test walks every foreign key to users; that test
// only sees rows its fixture made, and it made none here. H-L10-01.
func TestErasureLeavesRetryIdsAndUploadBookingsOfThePerson(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	ch := a.createChannel(admin, "general", "text")
	nonce := uuid.New()
	if res := alice.post("/api/channels/"+ch.String()+"/messages", map[string]any{"content": "sent with a retry id", "nonce": nonce}); res.status != http.StatusCreated {
		t.Fatalf("send: %d %s", res.status, res.body)
	}
	if res := alice.upload("/api/channels/"+ch.String()+"/attachments", "file", "a.png", "image/png", pngBytes, nil); res.status != http.StatusCreated {
		t.Fatalf("upload: %d %s", res.status, res.body)
	}
	id := alice.user.ID
	count := func() (nonces, usage int) {
		return a.countRows(`SELECT COUNT(*) FROM message_nonces WHERE user_id = $1`, id),
			a.countRows(`SELECT COUNT(*) FROM upload_usage WHERE user_id = $1`, id)
	}
	if n, u := count(); n != 1 || u != 1 {
		t.Fatalf("the fixture must make one row of each before the erasure: nonces %d, usage %d", n, u)
	}
	if res := a.deleteAccount(alice, memberPassword, ""); res.status != http.StatusNoContent {
		t.Fatalf("delete: %d %s", res.status, res.body)
	}
	var deleted bool
	if err := a.db.QueryRow(context.Background(), `SELECT deleted_at IS NOT NULL FROM users WHERE id = $1`, id).Scan(&deleted); err != nil || !deleted {
		t.Fatalf("the account must be erased: %v %v", deleted, err)
	}
	n, u := count()
	t.Logf("after the erasure: %d message_nonces rows and %d upload_usage rows still name the person", n, u)
	knownGap(t, "H-L10-01", "no message_nonces row names the erased person", n == 0)
	knownGap(t, "H-L10-01", "no upload_usage row names the erased person", u == 0)
}

// The export leaves out settings the member made and words the member wrote:
// "nobody may start a direct message with me" (dm_policy), "mute every direct
// message" (dm_mute_all, the notification level of the whole list, which
// settings.json, "notification levels per channel and category", does not
// hold), the comment mute of an event (doc/events.md: the export holds the
// comments and mute settings in event_comments.json), and the title and
// description of the events the member created. H-L10-02.
func TestExportLeavesOutDirectMessageSettingsCommentMutesAndOwnEvents(t *testing.T) {
	a, _ := mailApp(t)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	assignRoles(t, admin, alice, createRole(t, admin, "Planner", perm.ManageEvents))
	for path, body := range map[string]any{
		"/api/users/me/privacy":     map[string]string{"dm_policy": "nobody"},
		"/api/users/me/dm-settings": map[string]bool{"mute_all": true, "list_collapsed": false},
	} {
		if res := alice.put(path, body); res.status >= 300 {
			t.Fatalf("%s: %d %s", path, res.status, res.body)
		}
	}
	res := alice.post("/api/events", map[string]any{
		"title": "Alices picnic", "description": "bring the blue blanket, written by alice",
		"start_time": time.Now().Add(48 * time.Hour), "location": "the park",
	})
	if res.status != http.StatusCreated {
		t.Fatalf("event: %d %s", res.status, res.body)
	}
	var ev struct{ ID uuid.UUID }
	res.decode(t, &ev)
	if res := alice.put("/api/events/"+ev.ID.String()+"/comments/mute", map[string]bool{"muted": true}); res.status >= 300 {
		t.Fatalf("mute: %d %s", res.status, res.body)
	}
	if a.countRows(`SELECT COUNT(*) FROM users WHERE id = $1 AND dm_policy = 'nobody' AND dm_mute_all`, alice.user.ID) != 1 ||
		a.countRows(`SELECT COUNT(*) FROM event_comment_mutes WHERE user_id = $1`, alice.user.ID) != 1 {
		t.Fatal("the fixture must have stored the settings")
	}
	archive, files := a.exportArchive(alice)
	if !strings.Contains(archive, alice.user.ID.String()) || files < 20 {
		t.Fatalf("the archive is not alice's full export: %d files", files)
	}
	knownGap(t, "H-L10-02", "the export holds dm_policy", strings.Contains(archive, `"nobody"`))
	knownGap(t, "H-L10-02", "the export holds the direct message mute", strings.Contains(archive, "mute_all"))
	knownGap(t, "H-L10-02", "the export holds the event comment mute", strings.Contains(archive, ev.ID.String()))
	knownGap(t, "H-L10-02", "the export holds the events the member created", strings.Contains(archive, "blue blanket"))
}

// The erasure deletes the person's files after its transaction; when that
// delete fails, "anything that could not be removed is found by Admin > Media >
// orphan cleanup" (doc/operations.md, account deletion; the comment of
// removeObjects says the same). The finished export is one of those files, and
// it lives under exports/: the scan finds an archive no export row names, and
// leaves another member's ready export and an archive being written alone.
// The failed delete is modelled by putting the objects back after the
// erasure, as old as the scan wants them. H-L10-03.
func TestAFailedDeleteOfAnErasedExportIsFoundByTheOrphanCleanup(t *testing.T) {
	a, _ := mailApp(t)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	bob := a.register(admin, "bob")
	if res := a.requestExport(bob, memberPassword, ""); res.status != http.StatusAccepted {
		t.Fatalf("request: %d %s", res.status, res.body)
	}
	if st := a.waitForExport(bob); st.State != "ready" {
		t.Fatalf("export: %+v", st)
	}
	ch := a.createChannel(admin, "general", "text")
	if res := alice.upload("/api/channels/"+ch.String()+"/upload", "file", "a.png", "image/png", pngBytes, map[string]string{"content": "x"}); res.status != http.StatusCreated {
		t.Fatalf("upload: %d %s", res.status, res.body)
	}
	// Built and not downloaded yet: the file waits in exports/ for a day.
	if res := a.requestExport(alice, memberPassword, ""); res.status != http.StatusAccepted {
		t.Fatalf("request: %d %s", res.status, res.body)
	}
	if st := a.waitForExport(alice); st.State != "ready" {
		t.Fatalf("export: %+v", st)
	}
	ctx := context.Background()
	var exportKey, uploadKey string
	if err := a.db.QueryRow(ctx, `SELECT s3_key FROM data_exports WHERE user_id = $1 AND s3_key IS NOT NULL`, alice.user.ID).Scan(&exportKey); err != nil {
		t.Fatalf("a ready export with a file: %v", err)
	}
	if err := a.db.QueryRow(ctx, `SELECT s3_key FROM media WHERE uploader_id = $1`, alice.user.ID).Scan(&uploadKey); err != nil {
		t.Fatal(err)
	}
	if res := a.deleteAccount(alice, memberPassword, ""); res.status != http.StatusNoContent {
		t.Fatalf("delete: %d %s", res.status, res.body)
	}
	if a.store.Has(exportKey) || a.store.Has(uploadKey) {
		t.Fatal("the erasure must have deleted both files when the store works")
	}
	var bobKey string
	if err := a.db.QueryRow(ctx, `SELECT s3_key FROM data_exports WHERE user_id = $1 AND s3_key IS NOT NULL`, bob.user.ID).Scan(&bobKey); err != nil {
		t.Fatalf("bob's ready export: %v", err)
	}
	old := time.Now().Add(-48 * time.Hour)
	a.store.Put(exportKey, []byte("the person's archive"), old)
	a.store.Put(uploadKey, pngBytes, old)
	a.store.Put(bobKey, []byte("bob's archive"), old)
	writing := "exports/" + bob.user.ID.String() + "/being-written.zip"
	a.store.Put(writing, []byte("not named by its row yet"), time.Now())

	var found struct{ Count, Deleted int64 }
	admin.post("/api/admin/media/orphans/cleanup", nil).decode(t, &found)
	if a.store.Has(uploadKey) || a.store.Has(exportKey) || found.Deleted != 2 {
		t.Fatalf("the left-over upload and export must be found and removed: %+v, export stored: %v", found, a.store.Has(exportKey))
	}
	if !a.store.Has(bobKey) || !a.store.Has(writing) {
		t.Fatalf("a ready export with its row and a young archive stay: %v %v", a.store.Has(bobKey), a.store.Has(writing))
	}
}

// exportArchive builds and downloads c's export and returns every file but
// README.txt joined, and the number of files.
func (a *app) exportArchive(c *client) (string, int) {
	a.t.Helper()
	if res := a.requestExport(c, memberPassword, ""); res.status != http.StatusAccepted {
		a.t.Fatalf("request: %d %s", res.status, res.body)
	}
	if st := a.waitForExport(c); st.State != "ready" {
		a.t.Fatalf("export: %+v", st)
	}
	dl := c.get("/api/users/me/export/download")
	if dl.status != http.StatusOK {
		a.t.Fatalf("download: %d", dl.status)
	}
	var all strings.Builder
	files := unzipAll(a.t, dl.body)
	for name, body := range files {
		if name != "README.txt" {
			all.WriteString(body)
		}
	}
	return all.String(), len(files)
}
