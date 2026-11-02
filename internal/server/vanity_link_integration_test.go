//go:build integration

package server

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/perm"
)

const vanityRoute = "/api/admin/vanity-link"

func setVanity(t *testing.T, c *client, slug string, accepts bool, want int) response {
	t.Helper()
	res := c.put(vanityRoute, map[string]any{"slug": slug, "accepts_signups": accepts})
	if res.status != want {
		t.Fatalf("set vanity %q: %d %s, want %d", slug, res.status, res.body, want)
	}
	return res
}

func joinPreview(t *testing.T, c *client, slug string) (valid, accepts bool, name string) {
	t.Helper()
	res := c.get("/api/join/" + slug)
	if res.status != http.StatusOK {
		t.Fatalf("preview %q: %d %s", slug, res.status, res.body)
	}
	var p struct {
		Valid          bool   `json:"valid"`
		AcceptsSignups bool   `json:"accepts_signups"`
		CommunityName  string `json:"community_name"`
	}
	res.decode(t, &p)
	return p.Valid, p.AcceptsSignups, p.CommunityName
}

func registerWith(c *client, username, code string) response {
	return c.post("/api/auth/register", map[string]any{"username": username, "password": "correct-horse-battery", "invite_code": code})
}

func TestVanityLinkPermissionsAndValidation(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	boss := a.register(admin, "boss")
	assignRoles(t, admin, boss, createRole(t, admin, "caretaker", perm.Administrator))

	if r := a.anon().put(vanityRoute, map[string]any{"slug": "owls"}); r.status != http.StatusUnauthorized {
		t.Errorf("without a session: %d", r.status)
	}
	if r := alice.put(vanityRoute, map[string]any{"slug": "owls"}); r.status != http.StatusForbidden {
		t.Errorf("member set: %d", r.status)
	}
	if r := alice.get(vanityRoute); r.status != http.StatusForbidden {
		t.Errorf("member read: %d", r.status)
	}
	if r := admin.do(http.MethodPut, vanityRoute, map[string]any{"slug": "owls"}, map[string]string{"Origin": "https://evil.example"}); r.status != http.StatusForbidden {
		t.Errorf("cross-origin: %d", r.status)
	}
	for _, bad := range []string{"ab", "-owls", "owls-", "my--clan", "my_clan", "admin", "Join", strings.Repeat("x", 33)} {
		if r := setVanity(t, admin, bad, false, http.StatusBadRequest); r.code(t) != "INVALID_INPUT" {
			t.Errorf("%q: %s", bad, r.body)
		}
	}
	if r := admin.put(vanityRoute, map[string]any{"slug": "owls", "extra": 1}); r.status != http.StatusBadRequest {
		t.Errorf("unknown field: %d", r.status)
	}

	// An Administrator may set it; upper case folds to lower.
	res := setVanity(t, boss, "Mein-Clan", false, http.StatusOK)
	if !strings.Contains(string(res.body), `"slug":"mein-clan"`) || !strings.Contains(string(res.body), `"path":"/join/mein-clan"`) {
		t.Errorf("setting: %s", res.body)
	}
	var got struct{ Slug string }
	admin.get(vanityRoute).decode(t, &got)
	if got.Slug != "mein-clan" {
		t.Errorf("read back %q", got.Slug)
	}
	// A link that does not exist cannot take sign-ups.
	res = setVanity(t, admin, "", true, http.StatusOK)
	if strings.Contains(string(res.body), `"accepts_signups":true`) || strings.Contains(string(res.body), "mein-clan") {
		t.Errorf("cleared: %s", res.body)
	}

	actions := map[string]int{}
	for _, e := range auditEntries(t, admin, "limit=200").Entries {
		actions[e.Action]++
	}
	if actions["invite.vanity_link"] < 2 {
		t.Errorf("audit actions: %v", actions)
	}
}

