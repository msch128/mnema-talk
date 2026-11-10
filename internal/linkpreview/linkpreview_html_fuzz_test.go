package linkpreview

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"unicode/utf8"
)

var sixHexDigits = regexp.MustCompile(`^#[0-9a-f]{6}$`)

// FuzzParseHTML checks that reading the head of a fetched page never panics,
// whatever the page and the charset it claims, and that what comes out keeps
// the limits the clients rely on: clipped texts, an absolute http(s) image URL
// of bounded length and a theme colour in one notation.
func FuzzParseHTML(f *testing.F) {
	pages, err := filepath.Glob("testdata/*.html")
	if err != nil || len(pages) == 0 {
		f.Fatalf("no pages in testdata: %v", err)
	}
	for _, name := range pages {
		page, err := os.ReadFile(name)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(page, "text/html; charset=utf-8")
	}
	for _, seed := range []struct{ body, ctype string }{
		{`<head><title>t</title><meta property="og:image" content="/i.png">`, "text/html"},
		{"<head><title>Caf\xe9</title>", "text/html; charset=iso-8859-1"},
		{"<head><meta charset=\"windows-1252\"><title>Caf\xe9</title>", "text/html"},
		{"<head><title>Café</title>", "text/html; charset=utf-8"},
		{"<head><title>\u00fc\u65e5\u672c\u8a9e\xc3", "text/html"},
		{`<head><meta name="theme-color" content="#AbC"><meta name="twitter:card" content="summary"><meta property="og:image:width" content="300"><meta property="og:image:height" content="300"><link rel="alternate" type="application/json+oembed" href="/oembed?u=1">`, "text/html"},
		{`<meta property="og:image" content="javascript:alert(1)"><meta name="author" content="https://example.com/me"><body><title>late</title>`, ""},
		{"\xff\xfe<\x00t\x00i\x00t\x00l\x00e\x00>\x00", "text/html; charset=utf-16"},
		{"", "text/html; charset=bogus"},
	} {
		f.Add([]byte(seed.body), seed.ctype)
	}
	base, err := url.Parse("https://example.com/a/page")
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, body []byte, ctype string) {
		p := parseHTML(body, ctype, base)
		for _, text := range []struct {
			name, got string
			max       int
		}{{"title", p.Title, maxTitleRunes}, {"description", p.Description, maxDescRunes}, {"site name", p.SiteName, 80}, {"author", p.Author, 100}} {
			if n := utf8.RuneCountInString(text.got); n > text.max {
				t.Fatalf("%s of %d runes, at most %d", text.name, n, text.max)
			}
		}
		if p.Image != "" {
			u, err := url.Parse(p.Image)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || len(p.Image) > maxURLLen {
				t.Fatalf("image URL %q (%v)", p.Image, err)
			}
		}
		if p.Color != "" && !sixHexDigits.MatchString(p.Color) {
			t.Fatalf("theme colour %q", p.Color)
		}
		if p.Compact && p.Image == "" {
			t.Fatal("a thumbnail layout without an image")
		}
	})
}
