//go:build integration

package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

var (
	pngBytes = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")
	xssHTML  = []byte("<!DOCTYPE html><html><body><script>fetch('/api/auth/me')</script></body></html>")
	xssSVG   = []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
)

type uploaded struct {
	ID          uuid.UUID
	Attachments []struct {
		ID  uuid.UUID
		URL string
	}
}

func TestUploadRejectsDisguisedHTML(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "medien", "text")

	for _, ct := range []string{"image/png", "text/html", "text/plain"} {
		res := admin.upload("/api/channels/"+ch.String()+"/upload", "file", "cat.png", ct, xssHTML, nil)
		if res.status != http.StatusUnsupportedMediaType {
			t.Errorf("HTML declared as %s: %d %s", ct, res.status, res.body)
		}
	}
	if a.store.Len() != 0 {
		t.Fatal("rejected uploads must not reach storage")
	}
}

func TestUploadedMediaIsServedSafely(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "medien", "text")

	res := admin.upload("/api/channels/"+ch.String()+"/upload", "file", "../../cat.png", "image/png", pngBytes, map[string]string{"content": "look"})
	if res.status != http.StatusCreated {
		t.Fatalf("png upload: %d %s", res.status, res.body)
	}
	var msg uploaded
	res.decode(t, &msg)
	img := admin.get(msg.Attachments[0].URL)
	if img.status != http.StatusOK || img.header.Get("Content-Type") != "image/png" {
		t.Fatalf("serve png: %d %s", img.status, img.header.Get("Content-Type"))
	}
	if !strings.HasPrefix(img.header.Get("Content-Disposition"), "inline") || strings.Contains(img.header.Get("Content-Disposition"), "..") {
		t.Errorf("disposition %q", img.header.Get("Content-Disposition"))
	}
	if img.header.Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(img.header.Get("Content-Security-Policy"), "sandbox") {
		t.Error("media must be served with nosniff and a sandbox CSP")
	}

	svg := admin.upload("/api/channels/"+ch.String()+"/upload", "file", "x.svg", "image/svg+xml", xssSVG, nil)
	if svg.status != http.StatusCreated {
		t.Fatalf("svg upload: %d %s", svg.status, svg.body)
	}
	var svgMsg uploaded
	svg.decode(t, &svgMsg)
	served := admin.get(svgMsg.Attachments[0].URL)
	if !strings.HasPrefix(served.header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("svg must be download-only, got %q", served.header.Get("Content-Disposition"))
	}

	if anon := a.anon().get(msg.Attachments[0].URL); anon.status != http.StatusUnauthorized {
		t.Fatalf("media without session: %d", anon.status)
	}
}

func TestAvatarUploadReplacesPrevious(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()

	if r := admin.upload("/api/users/me/avatar", "avatar", "me.svg", "image/svg+xml", xssSVG, nil); r.status != http.StatusUnsupportedMediaType {
		t.Fatalf("svg avatar: %d", r.status)
	}
	if r := admin.upload("/api/users/me/avatar", "avatar", "a.png", "image/png", pngBytes, nil); r.status != http.StatusOK {
		t.Fatalf("first avatar: %d %s", r.status, r.body)
	}
	if r := admin.upload("/api/users/me/avatar", "avatar", "b.png", "image/png", pngBytes, nil); r.status != http.StatusOK {
		t.Fatalf("second avatar: %d %s", r.status, r.body)
	}
	if a.store.Len() != 1 {
		t.Fatalf("old avatar should be removed from storage, %d objects left", a.store.Len())
	}
}

func TestPruneIsOptIn(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "medien", "text")
	admin.upload("/api/channels/"+ch.String()+"/upload", "file", "a.png", "image/png", pngBytes, nil)

	// MEDIA_RETENTION_DAYS=0: a prune without an explicit day count does nothing.
	if r := admin.post("/api/admin/media/prune", nil); r.status != http.StatusBadRequest {
		t.Fatalf("implicit prune: %d %s", r.status, r.body)
	}
	if r := admin.post("/api/admin/media/prune?days=1", nil); r.status != http.StatusOK || !strings.Contains(string(r.body), `"pruned_count":0`) {
		t.Fatalf("fresh media must survive a 1-day prune: %d %s", r.status, r.body)
	}
	if a.store.Len() != 1 {
		t.Fatal("media was deleted")
	}
}