func TestVanityLinkNeverBypassesInviteOnlyUnlessTheOwnerSaysSo(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	anon := a.anon()
	// Ordinary invites work whatever the link does (counted first: sign-ups share a budget).
	if r := a.register(admin, "regular"); r.user == nil {
		t.Fatal("regular invite sign-up")
	}

	if valid, _, _ := joinPreview(t, anon, "owls"); valid {
		t.Fatal("no link yet")
	}
	setVanity(t, admin, "owls", false, http.StatusOK)
	if valid, accepts, _ := joinPreview(t, anon, "OWLS"); !valid || accepts {
		t.Fatalf("preview: valid=%v accepts=%v, want a link that takes no sign-ups", valid, accepts)
	}
	// Default: the link is only a door to the sign-in page; the slug is no invite code.
	for _, code := range []string{"~owls", "owls"} {
		if r := registerWith(anon, "newbie", code); r.status != http.StatusBadRequest {
			t.Errorf("code %q without accepts_signups: %d %s", code, r.status, r.body)
		}
	}
	var n int
	if err := a.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM users WHERE username = 'newbie'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("no account was created: %d %v", n, err)
	}

	// The Owner opts in: the link is a permanent, unlimited public invite.
	setVanity(t, admin, "owls", true, http.StatusOK)
	if valid, accepts, _ := joinPreview(t, anon, "owls"); !valid || !accepts {
		t.Fatalf("preview after opting in: %v %v", valid, accepts)
	}
	for _, name := range []string{"newbie", "second", "third"} {
		if r := registerWith(a.anon(), name, "~OWLS"); r.status != http.StatusCreated {
			t.Fatalf("sign-up %s: %d %s", name, r.status, r.body)
		}
	}
	if r := registerWith(a.anon(), "wrongone", "~owl"); r.status != http.StatusBadRequest {
		t.Errorf("a different slug: %d", r.status)
	}
	var invite *string
	if err := a.db.QueryRow(context.Background(), `SELECT invite_id::text FROM users WHERE username = 'newbie'`).Scan(&invite); err != nil || invite != nil {
		t.Errorf("vanity sign-ups carry no invite row: %v %v", invite, err)
	}

	// Switching it off, or removing the link, closes the door at once.
	setVanity(t, admin, "owls", false, http.StatusOK)
	if r := registerWith(a.anon(), "late", "~owls"); r.status != http.StatusBadRequest {
		t.Errorf("after switching off: %d", r.status)
	}
	setVanity(t, admin, "owls", true, http.StatusOK)
	setVanity(t, admin, "", false, http.StatusOK)
	if r := registerWith(a.anon(), "later", "~owls"); r.status != http.StatusBadRequest {
		t.Errorf("after removal: %d", r.status)
	}
	if valid, _, _ := joinPreview(t, anon, "owls"); valid {
		t.Error("a removed link is unknown")
	}
}

func TestVanityLinkPreviewIsRateLimitedAndRefusesOddSlugs(t *testing.T) {
	a := newApp(t, false)
	anon := a.anon()
	for _, odd := range []string{"a", "x%20y", strings.Repeat("a", 40), "snow_man"} {
		if valid, _, _ := joinPreview(t, anon, odd); valid {
			t.Errorf("%q cannot be a link", odd)
		}
	}
	var limited bool
	for i := 0; i < 200 && !limited; i++ {
		limited = anon.get("/api/join/someslug").status == http.StatusTooManyRequests
	}
	if !limited {
		t.Error("lookups are rate limited")
	}
}

func TestVanityLinkPageCarriesTheCommunityNameAndEscapesTheSlug(t *testing.T) {
	a, admin := newSharePageApp(t)
	setCommunity(t, admin, `Owls <b>&"`, "A private welcome text.", "")
	setVanity(t, admin, "owls", false, http.StatusOK)

	page := string(a.anon().get("/join/owls").body)
	for _, want := range []string{"<title>Join Owls &lt;b&gt;&amp;&#34; on Mnema Talk</title>", `property="og:url" content="` + a.origin() + `/join/owls"`, "noindex"} {
		if !strings.Contains(page, want) {
			t.Errorf("missing %q in\n%s", want, page)
		}
	}
	if strings.Contains(page, "private welcome") {
		t.Errorf("a vanity page shows no welcome text:\n%s", page)
	}
	// An unknown slug gets the generic page, and nothing of it is reflected.
	generic := string(a.anon().get("/join/nobody-here").body)
	if strings.Contains(generic, "nobody-here") || strings.Contains(generic, "Owls") || !strings.Contains(generic, "<title>Mnema Talk</title>") {
		t.Errorf("unknown slug:\n%s", generic)
	}
	// Anything that is not a slug falls through to the app shell untouched.
	if odd := string(a.anon().get("/join/%3Cscript%3E").body); strings.Contains(odd, "og:title") || strings.Contains(odd, "script%3E") {
		t.Errorf("odd slug:\n%s", odd)
	}
	if r := a.anon().get("/join/owls"); r.header.Get("Location") != "" {
		t.Error("no redirect from a vanity link")
	}
}
