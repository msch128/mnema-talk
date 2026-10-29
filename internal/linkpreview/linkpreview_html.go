package linkpreview

import (
	"bytes"
	"cmp"
	"io"
	"net/url"
	"regexp"
	"strconv"
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

// pageMeta collects what the head of a page says, by source, so finish can
// apply the precedence: Open Graph, then Twitter card, then plain HTML.
type pageMeta struct {
	og, tw      Preview
	title, desc string
	author      string
	themeColor  string
	card        string
	imgW, imgH  int
	oembed      string
}

// parseHTML reads the head of a page: Open Graph and Twitter card tags with
// <title> and the plain meta tags as fallbacks, the theme colour and the
// oEmbed endpoint the page advertises. Text is plain (entities decoded); the
// client escapes it.
func parseHTML(body []byte, ctype string, base *url.URL) *Preview {
	m := &pageMeta{}
	z := html.NewTokenizer(utf8Reader(body, ctype))
	inTitle := false
	for {
		switch z.Next() {
		case html.ErrorToken:
			return m.finish(base)
		case html.StartTagToken, html.SelfClosingTagToken:
			tok := z.Token()
			switch tok.Data {
			case "title":
				inTitle = true
			case "meta":
				m.applyMeta(tok)
			case "link":
				m.applyLink(tok)
			case "body":
				// Everything needed lives in <head>.
				return m.finish(base)
			}
		case html.TextToken:
			if inTitle && m.title == "" {
				m.title = string(z.Text())
			}
		case html.EndTagToken:
			if tok := z.Token(); tok.Data == "title" {
				inTitle = false
			}
		}
	}
}

// applyLink notes the oEmbed endpoint of <link rel="alternate"
// type="application/json+oembed" href>. XML oEmbed is not read.
func (m *pageMeta) applyLink(tok html.Token) {
	var rel, typ, href string
	for _, a := range tok.Attr {
		switch strings.ToLower(a.Key) {
		case "rel":
			rel = strings.ToLower(a.Val)
		case "type":
			typ = strings.ToLower(strings.TrimSpace(a.Val))
		case "href":
			href = strings.TrimSpace(a.Val)
		}
	}
	if m.oembed == "" && href != "" && strings.Contains(rel, "alternate") && typ == "application/json+oembed" {
		m.oembed = href
	}
}

// applyMeta stores the value of one <meta> tag.
func (m *pageMeta) applyMeta(tok html.Token) {
	var key, content string
	for _, a := range tok.Attr {
		switch strings.ToLower(a.Key) {
		case "property", "name":
			key = strings.ToLower(a.Val)
		case "content":
			content = a.Val
		}
	}
	first := func(dst *string) {
		if *dst == "" {
			*dst = content
		}
	}
	switch key {
	case "og:title":
		first(&m.og.Title)
	case "og:description":
		first(&m.og.Description)
	case "og:site_name", "og:site":
		first(&m.og.SiteName)
	case "og:image", "og:image:url", "og:image:secure_url":
		first(&m.og.Image)
	case "og:image:width":
		m.imgW = atoiCap(content)
	case "og:image:height":
		m.imgH = atoiCap(content)
	case "twitter:title":
		first(&m.tw.Title)
	case "twitter:description":
		first(&m.tw.Description)
	case "twitter:image", "twitter:image:src":
		first(&m.tw.Image)
	case "twitter:card":
		m.card = strings.ToLower(strings.TrimSpace(content))
	case "description":
		first(&m.desc)
	case "author", "article:author":
		// article:author is often a profile URL; only a name is useful here.
		if !strings.Contains(content, "://") {
			first(&m.author)
		}
	case "theme-color":
		first(&m.themeColor)
	}
}

func atoiCap(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 0 || n > 100000 {
		return 0
	}
	return n
}

func (m *pageMeta) finish(base *url.URL) *Preview {
	p := &Preview{
		Title:       cmp.Or(m.og.Title, m.tw.Title, m.title),
		Description: cmp.Or(m.og.Description, m.tw.Description, m.desc),
		SiteName:    m.og.SiteName,
		Image:       cmp.Or(m.og.Image, m.tw.Image),
		Author:      m.author,
		Color:       themeColor(m.themeColor),
		oembed:      m.oembed,
	}
	p.Title = clip(p.Title, maxTitleRunes)
	p.Description = clip(p.Description, maxDescRunes)
	p.SiteName = clip(p.SiteName, 80)
	p.Author = clip(p.Author, 100)
	if p.Image != "" {
		p.Image = absoluteHTTP(base, p.Image)
	}
	// A "summary" card, or a small near-square image, wants a thumbnail
	// beside the text. Anything else (and unknown sizes) gets the wide image.
	p.Compact = p.Image != "" && (m.card == "summary" ||
		(m.card == "" && m.imgW > 0 && m.imgH > 0 && m.imgW < 600 && m.imgW*10 < m.imgH*14 && m.imgH*10 < m.imgW*14))
	return p
}

// absoluteHTTP resolves raw against base and keeps it only when it is a
// short http(s) URL. An URL the image proxy would refuse anyway is not worth
// caching and sending to every client.
func absoluteHTTP(base *url.URL, raw string) string {
	u, err := base.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || len(u.String()) > maxURLLen {
		return ""
	}
	return u.String()
}

var hexColor = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// themeColor returns a page's theme colour as lowercase #rrggbb, or "" for
// anything else (named colours, rgba(), media-query variants).
func themeColor(raw string) string {
	raw = strings.TrimSpace(raw)
	if !hexColor.MatchString(raw) {
		return ""
	}
	raw = strings.ToLower(raw)
	if len(raw) == 4 {
		return "#" + strings.Repeat(string(raw[1]), 2) + strings.Repeat(string(raw[2]), 2) + strings.Repeat(string(raw[3]), 2)
	}
	return raw
}

func clip(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max-1]) + "…"
}
