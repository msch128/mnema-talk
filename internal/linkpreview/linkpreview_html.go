package linkpreview

import (
	"bytes"
	"io"
	"net/url"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
	"golang.org/x/text/transform"
)

// utf8Reader decodes body to UTF-8 using the charset of the Content-Type
// header or the page's own <meta charset>. A page that declares no charset
// and is valid UTF-8 is read as is, since the sniffer only looks at the first
// KiB and would otherwise guess windows-1252.
func utf8Reader(body []byte, ctype string) io.Reader {
	enc, _, certain := charset.DetermineEncoding(body, ctype)
	if !certain && validUTF8Prefix(body) {
		return bytes.NewReader(body)
	}
	return transform.NewReader(bytes.NewReader(body), enc.NewDecoder())
}

// validUTF8Prefix reports whether body is valid UTF-8, ignoring a rune cut
// off by the download limit at its end.
func validUTF8Prefix(body []byte) bool {
	for i := 0; i < utf8.UTFMax && i < len(body); i++ {
		if cut := body[:len(body)-i]; utf8.Valid(cut) {
			return true
		}
	}
	return false
}

// parseHTML reads Open Graph tags with <title> and meta description as
// fallbacks. Text is plain (entities decoded); the client escapes it.
func parseHTML(body []byte, ctype string, base *url.URL) *Preview {
	p := &Preview{}
	var title, desc string
	z := html.NewTokenizer(utf8Reader(body, ctype))
	inTitle := false
	for {
		switch z.Next() {
		case html.ErrorToken:
			return finish(p, title, desc, base)
		case html.StartTagToken, html.SelfClosingTagToken:
			tok := z.Token()
			switch tok.Data {
			case "title":
				inTitle = true
			case "meta":
				applyMeta(p, &desc, tok)
			case "body":
				// Everything needed lives in <head>.
				return finish(p, title, desc, base)
			}
		case html.TextToken:
			if inTitle && title == "" {
				title = string(z.Text())
			}
		case html.EndTagToken:
			if tok := z.Token(); tok.Data == "title" {
				inTitle = false
			}
		}
	}
}

// applyMeta stores the value of one <meta> tag: Open Graph fields on the
// preview, the plain description as fallback in desc.
func applyMeta(p *Preview, desc *string, tok html.Token) {
	var key, content string
	for _, a := range tok.Attr {
		switch strings.ToLower(a.Key) {
		case "property", "name":
			key = strings.ToLower(a.Val)
		case "content":
			content = a.Val
		}
	}
	switch key {
	case "og:title":
		p.Title = content
	case "og:description":
		p.Description = content
	case "og:site_name":
		p.SiteName = content
	case "og:image", "og:image:url":
		if p.Image == "" {
			p.Image = content
		}
	case "description":
		*desc = content
	}
}

func finish(p *Preview, title, desc string, base *url.URL) *Preview {
	if p.Title == "" {
		p.Title = title
	}
	if p.Description == "" {
		p.Description = desc
	}
	p.Title = clip(p.Title, maxTitleRunes)
	p.Description = clip(p.Description, maxDescRunes)
	p.SiteName = clip(p.SiteName, 80)
	if p.Image != "" {
		// An URL the image proxy would refuse anyway is not worth caching
		// and sending to every client.
		if img, err := base.Parse(strings.TrimSpace(p.Image)); err == nil && (img.Scheme == "http" || img.Scheme == "https") && len(img.String()) <= maxURLLen {
			p.Image = img.String()
		} else {
			p.Image = ""
		}
	}
	return p
}

func clip(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max-1]) + "…"
}
