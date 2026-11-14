//go:build integration

package media

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
)

func TestMediaHandlersWithoutStorage(t *testing.T) {
	h := &Handler{}
	for name, handler := range map[string]func(http.ResponseWriter, *http.Request) error{"upload": h.upload, "avatar": h.uploadAvatar, "serve": h.serve, "prune": h.prune, "delete": h.delete, "orphans": h.orphans, "cleanup": h.cleanupOrphans, "bot": h.botUpload} {
		t.Run(name, func(t *testing.T) {
			route := h.requireStore(httpx.Handle(handler))
			if name == "bot" {
				route = httpx.Handle(handler)
			}
			rec := httptest.NewRecorder()
			route.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("status %d, want 503", rec.Code)
			}
		})
	}
}

func TestMountedMediaRoutesServeUploadAndAdminLifecycle(t *testing.T) {
	f := mediaDB(t)
	old := f.attachment(t, "uploads/old", false)
	f.attachment(t, "avatars/retained", true)
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), f.user)))
		})
	})
	f.h.MountUploads(r)
	f.h.Mount(r)
	r.Route("/admin", f.h.MountAdmin)
	call := func(method, path string, req *http.Request) *httptest.ResponseRecorder {
		if req == nil {
			req = httptest.NewRequest(method, path, nil)
		}
		req.Method = method
		req.URL.Path = strings.TrimPrefix(strings.Split(path, "?")[0], "/api")
		if strings.Contains(path, "?") {
			req.URL.RawQuery = strings.SplitN(path, "?", 2)[1]
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	if got := f.h.UploadBodyLimit(); got != 1024+1<<20 {
		t.Fatalf("body ceiling=%d", got)
	}
	w := call(http.MethodPost, "/channels/"+f.channel.String()+"/upload", uploadRequest(t, formPart{name: "file", filename: "notes.txt", mime: "text/plain", value: "notes"}, formPart{name: "content", value: "caption"}))
	if w.Code != 201 {
		t.Fatalf("upload=%d %s", w.Code, w.Body.String())
	}
	var msg struct {
		ID          uuid.UUID `json:"id"`
		Attachments []struct {
			ID  uuid.UUID `json:"id"`
			URL string    `json:"url"`
		} `json:"attachments"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &msg); err != nil || len(msg.Attachments) != 1 {
		t.Fatalf("attachment response=%s %v", w.Body.String(), err)
	}
	id := msg.Attachments[0].ID
	w = call(http.MethodGet, msg.Attachments[0].URL, nil)
	if w.Code != 200 || w.Body.String() != "notes" || !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment") || w.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(w.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("served=%d %q headers=%v", w.Code, w.Body.String(), w.Header())
	}
	req := httptest.NewRequest(http.MethodGet, msg.Attachments[0].URL, nil)
	req.Header.Set("If-None-Match", w.Header().Get("ETag"))
	if w := call(http.MethodGet, msg.Attachments[0].URL, req); w.Code != 304 {
		t.Fatalf("conditional GET=%d", w.Code)
	}
	w = call(http.MethodGet, "/admin/media/stats", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"total_files":3`) {
		t.Fatalf("stats=%d %s", w.Code, w.Body.String())
	}
	w = call(http.MethodGet, "/admin/media", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), id.String()) {
		t.Fatalf("list=%d %s", w.Code, w.Body.String())
	}
	w = call(http.MethodPost, "/admin/media/prune?days=1", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"pruned_count":1`) || f.store.Has("uploads/old") || !f.store.Has("avatars/retained") {
		t.Fatalf("prune=%d %s", w.Code, w.Body.String())
	}
	if w := call(http.MethodGet, "/media/"+old.String(), nil); w.Code != 410 {
		t.Fatalf("pruned attachment=%d", w.Code)
	}
	f.store.Put("avatars/orphan", []byte("old"), time.Now().Add(-72*time.Hour))
	w = call(http.MethodGet, "/admin/media/orphans", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"count":1`) {
		t.Fatalf("orphan count=%d %s", w.Code, w.Body.String())
	}
	w = call(http.MethodPost, "/admin/media/orphans/cleanup", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"deleted":1`) || f.store.Has("avatars/orphan") {
		t.Fatalf("orphan cleanup=%d %s", w.Code, w.Body.String())
	}
	if w := call(http.MethodDelete, "/admin/media/"+id.String(), nil); w.Code != 204 {
		t.Fatalf("delete=%d %s", w.Code, w.Body.String())
	}
	if w := call(http.MethodPost, "/users/me/avatar", uploadRequest(t, formPart{name: "avatar", filename: "a.png", mime: "image/png", value: bannerPNG(t, 8, 8)})); w.Code != 200 {
		t.Fatalf("avatar=%d %s", w.Code, w.Body.String())
	}
	recorded := f.h.Events.(*events.Recorder).Snapshot()
	// The admin delete announces the message whose file it removed.
	if len(recorded) != 3 || recorded[0].Type != "message_create" || recorded[1].Type != "message_update" || recorded[2].Type != "user_update" {
		t.Fatalf("events=%+v", recorded)
	}
	upd, ok := recorded[1].Payload.(*core.MessageUpdate)
	if !ok || upd.ID != msg.ID || recorded[1].ChannelID != f.channel || len(upd.Attachments) != 1 || !upd.Attachments[0].IsDeleted {
		t.Fatalf("message_update=%+v channel=%s", recorded[1].Payload, recorded[1].ChannelID)
	}
}
