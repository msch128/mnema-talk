package server

import (
	"strings"
	"testing"
)

const testShell = `<!DOCTYPE html>
<html lang="de">
  <head>
    <meta charset="UTF-8" />
    <title>Mnema Talk</title>
    <link rel="icon" href="/favicon.svg" />
  </head>
  <body><div id="app"></div></body>
</html>`

func TestPageTagsWriteTitleAndPreviewTags(t *testing.T) {
	out := string(pageTags{
		title: "Join Team on Mnema Talk", description: "Welcome to the team.",
		url: "https://talk.example/invite/abc234xyz789", image: "https://talk.example/pwa-icon-512.png", noindex: true,
	}.apply([]byte(testShell)))
	for _, want := range []string{
		"<title>Join Team on Mnema Talk</title>",
		`<meta property="og:title" content="Join Team on Mnema Talk" />`,
		`<meta property="og:description" content="Welcome to the team." />`,
		`<meta property="og:url" content="https://talk.example/invite/abc234xyz789" />`,
		`<meta property="og:image" content="https://talk.example/pwa-icon-512.png" />`,
		`<meta property="og:site_name" content="Mnema Talk" />`,
		`<meta name="twitter:card" content="summary" />`,
		`<meta name="robots" content="noindex, nofollow" />`,
		`<link rel="icon" href="/favicon.svg" />`,
		`<div id="app"></div>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Count(out, "<title>") != 1 || strings.Index(out, `og:title`) > strings.Index(out, "</head>") {
		t.Errorf("tags must sit inside the head, once:\n%s", out)
	}
}

func TestPageTagsEscapeEverythingTheOwnerTyped(t *testing.T) {
	evil := `"><script>alert(1)</script><b a='`
	out := string(pageTags{title: evil, description: evil, url: "https://t.example/" + evil}.apply([]byte(testShell)))
	if strings.Contains(out, "<script>") || strings.Contains(out, evil) || strings.Contains(out, `a='`) {
		t.Fatalf("unescaped markup:\n%s", out)
	}
	if !strings.Contains(out, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Fatalf("text not escaped:\n%s", out)
	}
}

func TestPageTagsLeaveOutWhatIsEmpty(t *testing.T) {
	out := string(pageTags{title: "Mnema Talk"}.apply([]byte(testShell)))
	for _, absent := range []string{"og:url", "og:image", "twitter:image", "robots", `name="description"`} {
		if strings.Contains(out, absent) {
			t.Errorf("%s must be left out without a value", absent)
		}
	}
}
