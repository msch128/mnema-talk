//go:build integration

package server

import (
	"html"
	"net/http"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/auth"
)

func newSharePageApp(t *testing.T) (*app, *client) {
	t.Helper()
	a := newAppWithDeps(t, false, nil, func(d *Deps) { d.WebIndex = []byte(testShell) })
	return a, a.seedAdmin()
}

func setCommunity(t *testing.T, admin *client, name, welcome, rules string) {
	t.Helper()
	res := admin.put("/api/admin/onboarding", map[string]any{"community_name": name, "welcome_text": welcome, "rules_text": rules})
	if res.status != http.StatusOK {
		t.Fatalf("set onboarding: %d %s", res.status, res.body)
	}
}

func newInvite(t *testing.T, admin *client) string {
	t.Helper()
	res := admin.post("/api/admin/invites", map[string]any{})
	if res.status != http.StatusCreated {
		t.Fatalf("create invite: %d %s", res.status, res.body)
	}
	var inv auth.Invite
	res.decode(t, &inv)
	return inv.Code
}

func TestInviteLinkPreviewShowsNameAndTrimmedWelcomeText(t *testing.T) {
	a, admin := newSharePageApp(t)
	welcome := "# Welcome\n\n**Hello** and welcome to the [team](https://example.org)! " + strings.Repeat("We like long texts. ", 30)
	setCommunity(t, admin, "Night Owls", welcome, "Secret house rules: do not mention the code word.")
	code := newInvite(t, admin)

	res := a.anon().get("/invite/" + code)
	if res.status != http.StatusOK || !strings.HasPrefix(res.header.Get("Content-Type"), "text/html") {
		t.Fatalf("status/type: %d %q", res.status, res.header.Get("Content-Type"))
	}
	page := string(res.body)
	for _, want := range []string{
		"<title>Join Night Owls on Mnema Talk</title>",
		`property="og:title" content="Join Night Owls on Mnema Talk"`,
		`property="og:url" content="` + a.origin() + `/invite/` + code + `"`,
		`property="og:image" content="` + a.origin() + `/pwa-icon-512.png"`,
		`name="robots" content="noindex, nofollow"`,
		`<div id="app"></div>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("missing %q in\n%s", want, page)
		}
	}
	desc := metaContent(t, page, `property="og:description"`)
	if !strings.HasPrefix(desc, "Welcome Hello and welcome to the team!") || len([]rune(desc)) > 200 || !strings.HasSuffix(desc, "…") {
		t.Errorf("description = %q", desc)
	}
	if strings.ContainsAny(desc, "#*[]") || strings.Contains(page, "Secret house rules") || strings.Contains(page, "example.org") {
		t.Errorf("markup or private text leaked:\n%s", page)
	}
}

func TestInvitePreviewIsGenericForCodesThatDoNotWork(t *testing.T) {
	a, admin := newSharePageApp(t)
	setCommunity(t, admin, "Night Owls", "Welcome!", "")
	for _, path := range []string{"/invite/abc234xyz789", "/invite/abc234xyz789/"} {
		res := a.anon().get(path)
		page := string(res.body)
		if res.status != http.StatusOK {
			t.Fatalf("%s: status %d", path, res.status)
		}
		if strings.Contains(page, "Night Owls") || strings.Contains(page, "Welcome!") || !strings.Contains(page, "<title>Mnema Talk</title>") {
			t.Errorf("%s leaked the community for an unusable code:\n%s", path, page)
		}
	}
}

func TestRootLinkPreviewShowsOnlyTheName(t *testing.T) {
	a, admin := newSharePageApp(t)
	setCommunity(t, admin, `Owls <b>&"`, "A private welcome text.", "Rules text.")

	page := string(a.anon().get("/").body)
	if !strings.Contains(page, "<title>Owls &lt;b&gt;&amp;&#34;</title>") || !strings.Contains(page, `property="og:url" content="`+a.origin()+`/"`) {
		t.Errorf("root page:\n%s", page)
	}
	if strings.Contains(page, "private welcome") || strings.Contains(page, "Rules text") || strings.Contains(page, "noindex") {
		t.Errorf("root page shows more than the name:\n%s", page)
	}
	if res := a.anon().get("/c/some-route"); strings.Contains(string(res.body), "og:title") {
		t.Errorf("only the root and invites get preview tags")
	}
}

func TestInvitePreviewSharesTheInviteRateLimit(t *testing.T) {
	a, admin := newSharePageApp(t)
	setCommunity(t, admin, "Night Owls", "Welcome!", "")
	code := newInvite(t, admin)
	anon := a.anon()
	generic := 0
	for i := 0; i < 40; i++ {
		res := anon.get("/invite/" + code)
		if res.status != http.StatusOK {
			t.Fatalf("status %d", res.status)
		}
		if !strings.Contains(string(res.body), "Night Owls") {
			generic++
		}
	}
	if generic == 0 {
		t.Fatal("the invite budget (30 per minute) must also bound the page's database lookups")
	}
}

func metaContent(t *testing.T, page, selector string) string {
	t.Helper()
	i := strings.Index(page, selector)
	if i < 0 {
		t.Fatalf("no meta %s in\n%s", selector, page)
	}
	rest := page[i:]
	j := strings.Index(rest, `content="`)
	k := strings.Index(rest[j+9:], `"`)
	return html.UnescapeString(rest[j+9 : j+9+k])
}
