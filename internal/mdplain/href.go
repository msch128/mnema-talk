package mdplain

// safeHref is the client's safeHref (link.ts): the SAFE_HREF pattern, then
// the browser's URL parser must accept it with a host (http, https) or a
// path (mailto). The URL parser's host rules are reproduced for ASCII hosts
// (host.go); for other hosts (non-ASCII, percent signs, punycode, IPv6) the
// answer is not known here: sure is false, and the caller decides whether
// the answer matters (see decides).
// inClass: raw is known to hold only URL units (a bare URL), so only the
// authority is read.
func (p *parser) safeHref(raw []uint16, inClass bool) (ok, sure bool) {
	rest, mailto, ok := hrefRest(raw)
	if !ok {
		return false, true
	}
	authority := len(rest)
	for k, c := range rest {
		if !p.tick() || !urlUnit(c) {
			return false, true
		}
		if authority == len(rest) && (c == '/' || c == '?' || c == '#') {
			authority = k
			if inClass {
				break
			}
		}
	}
	if mailto {
		return rest[0] != '?' && rest[0] != '#', true
	}
	sure, valid := hostValid(decode(rest[:authority]))
	return valid, sure
}

// decides reports whether reading span as a link or as text can change what
// is hidden: it holds a unit that starts or ends a construct, or a place
// where another bare URL could start.
func decides(span []uint16) bool {
	for k, c := range span {
		switch c {
		case '|', '*', '_', '~', '[', '`', '\\', '<':
			return true
		case 'h', 'H':
			if k > 0 && !wordAt(span, k-1) {
				return true
			}
		}
	}
	return false
}

// hrefRest is raw after "http://", "https://" or "mailto:"; ok is false when
// the scheme is another or nothing follows (or a "/" follows "//").
func hrefRest(raw []uint16) ([]uint16, bool, bool) {
	switch {
	case hasAtFold(raw, 0, "https://"):
		raw = raw[len("https://"):]
	case hasAtFold(raw, 0, "http://"):
		raw = raw[len("http://"):]
	case hasAtFold(raw, 0, "mailto:"):
		raw = raw[len("mailto:"):]
		return raw, true, len(raw) > 0
	default:
		return nil, false, false
	}
	return raw, false, len(raw) > 0 && raw[0] != '/'
}
