package mdplain

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// The WHATWG URL parser's host rules for ASCII hosts, which the browser
// applies in the client's safeHref (new URL()), and the client's own rule
// for the authority (linkAuthority.ts plainAuthority).

// forbiddenDomain are the URL standard's forbidden domain code points (the
// controls apart, which never reach a host here).
const forbiddenDomain = " #%/:<>?@[\\]^|"

// hostValid parses the authority of an http(s) URL; sure is false when this
// code cannot tell what the browser decides (an IPv6 address, non-ASCII,
// punycode).
func hostValid(rest string) (sure, valid bool) {
	authority := rest
	if at := strings.IndexAny(rest, "/?#"); at >= 0 {
		authority = rest[:at]
	}
	if !plainAuthority(authority) {
		return true, false
	}
	if strings.HasPrefix(authority, "[") {
		return ipv6Literal(authority)
	}
	host, port, hasPort := strings.Cut(authority, ":")
	if hasPort && !validPort(port) {
		return true, false
	}
	// The URL parser reads the host percent-decoded (plainAuthority made sure it decodes).
	host, _ = percentDecode(host)
	if !isASCII(host) {
		return false, false
	}
	if host == "" || strings.ContainsAny(host, forbiddenDomain) || strings.ContainsFunc(host, unicode.IsControl) {
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

// plainAuthority is the client's rule (linkAuthority.ts): no user info, the
// percent escapes decode as UTF-8, and neither the authority as written nor
// decoded holds a control or format character. (The client also tests the
// hostname the URL parser makes; for an ASCII host it is the decoded host,
// for any other host this code is not sure anyway.)
func plainAuthority(authority string) bool {
	if strings.Contains(authority, "@") {
		return false
	}
	text, ok := percentDecode(authority)
	return ok && !hidden(authority) && !hidden(text)
}

// hidden is /[\p{Cc}\p{Cf}]/u.
func hidden(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool { return unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) })
}

// percentDecode is decodeURIComponent: false when an escape is malformed or
// the escaped bytes are no UTF-8.
func percentDecode(s string) (string, bool) {
	if !strings.Contains(s, "%") {
		return s, true
	}
	var b []byte
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			b = append(b, s[i])
			continue
		}
		if i+2 >= len(s) || !isHex(s[i+1]) || !isHex(s[i+2]) {
			return "", false
		}
		b = append(b, unhex(s[i+1])<<4|unhex(s[i+2]))
		i += 2
	}
	return string(b), utf8.Valid(b)
}

func isHex(c byte) bool { return (c >= '0' && c <= '9') || (c|0x20 >= 'a' && c|0x20 <= 'f') }

func unhex(c byte) byte {
	if c <= '9' {
		return c - '0'
	}
	return c | 0x20 - 'a' + 10
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
