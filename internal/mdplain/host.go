package mdplain

import "strings"

// The WHATWG URL parser's host rules for ASCII hosts, which the browser
// applies in the client's safeHref (new URL()).

// hostValid parses the authority of an http(s) URL; sure is false when this
// code cannot tell what the browser decides (an IP literal, a percent sign,
// non-ASCII, punycode).
func hostValid(rest string) (sure, valid bool) {
	authority := rest
	if at := strings.IndexAny(rest, "/?#"); at >= 0 {
		authority = rest[:at]
	}
	if at := strings.LastIndexByte(authority, '@'); at >= 0 {
		authority = authority[at+1:]
	}
	if strings.HasPrefix(authority, "[") {
		return ipv6Literal(authority)
	}
	if strings.Contains(authority, "%") || !isASCII(authority) {
		return false, false
	}
	host, port, hasPort := strings.Cut(authority, ":")
	if (hasPort && !validPort(port)) || host == "" || strings.ContainsAny(host, "[]^|") {
		return true, false
	}
	labels := strings.Split(strings.ToLower(host), ".")
	for _, l := range labels {
		if strings.HasPrefix(l, "xn--") {
			return false, false
		}
	}
	if !endsInNumber(labels) {
		return true, true
	}
	return true, validIPv4(labels)
}

// ipv6Literal judges "[...]" with an optional port: refused when it is not
// closed, when something else follows it or when it holds a unit no IPv6
// address has; whether the address itself parses is left open.
func ipv6Literal(authority string) (sure, valid bool) {
	end := strings.IndexByte(authority, ']')
	if end < 0 {
		return true, false
	}
	if after := authority[end+1:]; after != "" && (after[0] != ':' || !validPort(after[1:])) {
		return true, false
	}
	if strings.Trim(authority[1:end], "0123456789abcdefABCDEF:.") != "" {
		return true, false
	}
	return false, false
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func validPort(port string) bool {
	value := 0
	for _, c := range port {
		if c < '0' || c > '9' {
			return false
		}
		if value = value*10 + int(c-'0'); value > 65535 {
			return false
		}
	}
	return true
}

// endsInNumber is the standard's "ends in a number" check.
func endsInNumber(labels []string) bool {
	if labels[len(labels)-1] == "" {
		if len(labels) == 1 {
			return false
		}
		labels = labels[:len(labels)-1]
	}
	last := labels[len(labels)-1]
	if last != "" && strings.Trim(last, "0123456789") == "" {
		return true
	}
	_, ok := ipv4Number(last)
	return ok
}

// ipv4Number is the standard's IPv4 number parser, saturating above 2^33.
func ipv4Number(s string) (uint64, bool) {
	if s == "" {
		return 0, false
	}
	radix := uint64(10)
	switch {
	case strings.HasPrefix(s, "0x"):
		s, radix = s[2:], 16
	case len(s) >= 2 && s[0] == '0':
		s, radix = s[1:], 8
	}
	var n uint64
	for i := 0; i < len(s); i++ {
		d := strings.IndexByte("0123456789abcdef", s[i])
		if d < 0 || uint64(d) >= radix {
			return 0, false
		}
		n = min(n*radix+uint64(d), 1<<33)
	}
	return n, true
}

// validIPv4 is the standard's IPv4 parser: true when it does not fail.
func validIPv4(labels []string) bool {
	if labels[len(labels)-1] == "" {
		labels = labels[:len(labels)-1]
	}
	if len(labels) > 4 {
		return false
	}
	for i, l := range labels {
		n, ok := ipv4Number(l)
		last := i == len(labels)-1
		if !ok || (!last && n > 255) || (last && n >= 1<<(8*(5-len(labels)))) {
			return false
		}
	}
	return true
}
