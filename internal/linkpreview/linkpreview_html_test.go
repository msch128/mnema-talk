package linkpreview

import (
	"net/url"
	"strings"
	"testing"
)

func TestOversizedOGImageIsDropped(t *testing.T) {
	base, _ := url.Parse("https://example.com/")
	long := "https://cdn.example.com/" + strings.Repeat("a", maxURLLen)
	p := parseHTML([]byte(`<head><title>t</title><meta property="og:image" content="`+long+`">`), "text/html", base)
	if p.Image != "" {
		t.Fatalf("image kept, len %d", len(p.Image))
	}
	p = parseHTML([]byte(`<head><title>t</title><meta property="og:image" content="/i.png">`), "text/html", base)
	if p.Image != "https://example.com/i.png" {
		t.Fatalf("image %q", p.Image)
	}
}

func TestPageCharsetIsHonoured(t *testing.T) {
	base, _ := url.Parse("https://example.com/")
	latin1 := []byte("<head><title>Caf\xe9</title>")
	if p := parseHTML(latin1, "text/html; charset=iso-8859-1", base); p.Title != "Café" {
		t.Fatalf("header charset: %q", p.Title)
	}
	meta := []byte(`<head><meta charset="windows-1252"><title>Caf` + "\xe9" + `</title>`)
	if p := parseHTML(meta, "text/html", base); p.Title != "Café" {
		t.Fatalf("meta charset: %q", p.Title)
	}
	if p := parseHTML([]byte("<head><title>Café</title>"), "text/html; charset=utf-8", base); p.Title != "Café" {
		t.Fatalf("utf-8: %q", p.Title)
	}
}

func TestUndeclaredUTF8PageKeepsNonASCIITitle(t *testing.T) {
	base, _ := url.Parse("https://example.com/")
	pad := strings.Repeat(`<meta name="x" content="aaaaaaaaaa">`, 40)
	body := []byte("<head>" + pad + "<title>ü日本語é</title>")
	if p := parseHTML(body, "text/html", base); p.Title != "ü日本語é" {
		t.Fatalf("undeclared utf-8: %q", p.Title)
	}
}
