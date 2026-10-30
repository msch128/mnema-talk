//go:build integration

package server

import (
	"bytes"
	"image"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
)

const (
	iconAdminRoute  = "/api/admin/community/icon"
	iconPublicRoute = "/api/community/icon"
)

func putIcon(c *client, filename, contentType string, data []byte) response {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="icon"; filename="`+filename+`"`)
	h.Set("Content-Type", contentType)
	part, _ := mw.CreatePart(h)
	_, _ = part.Write(data)
	_ = mw.Close()
	return c.do(http.MethodPut, iconAdminRoute, &buf, map[string]string{"Content-Type": mw.FormDataContentType()})
}

func healthIcon(t *testing.T, c *client) (version string, present bool) {
	t.Helper()
	res := c.get("/api/health")
	if res.status != http.StatusOK {
		t.Fatalf("health: %d %s", res.status, res.body)
	}
	var raw map[string]any
	res.decode(t, &raw)
	v, ok := raw["community_icon"].(string)
	return v, ok
}

func TestCommunityIconIsSetServedPubliclyReplacedAndRemoved(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	anon := a.anon()

	if r := anon.get(iconPublicRoute); r.status != http.StatusNotFound || r.code(t) != "NOT_FOUND" {
		t.Fatalf("no icon yet: %d %s", r.status, r.body)
	}
	if _, present := healthIcon(t, anon); present {
		t.Fatal("health leaves community_icon out while none is set")
	}
	before := a.store.Len()

	// Metadata in the upload does not survive the redraw.
	res := putIcon(admin, "logo.jpg", "image/jpeg", bannerJPEGWithEXIF(t, "GPS-SECRET-MARKER"))
	if res.status != http.StatusOK {
		t.Fatalf("set icon: %d %s", res.status, res.body)
	}
	var set struct {
		Icon string `json:"community_icon"`
	}
	res.decode(t, &set)
	if set.Icon == "" {
		t.Fatalf("settings carry the icon version: %s", res.body)
	}
	if a.store.Len() != before+2 {
		t.Fatalf("two files stored: %d objects, had %d", a.store.Len(), before)
	}
	if v, ok := healthIcon(t, anon); !ok || v != set.Icon {
		t.Fatalf("health community_icon = %q, %v, want %q", v, ok, set.Icon)
	}

	// The sign-in screen learns the version as well, before anyone has a session.
	var options struct {
		Icon string `json:"community_icon"`
	}
	anon.get("/api/auth/options").decode(t, &options)
	if options.Icon != set.Icon {
		t.Errorf("sign-in options community_icon = %q, want %q", options.Icon, set.Icon)
	}

	// Public: no session needed, a square PNG at each size, no metadata.
	for size, edge := range map[string]int{"": 512, "512": 512, "128": 128} {
		path := iconPublicRoute
		if size != "" {
			path += "?size=" + size
		}
		got := anon.get(path)
		cfg, format, err := image.DecodeConfig(bytes.NewReader(got.body))
		if got.status != http.StatusOK || err != nil || format != "png" || cfg.Width != edge || cfg.Height != edge {
			t.Fatalf("size %q: %d %v %s %dx%d", size, got.status, err, format, cfg.Width, cfg.Height)
		}
		if bytes.Contains(got.body, []byte("GPS-SECRET-MARKER")) || got.header.Get("Content-Type") != "image/png" || got.header.Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("size %q: served as redrawn PNG, nosniff: %v", size, got.header)
		}
	}
	if r := anon.get(iconPublicRoute + "?size=64"); r.status != http.StatusBadRequest {
		t.Errorf("size=64: %d", r.status)
	}
	if r := anon.get(iconPublicRoute + "?size=abc"); r.status != http.StatusBadRequest {
		t.Errorf("size=abc: %d", r.status)
	}

	// Caching: validators, a short lifetime, immutable for the current version.
	first := anon.get(iconPublicRoute + "?size=128")
	etag := first.header.Get("ETag")
	if etag == "" || first.header.Get("Last-Modified") == "" || !strings.Contains(first.header.Get("Cache-Control"), "public") || strings.Contains(first.header.Get("Cache-Control"), "immutable") {
		t.Errorf("revalidated cache headers: %v", first.header)
	}
	if r := anon.do(http.MethodGet, iconPublicRoute+"?size=128", nil, map[string]string{"If-None-Match": etag}); r.status != http.StatusNotModified || len(r.body) != 0 {
		t.Errorf("If-None-Match: %d, %d bytes", r.status, len(r.body))
	}
	if r := anon.do(http.MethodGet, iconPublicRoute+"?size=512", nil, map[string]string{"If-None-Match": etag}); r.status != http.StatusOK {
		t.Errorf("the ETag of one size is not the other's: %d", r.status)
	}
	if r := anon.do(http.MethodGet, iconPublicRoute+"?size=128", nil, map[string]string{"If-Modified-Since": first.header.Get("Last-Modified")}); r.status != http.StatusNotModified {
		t.Errorf("If-Modified-Since: %d", r.status)
	}
	if r := anon.get(iconPublicRoute + "?size=128&v=" + set.Icon); !strings.Contains(r.header.Get("Cache-Control"), "immutable") {
		t.Errorf("the current version is immutable: %v", r.header)
	}
	if r := anon.get(iconPublicRoute + "?size=128&v=stale"); strings.Contains(r.header.Get("Cache-Control"), "immutable") {
		t.Errorf("an old version is not immutable: %v", r.header)
	}

	// Replacing keeps two files and changes the version, and so the ETag.
	second := putIcon(admin, "next.png", "image/png", bannerPNG(t, 300, 200))
	if second.status != http.StatusOK {
		t.Fatalf("replace: %d %s", second.status, second.body)
	}
	var next struct {
		Icon string `json:"community_icon"`
	}
	second.decode(t, &next)
	if next.Icon == "" || next.Icon == set.Icon || a.store.Len() != before+2 {
		t.Fatalf("replaced: version %q (was %q), %d objects (want %d)", next.Icon, set.Icon, a.store.Len(), before+2)
	}
	if r := anon.do(http.MethodGet, iconPublicRoute+"?size=128", nil, map[string]string{"If-None-Match": etag}); r.status != http.StatusOK {
		t.Errorf("the old ETag no longer matches: %d", r.status)
	}

	// Removing deletes both files; removing again is fine.
	if r := admin.delete(iconAdminRoute); r.status != http.StatusOK || strings.Contains(string(r.body), next.Icon) {
		t.Fatalf("remove: %d %s", r.status, r.body)
	}
	if a.store.Len() != before {
		t.Fatalf("removed: %d objects, had %d", a.store.Len(), before)
	}
	if r := anon.get(iconPublicRoute); r.status != http.StatusNotFound {
		t.Errorf("after removing: %d", r.status)
	}
	if _, present := healthIcon(t, anon); present {
		t.Error("health leaves community_icon out after removing")
	}
	if r := admin.delete(iconAdminRoute); r.status != http.StatusOK {
		t.Errorf("remove again: %d", r.status)
	}

	// Open clients are told each time, with the new version ("" once removed).
	var told []string
	for _, e := range a.events.Snapshot() {
		if p, ok := e.Payload.(map[string]string); ok && e.Type == events.CommunityUpdated {
			told = append(told, p["community_icon"])
		}
	}
	if want := []string{set.Icon, next.Icon, ""}; strings.Join(told, ",") != strings.Join(want, ",") {
		t.Errorf("community_updated icons %q, want %q (set, replace, remove)", told, want)
	}
}

func TestCommunityIconIsAuditedAndNeedsAdministrator(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	boss := a.register(admin, "boss")
	assignRoles(t, admin, boss, createRole(t, admin, "caretaker", perm.Administrator))

	png := bannerPNG(t, 400, 400)
	if r := a.anon().do(http.MethodPut, iconAdminRoute, nil, nil); r.status != http.StatusUnauthorized {
		t.Errorf("without a session: %d", r.status)
	}
	if r := putIcon(alice, "a.png", "image/png", png); r.status != http.StatusForbidden {
		t.Errorf("member: %d %s", r.status, r.body)
	}
	if r := alice.delete(iconAdminRoute); r.status != http.StatusForbidden {
		t.Errorf("member remove: %d", r.status)
	}
	if r := a.anon().get(iconPublicRoute); r.status != http.StatusNotFound {
		t.Errorf("a refused upload sets nothing: %d", r.status)
	}
	// Same CSRF protection as every admin route.
	if r := admin.do(http.MethodDelete, iconAdminRoute, nil, map[string]string{"Origin": "https://evil.example"}); r.status != http.StatusForbidden {
		t.Errorf("cross-origin: %d", r.status)
	}
	if r := putIcon(boss, "a.png", "image/png", png); r.status != http.StatusOK {
		t.Fatalf("administrator: %d %s", r.status, r.body)
	}
	if r := boss.delete(iconAdminRoute); r.status != http.StatusOK {
		t.Fatalf("administrator remove: %d %s", r.status, r.body)
	}

	byAction := map[string]int{}
	for _, e := range auditEntries(t, admin, "limit=200").Entries {
		byAction[e.Action]++
		if strings.HasPrefix(e.Action, "settings.community_icon") && e.ActorID != nil && *e.ActorID != boss.user.ID {
			t.Errorf("%s by %v, want the administrator", e.Action, e.ActorID)
		}
	}
	if byAction["settings.community_icon"] != 1 || byAction["settings.community_icon_remove"] != 1 {
		t.Errorf("audit actions: %v", byAction)
	}
}

func TestCommunityIconUploadLimitsAndTypes(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()

	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	cases := []struct {
		name, ct string
		data     []byte
		want     int
	}{
		{"icon.svg", "image/svg+xml", svg, http.StatusUnsupportedMediaType},
		{"page.png", "image/png", []byte("<!DOCTYPE html><script>1</script>"), http.StatusUnsupportedMediaType},
		{"small.png", "image/png", bannerPNG(t, 100, 100), http.StatusBadRequest},
		{"huge.png", "image/png", bannerPNG(t, 5000, 4000), http.StatusBadRequest},
		{"moving.gif", "image/gif", bannerGIF(t, 300, 300, 3), http.StatusBadRequest},
		{"big.png", "image/png", append(bannerPNG(t, 300, 300), make([]byte, 7<<20)...), http.StatusRequestEntityTooLarge},
	}
	for _, c := range cases {
		if r := putIcon(admin, c.name, c.ct, c.data); r.status != c.want {
			t.Errorf("%s: %d %s, want %d", c.name, r.status, r.body, c.want)
		}
	}
	if a.store.Len() != 0 {
		t.Errorf("a refused icon leaves no file: %d objects", a.store.Len())
	}
	if r := a.anon().get(iconPublicRoute); r.status != http.StatusNotFound {
		t.Errorf("nothing was set: %d", r.status)
	}
	// A wide picture is cropped to the centre square; a still GIF is fine.
	if r := putIcon(admin, "wide.png", "image/png", bannerPNG(t, 1600, 400)); r.status != http.StatusOK {
		t.Errorf("wide picture: %d %s", r.status, r.body)
	}
	if r := putIcon(admin, "still.gif", "image/gif", bannerGIF(t, 300, 300, 1)); r.status != http.StatusOK {
		t.Errorf("single-frame gif: %d %s", r.status, r.body)
	}
}
