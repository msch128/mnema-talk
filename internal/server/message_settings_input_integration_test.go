//go:build integration

package server

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/chat/channels"
)

// The message settings refuse what they cannot store, and change nothing when
// they do.
func TestTheMessageSettingsRefuseBadInputAndKeepWhatWasSet(t *testing.T) {
	f := newAutoDeleteFixture(t)
	notAnObject := []int{1}
	for _, path := range []string{"/api/admin/message-settings/auto-delete", "/api/admin/message-settings/auto-delete/preview"} {
		method := http.MethodPut
		if path != "/api/admin/message-settings/auto-delete" {
			method = http.MethodPost
		}
		for name, body := range map[string]any{
			"not an object":   notAnObject,
			"too long":        map[string]any{"hours": channels.MaxAutoDeleteHours + 1},
			"negative":        map[string]any{"hours": -1},
			"an unknown kind": map[string]any{"hours": 24, "scope": "everything"},
		} {
			wantStatus(t, path+": "+name, f.owner.do(method, path, body, nil), http.StatusBadRequest)
		}
	}
	wantStatus(t, "lifetimes: not an object", f.owner.put("/api/admin/message-settings/lifetimes", notAnObject), http.StatusBadRequest)

	var view struct {
		AutoDeleteDefaultHours int  `json:"auto_delete_default_hours"`
		LifetimesEnabled       bool `json:"message_lifetimes_enabled"`
	}
	f.owner.get("/api/admin/message-settings").decode(t, &view)
	if view.AutoDeleteDefaultHours != 0 || !view.LifetimesEnabled {
		t.Fatalf("a refused request changed the settings: %+v", view)
	}
}

// A channel that follows the community default is previewed with the default;
// a conversation has no auto-delete to preview.
func TestTheChannelPreviewUsesTheDefaultAndSkipsConversations(t *testing.T) {
	f := newAutoDeleteFixture(t)
	old := f.a.send(f.alice, f.channel, "two days old")
	f.age(t, old, 2)
	wantStatus(t, "default", f.owner.put("/api/admin/message-settings/auto-delete", map[string]any{"hours": 24}), http.StatusOK)

	var counts struct{ Messages, Attachments int }
	res := f.owner.post("/api/admin/channels/"+f.channel.String()+"/auto-delete/preview", map[string]any{"use_default": true})
	wantStatus(t, "follow the default", res, http.StatusOK)
	res.decode(t, &counts)
	if counts.Messages != 1 {
		t.Fatalf("the preview of a channel following the 24 h default: %+v", counts)
	}

	dm := f.owner.mustDM(t, f.alice)
	wantStatus(t, "a conversation", f.owner.post("/api/admin/channels/"+dm.ID.String()+"/auto-delete/preview", map[string]any{"hours": 24}), http.StatusNotFound)
	wantStatus(t, "not an id", f.owner.post("/api/admin/channels/nope/auto-delete/preview", map[string]any{"hours": 24}), http.StatusBadRequest)
}

// A sweep run without a live-event publisher (a tool, a test) still removes
// the files; it only has no one to tell.
func TestAFilesOnlySweepWithoutAPublisherStillRemovesTheFiles(t *testing.T) {
	f := newAutoDeleteFixture(t)
	res := f.alice.upload(f.base()+"/upload", "file", "old.png", "image/png", pngBytes, map[string]string{"content": "kept"})
	wantStatus(t, "upload", res, http.StatusCreated)
	var m struct{ ID uuid.UUID }
	res.decode(t, &m)
	f.age(t, m.ID, 2)
	wantStatus(t, "files only", f.setHours(f.owner, f.channel, map[string]any{"hours": 24, "scope": "attachments"}), http.StatusOK)

	d := &channels.AutoDeleter{DB: f.a.db, Objects: f.a.store, Audit: &audit.Log{DB: f.a.db}}
	if err := d.Sweep(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !f.exists(m.ID) || f.a.countRows(`SELECT COUNT(*) FROM media WHERE message_id = $1 AND is_deleted`, m.ID) != 1 {
		t.Fatal("the files-only sweep did not remove the file and keep the message")
	}
}
