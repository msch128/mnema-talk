package mdplain

import "strings"

// safeHref mirrors the client: http(s) with a host, or mailto:, without
// spaces, quotes, backslashes or control characters.
func safeHref(raw string) bool {
	switch {
	case hasPrefixFold(raw, "https://"):
		raw = raw[len("https://"):]
	case hasPrefixFold(raw, "http://"):
		raw = raw[len("http://"):]
	case hasPrefixFold(raw, "mailto:"):
		raw = raw[len("mailto:"):]
	default:
		return false
	}
	if raw == "" || raw[0] == '/' {
		return false
	}
	return !strings.ContainsFunc(raw, func(r rune) bool {
		return r <= 0x1f || r == 0x7f || r == ' ' || strings.ContainsRune("<>\"'`\\", r)
	})
}

// hasPrefixFold reports whether s starts with the lower-case ASCII prefix,
// ignoring case, without copying s.
func hasPrefixFold(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		c := s[i]
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != prefix[i] {
			return false
		}
	}
	return true
}

// autolink reads <https://example.com>: the address is the text.
func autolink(src string, i int, noLinks bool) (string, int, bool) {
	if noLinks {
		return "", i, false
	}
	end := strings.IndexAny(src[i+1:], "> \t\r\n<")
	if end < 0 || src[i+1+end] != '>' {
		return "", i, false
	}
	href := src[i+1 : i+1+end]
	lower := strings.ToLower(href)
	if !(strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "mailto:")) || !safeHref(href) {
		return "", i, false
	}
	return href, i + end + 2, true
}

// trailing is what ends a sentence or closes surrounding markup, not the URL.
const trailing = ".,:;!?'\"*_~|"

func trimURL(raw string) string {
	url := raw
	if k := strings.Index(url, "||"); k > 0 {
		url = url[:k]
	}
	for url != "" {
		last := url[len(url)-1]
		closing := (last == ')' && strings.Count(url, ")") > strings.Count(url, "(")) ||
			(last == ']' && strings.Count(url, "]") > strings.Count(url, "["))
		if !closing && !strings.ContainsRune(trailing, rune(last)) {
			break
		}
		url = url[:len(url)-1]
	}
	return url
}

// bareURL keeps a plain http(s) address as it is, so its underscores and
// asterisks are never read as formatting.
func bareURL(src string, i int, noLinks bool) (string, int, bool) {
	if noLinks || (i > 0 && isWord(src[i-1])) {
		return "", i, false
	}
	rest := src[i:]
	if !hasPrefixFold(rest, "http://") && !hasPrefixFold(rest, "https://") {
		return "", i, false
	}
	end := strings.IndexAny(rest, " \t\r\n<")
	if end < 0 {
		end = len(rest)
	}
	url := trimURL(rest[:end])
	if !safeHref(url) {
		return "", i, false
	}
	return url, i + len(url), true
}

// labelEnd is the index of the "]" closing the "[" at i, or -1.
func labelEnd(src string, i int) int {
	depth := 0
	for j := i; j < len(src) && j < i+maxLabel; j++ {
		switch src[j] {
		case '\\':
			j++
		case '[':
			depth++
		case ']':
			if depth--; depth == 0 {
				return j
			}
		}
	}
	return -1
}

// destination reads "(url)", "(<url>)" or `(url "title")` at i; the title is dropped.
func destination(src string, i int) (string, int, bool) {
	if i >= len(src) || src[i] != '(' {
		return "", 0, false
	}
	angled := i+1 < len(src) && src[i+1] == '<'
	start := i + 1
	if angled {
		start++
	}
	j := destinationEnd(src, start, angled)
	url := src[start:min(j, len(src))]
	if angled {
		j++
	}
	end, ok := closeDestination(src, j)
	return url, end, ok
}

// destinationEnd is where the address starting at start stops.
func destinationEnd(src string, start int, angled bool) int {
	depth, j := 0, start
	for ; j < len(src) && j < start+maxLabel*4; j++ {
		if endsDestination(src[j], angled, depth) {
			break
		}
		switch src[j] {
		case '(':
			depth++
		case ')':
			depth--
		}
	}
	return j
}

func endsDestination(ch byte, angled bool, depth int) bool {
	if angled {
		return ch == '>'
	}
	return (ch == ')' && depth == 0) || isSpace(ch)
}

// closeDestination skips an optional quoted title and the closing ")".
func closeDestination(src string, j int) (int, bool) {
	k := j
	for k < len(src) && isSpace(src[k]) {
		k++
	}
	if k < len(src) && (src[k] == '"' || src[k] == '\'') {
		if q := strings.IndexByte(src[k+1:], src[k]); q >= 0 && !strings.Contains(src[k+1:k+1+q], "\n") {
			k += q + 2
		}
	}
	for k < len(src) && isSpace(src[k]) {
		k++
	}
	if k < len(src) && src[k] == ')' {
		return k + 1, true
	}
	return 0, false
}

// masked reads [label](url): the label is the text.
func (s *scanner) masked(src string, i, depth int, noLinks bool) (string, int, bool) {
	if noLinks || depth >= maxDepth {
		return "", i, false
	}
	closeAt := labelEnd(src, i)
	if closeAt < 0 || closeAt == i+1 {
		return "", i, false
	}
	url, end, ok := destination(src, closeAt+1)
	if !ok || !safeHref(url) {
		return "", i, false
	}
	return s.inline(src[i+1:closeAt], depth+1, true), end, true
}
