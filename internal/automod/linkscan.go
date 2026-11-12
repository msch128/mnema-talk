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
// When the message costs more work than linkWork allows, it ends with an
// opaque link: a message that tangled may lead anywhere.
func links(content string) []link {
	sc := newScan(strings.ReplaceAll(content, "\x00", "")) // the client drops NUL before parsing
	if !sc.schemes() || !sc.wwwLinks() {
		return append(sc.out, link{opaque: true})
	}
	if len(sc.out) == 0 && !isASCII(sc.s) && foldedLink.MatchString(fold(sc.s)) {
		// Only folding shows a link (full-width letters, an invisible
		// character inside the scheme): no client links it, but a member who
		// copies it gets one. It counts as a link nobody can read.
		sc.out = append(sc.out, link{})
	}
	return sc.out
}

// foldedLink is a link-like token in folded text, for the case above.
var foldedLink = regexp.MustCompile(`(?i)(?:https?://|\bwww\.)[^\s<>"'\x60]`)

// schemes judges every "http://" and "https://" of the text. Nested schemes
// share the end of their token: it is found once, and so is the run of
// trailing punctuation at its end. false: the work budget ran out.
func (sc *scan) schemes() bool {
	s := sc.s
	stop := -1 // the next character at or after the token that ends every link
	for i := 0; ; {
		k := strings.Index(s[i:], "://")
		if k < 0 {
			return true
		}
		j := i + k
		i = j + 1
		start, ok := schemeBefore(s, j)
		if !ok {
			continue
		}
		if stop < j+3 {
			stop = j + 3 + tokenLen(s[j+3:])
		}
		sc.spans = append(sc.spans, [2]int{start, stop})
		if !sc.scheme(start, j, stop) {
			return false
		}
	}
}

// scheme judges the link whose scheme starts at start, "://" at j, its token
// running to stop. An authority longer than the client's destination cap
// (maxDestination) may be cut anywhere by the client: opaque.
func (sc *scan) scheme(start, j, stop int) bool {
	rest, token := sc.s[j+3:], sc.s[j+3:stop]
	if utf16Longer(sc.s[start:j+3+authorityLen(token)], maxDestination) {
		sc.out = append(sc.out, link{opaque: true})
		return true
	}
	ls, ok := sc.schemeLinks(rest, token, j+3, framed(sc.s[:start]))
	sc.out = append(sc.out, ls...)
	return ok
}

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
// after it (at offset at of the message), token its start up to the first
// character that ends every link.
func (sc *scan) schemeLinks(rest, token string, at int, framed bool) ([]link, bool) {
	first, _ := utf8.DecodeRuneInString(rest)
	switch {
	case rest == "" || isJSSpace(first) || strings.ContainsRune("<>\"`", first):
		return nil, true // nothing after the scheme: not a link
	case first == '/' || first == '\\' || first < 0x20 || first == 0x7f:
		// The client links none of these; a browser's address bar skips the
		// slashes. Judge that host, but never as one that is fine.
		if first == '/' {
			n := len(token)
			token = strings.TrimLeft(token, "/")
			at += n - len(token)
		} else { // the token ended right here: read on past the stop
			trimmed := strings.TrimLeft(rest[1:], "/\\")
			at += len(rest) - len(trimmed)
			token = trimmed[:tokenLen(trimmed)]
		}
		ls, ok := sc.authorityLinks(token, token[:authorityLen(token)], at, framed)
		for i := range ls {
			ls[i].ok = false
		}
		return ls, ok
	}
	return sc.authorityLinks(token, token[:authorityLen(token)], at, framed)
}

// wwwLinks judges the "www." tokens outside the http(s) tokens and outside
// each other. false: the work budget ran out.
func (sc *scan) wwwLinks() bool {
	s := sc.s
	for i := 0; ; {
		k := indexFold(s[i:], "www.")
		if k < 0 {
			return true
		}
		at := i + k
		i = at + 4
		if at > 0 && isWordByte(s[at-1]) || inSpan(at, sc.spans) {
			continue
		}
		rest := s[at:]
		if r, _ := utf8.DecodeRuneInString(rest[4:]); len(rest) == 4 || linkStop(r) {
			continue
		}
		token := rest[:tokenLen(rest)]
		i = at + len(token)
		ls, ok := sc.authorityLinks(token, token[:authorityLen(token)], at, false)
		sc.out = append(sc.out, ls...)
		if !ok {
			return false
		}
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
