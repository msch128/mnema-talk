//go:build integration

package media

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm/permtest"
)

// An attachment belongs to its channel; when the permission state cannot be
// read the download fails instead of being served.
func TestServeFailsClosedWithoutPermissionState(t *testing.T) {
	f := mediaDB(t)
	id := f.attachment(t, "uploads/2026/10/guarded.txt", false)
	f.h.Perms = permtest.Unloadable()
	r := f.req(httptest.NewRequest(http.MethodGet, "/", nil), "id", id.String())
	err := f.h.serve(httptest.NewRecorder(), r)
	if err == nil {
		t.Fatal("the file was served without a permission state")
	}
	if _, refusal := httpx.AsAPIError(err); refusal {
		t.Fatalf("a load failure must not look like a refusal: %v", err)
	}
}

// Avatars have no channel and stay available to every member.
func TestAvatarsNeedNoChannelAccess(t *testing.T) {
	f := mediaDB(t)
	id := f.attachment(t, "avatars/2026/10/me.txt", true)
	f.h.Perms = permtest.Unloadable() // never consulted for a file without a channel
	r := f.req(httptest.NewRequest(http.MethodGet, "/", nil), "id", id.String())
	// attachment() records the avatar row with its channel too; clear it like an upload would.
	if _, err := f.p.Exec(r.Context(), `UPDATE media SET channel_id = NULL WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if err := f.h.serve(httptest.NewRecorder(), r); err != nil {
		t.Fatalf("avatar: %v", err)
	}
}
