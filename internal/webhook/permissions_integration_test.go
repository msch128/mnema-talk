//go:build integration

package webhook

import (
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/perm/permtest"
)

// A permission model that cannot load fails closed on every route.
func TestFailsClosedWithoutPermissionState(t *testing.T) {
	f := newFixture(t)
	rec, _ := f.newHook("hook")
	f.h.Perms = permtest.Unloadable()
	admin := "/admin/webhooks/" + rec.ID.String()
	f.owner = &auth.User{ID: uuid.New(), Role: "user"}
	for name, w := range map[string]*httptest.ResponseRecorder{
		"list":       f.serve(f.h, "GET", "/admin/channels/"+f.ch.String()+"/webhooks", nil, nil),
		"create":     f.json(f.h, "POST", "/admin/channels/"+f.ch.String()+"/webhooks", `{"name":"n"}`),
		"rename":     f.json(f.h, "PATCH", admin, `{"name":"n"}`),
		"regenerate": f.serve(f.h, "POST", admin+"/token", nil, nil),
		"delete":     f.serve(f.h, "DELETE", admin, nil, nil),
	} {
		wantStatus(t, w, 500, name)
	}
	// Executing needs the creator's permissions for @everyone only; the
	// default policy asks the model to filter who can see the channel.
	w := f.json(f.h, "POST", "/webhooks/"+rec.ID.String()+"/"+"x", `{"content":"hi"}`)
	wantStatus(t, w, 401, "wrong token")
}
