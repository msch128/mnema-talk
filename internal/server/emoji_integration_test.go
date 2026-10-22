//go:build integration

package server

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/emojitext"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
)

type emojiJSON struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	Animated bool      `json:"animated"`
	URL      string    `json:"url"`
}

type emojiList struct {
	Emoji []emojiJSON `json:"emoji"`
	Limit int         `json:"limit"`
}

func squarePNG(t *testing.T, edge int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, edge, edge))
	for i := 0; i < edge; i++ {
		img.Set(i, i, color.NRGBA{R: 255, A: 255})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func uploadEmoji(c *client, name, filename, ctype string, data []byte) response {
	return c.upload("/api/admin/emoji", "file", filename, ctype, data, map[string]string{"name": name})
}

func emojiFixture(t *testing.T, adjust func(*config.Config)) (*app, *client, *client) {
	t.Helper()
	a := newAppWith(t, false, adjust)
	admin := a.seedAdmin()
	manager := a.register(admin, "emojimaker")
	assignRoles(t, admin, manager, createRole(t, admin, "emoji", perm.ManageEmoji))
	return a, admin, manager
}

func TestEmojiUploadIsRedrawnAndServedSafely(t *testing.T) {
	a, _, manager := emojiFixture(t, nil)
	hostile := append(squarePNG(t, 300), []byte("<script>alert(1)</script>")...)

	res := uploadEmoji(manager, "Party_Blob", "blob.png", "image/png", hostile)
	if res.status != http.StatusCreated {
		t.Fatalf("upload: %d %s", res.status, res.body)
	}
	var e emojiJSON
	res.decode(t, &e)
	if e.Name != "party_blob" || e.Animated || !strings.HasPrefix(e.URL, "/api/emoji/"+e.ID.String()+"/image?v=") {
		t.Fatalf("%+v", e)
	}
	img := manager.get(e.URL)
	if img.status != http.StatusOK || img.header.Get("Content-Type") != "image/png" ||
		img.header.Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(img.header.Get("Cache-Control"), "immutable") {
		t.Fatalf("serve: %d %v", img.status, img.header)
	}
	if bytes.Contains(img.body, []byte("<script>")) {
		t.Fatal("hidden bytes survived the redraw")
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(img.body))
	if err != nil || cfg.Width != 128 || cfg.Height != 128 {
		t.Fatalf("size %+v %v", cfg, err)
	}
	if again := manager.do(http.MethodGet, e.URL, nil, map[string]string{"If-None-Match": img.header.Get("ETag")}); again.status != http.StatusNotModified {
		t.Fatalf("revalidation: %d", again.status)
	}
	if anon := a.anon().get(e.URL); anon.status != http.StatusUnauthorized {
		t.Fatalf("image without a session: %d", anon.status)
	}
}

func TestEmojiUploadRefusesWhatIsNotAnImage(t *testing.T) {
	a, _, manager := emojiFixture(t, nil)
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	for name, file := range map[string]struct {
		ctype string
		data  []byte
		want  int
	}{
		"svg":              {"image/svg+xml", svg, http.StatusUnsupportedMediaType},
		"html as png":      {"image/png", xssHTML, http.StatusUnsupportedMediaType},
		"too large":        {"image/png", append(squarePNG(t, 16), bytes.Repeat([]byte{0}, 300<<10)...), http.StatusRequestEntityTooLarge},
		"truncated":        {"image/png", squarePNG(t, 16)[:30], http.StatusBadRequest},
		"canvas too large": {"image/png", squarePNG(t, 1100), http.StatusBadRequest},
	} {
		if res := uploadEmoji(manager, "thing", "x.png", file.ctype, file.data); res.status != file.want {
			t.Errorf("%s: %d %s", name, res.status, res.body)
		}
	}
	if a.store.Len() != 0 {
		t.Fatal("a refused upload reached storage")
	}
}

func TestEmojiNamesAreValidatedAndUnique(t *testing.T) {
	_, _, manager := emojiFixture(t, nil)
	data := squarePNG(t, 32)
	for _, bad := range []string{"", "a", "has space", "dash-ed", strings.Repeat("a", 33), "ümlaut"} {
		if res := uploadEmoji(manager, bad, "x.png", "image/png", data); res.status != http.StatusBadRequest {
			t.Errorf("name %q: %d", bad, res.status)
		}
	}
	if res := uploadEmoji(manager, "same", "x.png", "image/png", data); res.status != http.StatusCreated {
		t.Fatal(res.status, string(res.body))
	}
	if res := uploadEmoji(manager, "SAME", "x.png", "image/png", data); res.status != http.StatusConflict {
		t.Fatalf("duplicate: %d", res.status)
	}
}

func TestEmojiLimitStopsNewUploads(t *testing.T) {
	a, _, manager := emojiFixture(t, func(c *config.Config) { c.EmojiMax = 1 })
	data := squarePNG(t, 32)
	if res := uploadEmoji(manager, "first", "x.png", "image/png", data); res.status != http.StatusCreated {
		t.Fatal(res.status)
	}
	if res := uploadEmoji(manager, "second", "x.png", "image/png", data); res.status != http.StatusConflict {
		t.Fatalf("over the limit: %d", res.status)
	}
	if a.store.Len() != 1 {
		t.Fatalf("objects left behind: %d", a.store.Len())
	}
}

func TestOnlyManageEmojiMayChangeEmoji(t *testing.T) {
	a, admin, manager := emojiFixture(t, nil)
	member := a.register(admin, "plain")
	data := squarePNG(t, 32)
	if res := uploadEmoji(member, "nope", "x.png", "image/png", data); res.status != http.StatusForbidden {
		t.Fatalf("member upload: %d", res.status)
	}
	var e emojiJSON
	uploadEmoji(manager, "mine", "x.png", "image/png", data).decode(t, &e)
	if res := member.patch("/api/admin/emoji/"+e.ID.String(), map[string]string{"name": "other"}); res.status != http.StatusForbidden {
		t.Fatalf("member rename: %d", res.status)
	}
	if res := member.delete("/api/admin/emoji/" + e.ID.String()); res.status != http.StatusForbidden {
		t.Fatalf("member delete: %d", res.status)
	}
	var list emojiList
	member.get("/api/emoji").decode(t, &list)
	if len(list.Emoji) != 1 || list.Limit != 200 {
		t.Fatalf("every member may list: %+v", list)
	}
}

func TestRenamedAndDeletedEmojiAreAuditedAndAnnounced(t *testing.T) {
	a, admin, manager := emojiFixture(t, nil)
	var e emojiJSON
	uploadEmoji(manager, "before", "x.png", "image/png", squarePNG(t, 32)).decode(t, &e)
	if res := manager.patch("/api/admin/emoji/"+e.ID.String(), map[string]string{"name": "after"}); res.status != http.StatusOK {
		t.Fatalf("rename: %d %s", res.status, res.body)
	}
	if res := manager.delete("/api/admin/emoji/" + e.ID.String()); res.status != http.StatusNoContent {
		t.Fatalf("delete: %d", res.status)
	}
	if res := manager.get(e.URL); res.status != http.StatusNotFound || a.store.Len() != 0 {
		t.Fatalf("deleted image: %d, %d objects left", res.status, a.store.Len())
	}
	changed := 0
	for _, ev := range a.events.Snapshot() {
		if ev.Type == events.EmojiChanged {
			changed++
		}
	}
	if changed != 3 {
		t.Fatalf("emoji_changed sent %d times", changed)
	}
	log := string(admin.get("/api/admin/audit-log").body)
	for _, action := range []string{"emoji.create", "emoji.rename", "emoji.delete"} {
		if !strings.Contains(log, action) {
			t.Errorf("audit log lacks %s", action)
		}
	}
}

func TestCustomEmojiCanBeUsedAsAReaction(t *testing.T) {
	a, admin, _ := emojiFixture(t, nil)
	ch := a.createChannel(admin, "general", "text")
	msg := a.send(admin, ch, "hello")
	token := emojitext.Token("party", uuid.New())
	res := admin.post("/api/messages/"+msg.String()+"/reactions", map[string]string{"emoji": token})
	var out struct {
		Reactions []struct{ Emoji string } `json:"reactions"`
	}
	res.decode(t, &out)
	if res.status != http.StatusOK || len(out.Reactions) != 1 || out.Reactions[0].Emoji != token {
		t.Fatalf("reaction: %d %s", res.status, res.body)
	}
}

func animatedGIF(t *testing.T) []byte {
	t.Helper()
	pal := color.Palette{color.Transparent, color.RGBA{R: 255, A: 255}, color.RGBA{B: 255, A: 255}}
	g := &gif.GIF{}
	for i := 0; i < 3; i++ {
		p := image.NewPaletted(image.Rect(0, 0, 40, 40), pal)
		p.SetColorIndex(i, i, uint8(1+i%2))
		g.Image = append(g.Image, p)
		g.Delay = append(g.Delay, 4)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestAnimatedEmojiHasAStillFrame(t *testing.T) {
	_, _, manager := emojiFixture(t, nil)
	var e struct {
		emojiJSON
		StillURL string `json:"still_url"`
	}
	res := uploadEmoji(manager, "spin", "spin.gif", "image/gif", animatedGIF(t))
	if res.status != http.StatusCreated {
		t.Fatalf("upload: %d %s", res.status, res.body)
	}
	res.decode(t, &e)
	if !e.Animated || e.StillURL == "" {
		t.Fatalf("%+v", e)
	}
	if anim := manager.get(e.URL); anim.header.Get("Content-Type") != "image/gif" {
		t.Fatalf("animation: %v", anim.header)
	}
	still := manager.get(e.StillURL)
	if still.header.Get("Content-Type") != "image/png" || !bytes.HasPrefix(still.body, []byte("\x89PNG")) {
		t.Fatalf("still: %d %v", still.status, still.header)
	}
	static := uploadEmoji(manager, "flat", "x.png", "image/png", squarePNG(t, 16))
	var flat struct{ ID uuid.UUID }
	static.decode(t, &flat)
	if res := manager.get("/api/emoji/" + flat.ID.String() + "/still"); res.status != http.StatusNotFound {
		t.Fatalf("still of a static emoji: %d", res.status)
	}
}
