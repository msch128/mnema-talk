//go:build integration

package server

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestExportSurvivesABannerFileMissingFromStorage(t *testing.T) {
	a, _ := mailApp(t)
	alice := a.register(a.seedAdmin(), "alice")
	if r := putBanner(alice, "b.png", "image/png", bannerPNG(t, 900, 300)); r.status != http.StatusOK {
		t.Fatalf("banner: %d %s", r.status, r.body)
	}
	var key string
	if err := a.db.QueryRow(context.Background(), `SELECT s3_key FROM media WHERE uploader_id = $1`, alice.user.ID).Scan(&key); err != nil {
		t.Fatal(err)
	}
	if err := a.store.Delete(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	if res := a.requestExport(alice, memberPassword, ""); res.status != http.StatusAccepted {
		t.Fatalf("export: %d %s", res.status, res.body)
	}
	if st := a.waitForExport(alice); st.State != "ready" {
		t.Fatalf("export state: %+v", st)
	}
	files := unzipAll(t, alice.get("/api/users/me/export/download").body)
	if strings.Contains(files["profile.json"], "banner_file") {
		t.Errorf("no file, no name for it: %s", files["profile.json"])
	}
	for name := range files {
		if strings.HasPrefix(name, "banner") {
			t.Errorf("unexpected %s in the archive", name)
		}
	}
}
