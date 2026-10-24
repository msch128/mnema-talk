//go:build integration

package server

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/communityevents"
)

func (a *app) coverEvent(admin *client, channel *uuid.UUID) communityevents.CommunityEvent {
	a.t.Helper()
	body := map[string]any{"title": "Cover test", "start_time": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}
	if channel != nil {
		body["channel_id"] = *channel
	} else {
		body["location"] = "Hall"
	}
	res := admin.post("/api/events", body)
	wantStatus(a.t, "create event", res, http.StatusCreated)
	var ev communityevents.CommunityEvent
	res.decode(a.t, &ev)
	return ev
}

func (a *app) uploadCover(c *client, id uuid.UUID, field string, data []byte) response {
	return c.upload("/api/events/"+id.String()+"/cover", field, "cover.png", "image/png", data, nil)
}

func (a *app) liveMediaCount() int {
	a.t.Helper()
	var n int
	if err := a.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM media WHERE original_filename LIKE 'cover%' AND NOT is_deleted`).Scan(&n); err != nil {
		a.t.Fatal(err)
	}
	return n
}

func TestEventCoverUploadReplaceAndRemove(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ev := a.coverEvent(admin, nil)

	res := a.uploadCover(admin, ev.ID, "cover", bannerPNG(t, 900, 400))
	wantStatus(t, "upload", res, http.StatusOK)
	var got communityevents.CommunityEvent
	res.decode(t, &got)
	if got.CoverURL == nil || a.store.Len() != 1 || a.liveMediaCount() != 1 {
		t.Fatalf("cover=%v objects=%d media=%d", got.CoverURL, a.store.Len(), a.liveMediaCount())
	}

	// A second upload (via the alternative field name) replaces the first one.
	wantStatus(t, "replace", a.uploadCover(admin, ev.ID, "image", bannerPNG(t, 600, 600)), http.StatusOK)
	if a.store.Len() != 1 || a.liveMediaCount() != 1 {
		t.Fatalf("replacing must drop the old file: objects=%d media=%d", a.store.Len(), a.liveMediaCount())
	}

	res = admin.delete("/api/events/" + ev.ID.String() + "/cover")
	wantStatus(t, "remove", res, http.StatusOK)
	got = communityevents.CommunityEvent{}
	res.decode(t, &got)
	if got.CoverURL != nil || a.store.Len() != 0 || a.liveMediaCount() != 0 {
		t.Fatalf("cover=%v objects=%d media=%d", got.CoverURL, a.store.Len(), a.liveMediaCount())
	}
	wantStatus(t, "remove again", admin.delete("/api/events/"+ev.ID.String()+"/cover"), http.StatusOK)
}

func TestEventCoverRejections(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	member := a.register(admin, "member")
	manager := a.register(admin, "manager")
	assignRoles(t, admin, manager, a.roleWithManageEvents(admin))
	secret := a.hiddenTalk(admin, "vip-lounge")
	open := a.coverEvent(admin, nil)
	hidden := a.coverEvent(admin, &secret)
	png := bannerPNG(t, 900, 400)
	del := func(c *client, id uuid.UUID) response { return c.delete("/api/events/" + id.String() + "/cover") }

	tests := []struct {
		name string
		res  func() response
		want int
		code string
	}{
		{"member upload", func() response { return a.uploadCover(member, open.ID, "cover", png) }, http.StatusForbidden, ""},
		{"member remove", func() response { return del(member, open.ID) }, http.StatusForbidden, ""},
		{"bad id upload", func() response { return admin.post("/api/events/nope/cover", nil) }, http.StatusBadRequest, ""},
		{"bad id remove", func() response { return admin.delete("/api/events/nope/cover") }, http.StatusBadRequest, ""},
		{"unknown event upload", func() response { return a.uploadCover(admin, uuid.New(), "cover", png) }, http.StatusNotFound, ""},
		{"unknown event remove", func() response { return del(admin, uuid.New()) }, http.StatusNotFound, ""},
		{"hidden talk upload", func() response { return a.uploadCover(manager, hidden.ID, "cover", png) }, http.StatusNotFound, ""},
		{"hidden talk remove", func() response { return del(manager, hidden.ID) }, http.StatusNotFound, ""},
		{"no multipart", func() response {
			return admin.post("/api/events/"+open.ID.String()+"/cover", map[string]string{"a": "b"})
		}, http.StatusBadRequest, ""},
		{"wrong field", func() response { return a.uploadCover(admin, open.ID, "file", png) }, http.StatusBadRequest, ""},
		{"not an image", func() response { return a.uploadCover(admin, open.ID, "cover", []byte("this is text")) }, http.StatusBadRequest, ""},
		{"over 5 MB", func() response { return a.uploadCover(admin, open.ID, "cover", make([]byte, 5<<20+512<<10)) }, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE"},
		{"way over 5 MB", func() response { return a.uploadCover(admin, open.ID, "cover", make([]byte, 7<<20)) }, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE"},
	}
	for _, tc := range tests {
		res := tc.res()
		wantStatus(t, tc.name, res, tc.want)
		if tc.code != "" && res.code(t) != tc.code {
			t.Errorf("%s: code %q, want %q", tc.name, res.code(t), tc.code)
		}
	}
	if a.store.Len() != 0 || a.liveMediaCount() != 0 {
		t.Fatalf("rejected uploads must store nothing: objects=%d media=%d", a.store.Len(), a.liveMediaCount())
	}
}

func TestEventCoverKeepsTheImageFormat(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ev := a.coverEvent(admin, nil)
	tests := []struct {
		name, mime string
		data       []byte
	}{
		{"jpeg", "image/jpeg", bannerJPEGWithEXIF(t, "Camera")},
		{"gif", "image/gif", bannerGIF(t, 600, 200, 2)},
	}
	for _, tc := range tests {
		wantStatus(t, tc.name, a.uploadCover(admin, ev.ID, "cover", tc.data), http.StatusOK)
		var stored string
		if err := a.db.QueryRow(context.Background(), `SELECT mime_type FROM media WHERE original_filename LIKE 'cover%' AND NOT is_deleted`).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if stored != tc.mime {
			t.Errorf("%s: stored as %s", tc.name, stored)
		}
	}
}
