package automod

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// How the link rules find links: on the raw text, the way the web client's
// Markdown parser (web/src/shared/lib/markdown/inline.ts) finds the links it
// renders, so that every host the client would open is a host the rules
// judge. The client links a URL that starts with http:// or https:// bare, as
// <url> and as [text](url); it ends one at white space (as JavaScript's \s
// knows it), at control characters and at < > " ` \, and a bare one also
// where a Markdown closer (** * __ _ ~~ ||) stands, cutting trailing
// punctuation. Instead of deciding which of these applies, every place where
// the client could end the host is judged (linkends.go): a message passes
// only if every reading passes.
//
// Every "http://" and "https://" in the text is judged, also one inside
// another link: when the client cannot parse the outer link (a port above
// 65535, say) it links the inner one. "www." starts a link too, as it always
// did, although the client does not link it.

// isJSSpace is JavaScript's \s: white space and line terminators.
func isJSSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

// linkStop ends every link the client renders (inline.ts BARE_URL, link.ts SAFE_HREF).
func linkStop(r rune) bool {
	return r < 0x20 || r == 0x7f || isJSSpace(r) || strings.ContainsRune("<>\"`\\", r)
}

// links lists the links in content: for each link-like token, every host the
// client could open from it, or one unreadable link when no host can be told.
func links(content string) []link {
	s := strings.ReplaceAll(content, "\x00", "") // the client drops NUL before parsing
	var out []link
	var spans [][2]int
	stop := -1 // the next character at or after the token that ends every link
	for i := 0; ; {
		k := strings.Index(s[i:], "://")
		if k < 0 {
			break
		}
		j := i + k
		if start, ok := schemeBefore(s, j); ok {
			if stop < j+3 {
				stop = j + 3 + tokenLen(s[j+3:]) // nested schemes share the end: one scan
			}
			spans = append(spans, [2]int{start, stop})
			out = append(out, schemeLinks(s[j+3:], s[j+3:stop], framed(s[:start]))...)
		}
		i = j + 1
	}
	out = append(out, wwwLinks(s, spans)...)
	if len(out) == 0 && !isASCII(s) && foldedLink.MatchString(fold(s)) {
		// Only folding shows a link (full-width letters, an invisible
		// character inside the scheme): no client links it, but a member who
		// copies it gets one. It counts as a link nobody can read.
		out = append(out, link{})
	}
	return out
}

// foldedLink is a link-like token in folded text, for the case above.
var foldedLink = regexp.MustCompile(`(?i)(?:https?://|\bwww\.)[^\s<>"'\x60]`)

// schemeBefore reports whether "http" or "https" (any case) ends at j.
func schemeBefore(s string, j int) (int, bool) {
	if j >= 5 && strings.EqualFold(s[j-5:j], "https") {
		return j - 5, true
	}
	return j - 4, j >= 4 && strings.EqualFold(s[j-4:j], "http")
}

// framed reports whether a link starting after before can be the target of
// [text](url), [text](<url>) or <url>: then the client takes it as written,
// without cutting anything off its end.
func framed(before string) bool {
	return strings.HasSuffix(before, "<") || strings.HasSuffix(before, "](")
}

// tokenLen is the length of the run up to the first character that ends every link.
func tokenLen(s string) int {
	if i := strings.IndexFunc(s, linkStop); i >= 0 {
		return i
	}
	return len(s)
}

// authorityLen is where the authority ends for a browser (/ ? # or \) or the
// client's link ends.
func authorityLen(s string) int {
	if i := strings.IndexFunc(s, func(r rune) bool { return linkStop(r) || r == '/' || r == '?' || r == '#' }); i >= 0 {
		return i
	}
	return len(s)
}

// schemeLinks reads what follows "http://" or "https://": rest is the text
// after it, token its start up to the first character that ends every link.
func schemeLinks(rest, token string, framed bool) []link {
	first, _ := utf8.DecodeRuneInString(rest)
	switch {
	case rest == "" || isJSSpace(first) || strings.ContainsRune("<>\"`", first):
		return nil // nothing after the scheme: not a link
	case first == '/' || first == '\\' || first < 0x20 || first == 0x7f:
		// The client links none of these; a browser's address bar skips the
		// slashes. Judge that host, but never as one that is fine.
		if first == '/' {
			token = strings.TrimLeft(token, "/")
		} else { // the token ended right here: read on past the stop
			rest = strings.TrimLeft(rest[1:], "/\\")
			token = rest[:tokenLen(rest)]
		}
		ls := authorityLinks(token, token[:authorityLen(token)], framed)
		for i := range ls {
			ls[i].ok = false
		}
		return ls
	}
	return authorityLinks(token, token[:authorityLen(token)], framed)
}

// wwwLinks are the "www." tokens outside the http(s) tokens and outside each
// other.
func wwwLinks(s string, spans [][2]int) []link {
	var out []link
	for i := 0; ; {
		k := indexFold(s[i:], "www.")
		if k < 0 {
			return out
		}
		at := i + k
		i = at + 4
		if at > 0 && isWordByte(s[at-1]) || inSpan(at, spans) {
			continue
		}
		rest := s[at:]
		if r, _ := utf8.DecodeRuneInString(rest[4:]); len(rest) == 4 || linkStop(r) {
			continue
		}
		token := rest[:tokenLen(rest)]
		i = at + len(token)
		out = append(out, authorityLinks(token, token[:authorityLen(token)], false)...)
	}
}

func indexFold(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if strings.EqualFold(s[i:i+len(sub)], sub) {
			return i
		}
	}
	return -1
}

func inSpan(at int, spans [][2]int) bool {
	for _, sp := range spans {
		if at >= sp[0] && at < sp[1] {
			return true
		}
	}
	return false
}

func isWordByte(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}
