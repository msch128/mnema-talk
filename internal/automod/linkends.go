package automod

import (
	"slices"
	"strings"
)

// Where the client can end the host of a link, and which hosts that gives.
// The client's bare link (inline.ts matchBare) runs to the first character
// that ends every link, is cut at "||" and loses trailing punctuation
// (trimUrl); inside **, *, __, _, ~~ or || it also ends at the closer, since
// the formatted text is parsed on its own. A target of [text](url) or <url>
// (framed) is taken as written up to white space, ")" or ">".

// maxEnds bounds the readings of one token. An authority with more places to
// end it is no link anyone writes; it counts as one that may lead anywhere.
const maxEnds = 16

// clientTrailing is what the client cuts off the end of a bare link (inline.ts
// TRAILING); ")" and "]" go too when they are unbalanced.
const clientTrailing = ".,:;!?'\"*_~|"

// authorityLinks judges every host the client could open from one token:
// token runs to the first character that ends every link (it starts at
// offset at of the message), authority is its start up to / ? # (where a
// browser's authority ends). A reading the URL Standard rejects is left out
// when another reading exists: the client cannot link it. When every reading
// is rejected, the token is one link nobody can read. false: the work budget
// ran out.
func (sc *scan) authorityLinks(token, authority string, at int, framed bool) ([]link, bool) {
	var out []link
	fallback := link{}
	ends, overflow, ok := sc.clientEnds(token, authority, at, framed)
	if !ok {
		return nil, false
	}
	for _, end := range ends {
		if !sc.spendReading(authority[:end]) {
			return out, false
		}
		r := readAuthority(authority[:end])
		if fallback.host == "" {
			fallback.host = r.host
		}
		if r.rejected {
			continue
		}
		if l := (link{host: r.host, ok: r.exact}); !slices.Contains(out, l) {
			out = append(out, l)
		}
	}
	if overflow {
		out = append(out, link{opaque: true})
	}
	if len(out) == 0 {
		return []link{fallback}, true
	}
	return out, true
}

// clientEnds lists where the client could end the host inside authority, at
// most maxEnds of them (overflow: there were more), each once.
func (sc *scan) clientEnds(token, authority string, at int, framed bool) (ends []int, overflow, ok bool) {
	bare, ok := sc.bareLink(token, at)
	if !ok {
		return nil, false, false
	}
	ends = []int{min(len(bare), len(authority))}
	if framed {
		ends = addEnd(ends, len(authority))
	}
	a := authority
	for i := 0; i < len(a) && !overflow; i++ {
		switch {
		case framed && a[i] == ')':
			ends = addEnd(ends, i) // the end of [text](url)
		case closerAt(a, i):
			if !sc.spend(i/8 + 1) {
				return nil, false, false
			}
			ends = addEnd(ends, len(trimClient(a[:i])))
			// A cut anywhere in a run of trailing characters reads the same
			// host as one at its start ("]" is not one: it may stay).
			for strings.IndexByte(clientTrailing, a[i]) >= 0 && i+1 < len(a) && strings.IndexByte(clientTrailing, a[i+1]) >= 0 {
				i++
			}
		}
		overflow = len(ends) > maxEnds
	}
	if overflow {
		ends = ends[:maxEnds]
	}
	return ends, overflow, true
}

// addEnd adds an end once: each host is read once.
func addEnd(ends []int, end int) []int {
	if slices.Contains(ends, end) {
		return ends
	}
	return append(ends, end)
}

// bareLink is the client's bare link of the token at offset at: cut at the
// first "||", then trimmed (trimUrl). Nested schemes share their token, so
// the "||" and the run of trailing characters at its end are found once.
func (sc *scan) bareLink(token string, at int) (string, bool) {
	end := at + len(token)
	if sc.barEnd != end || sc.barFrom > at || sc.barAt >= 0 && sc.barAt < at {
		sc.barFrom, sc.barEnd, sc.barAt = at, end, -1
		if i := strings.Index(token, "||"); i >= 0 {
			sc.barAt = at + i
		}
	}
	if sc.barAt >= 0 {
		end = sc.barAt
	}
	if sc.tailEnd != end {
		sc.tailEnd, sc.tailStart = end, end
		for sc.tailStart > 0 && strings.IndexByte(clientTrailing, sc.s[sc.tailStart-1]) >= 0 {
			sc.tailStart--
		}
	}
	u := sc.s[at:max(at, sc.tailStart)]
	if u != "" && (u[len(u)-1] == ')' || u[len(u)-1] == ']') && !sc.spend(len(u)/8+1) {
		return "", false // trimClient counts brackets over all of u
	}
	return trimClient(u), true
}

// closerAt reports whether a Markdown closer can stand at a[i]: ** or *, __
// or a _ that ends a word, ~~, ||. The "]" of a link label is one too: a link
// inside a label is never linked, so ending there only judges a host more.
func closerAt(a string, i int) bool {
	next := byte(0)
	if i+1 < len(a) {
		next = a[i+1]
	}
	switch a[i] {
	case '*', ']':
		return true
	case '_':
		return next == '_' || !isWordByte(next)
	case '~', '|':
		return next == a[i]
	}
	return false
}

// trimClient is the client's trimUrl after the "||" cut: trailing punctuation
// and Markdown markers go, a ")" or "]" only when it is unbalanced.
func trimClient(u string) string {
	counted := false
	var opens, closes, openB, closeB int
	for u != "" {
		last := u[len(u)-1]
		if (last == ')' || last == ']') && !counted {
			opens, closes = strings.Count(u, "("), strings.Count(u, ")")
			openB, closeB = strings.Count(u, "["), strings.Count(u, "]")
			counted = true
		}
		switch {
		case last == ')' && closes > opens:
			closes--
		case last == ']' && closeB > openB:
			closeB--
		case strings.IndexByte(clientTrailing, last) < 0:
			return u
		}
		u = u[:len(u)-1]
	}
	return u
}
