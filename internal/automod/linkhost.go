package automod

import (
	"errors"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/idna"
)

// The host of a link as a browser's URL parser reads it (the WHATWG URL
// Standard, which the web client's new URL() follows). Go's net/url is not
// that parser: it differs in percent-decoding, IDNA, IPv4 shorthand and ports,
// so the steps are written out here, and where Go cannot reproduce a step
// exactly the reading says so instead of guessing.

// reading is one host read from an authority.
type reading struct {
	host string // best effort even when not exact: lower case, punycode, no trailing dot
	// exact: the browser opens exactly this host. Otherwise rejected tells
	// whether the URL Standard refuses the authority (then the client links
	// nothing from it) or only Go's IDNA tables do (then the browser may still
	// open it).
	exact, rejected bool
}

// browserIDNA is UTS #46 the way the URL Standard calls it (domain to ASCII,
// beStrict false): non-transitional, no STD3 rules, no hyphen check, with the
// joiner and bidi checks, no DNS length check.
var browserIDNA = idna.New(idna.MapForLookup(), idna.BidiRule(), idna.Transitional(false),
	idna.StrictDomainName(false), idna.CheckHyphens(false), idna.CheckJoiners(true), idna.VerifyDNSLength(false))

// readAuthority reads the host of an authority ("user:pw@host:port"): the
// part after the last "@", without the port.
func readAuthority(authority string) reading {
	hostport := authority[strings.LastIndexByte(authority, '@')+1:]
	if strings.HasPrefix(hostport, "[") {
		return readIPv6(hostport)
	}
	host, port, _ := strings.Cut(hostport, ":")
	r := readDomain(host)
	if !validPort(port) {
		r.exact, r.rejected = false, true
	}
	return r
}

// validPort: empty, or digits up to 65535.
func validPort(p string) bool {
	if p == "" {
		return true
	}
	if strings.Trim(p, "0123456789") != "" {
		return false
	}
	p = strings.TrimLeft(p, "0")
	n, err := strconv.Atoi(p)
	return p == "" || err == nil && n <= 65535
}

// readIPv6 reads "[address]" or "[address]:port". An IP literal is never a
// listed domain; it only has to be told apart from what the client rejects.
func readIPv6(hostport string) reading {
	end := strings.IndexByte(hostport, ']')
	if end < 0 {
		return reading{host: strings.ToLower(hostport), rejected: true}
	}
	addr, err := netip.ParseAddr(hostport[1:end])
	rest := hostport[end+1:]
	if err != nil || !addr.Is6() || addr.Zone() != "" || rest != "" && (rest[0] != ':' || !validPort(rest[1:])) {
		return reading{host: strings.ToLower(hostport[:end+1]), rejected: true}
	}
	return reading{host: "[" + addr.String() + "]", exact: true}
}

// readDomain is the URL Standard's host parser for a special scheme, past the
// IPv6 case: percent-decode, domain to ASCII, forbidden code points, IPv4.
func readDomain(raw string) reading {
	d := strings.ToValidUTF8(percentDecode(raw), "\ufffd")
	var host string
	exact := true
	if isASCII(d) && !hasACELabel(d) {
		host = strings.ToLower(d) // the URL Standard's ASCII fast path
	} else {
		var err error
		host, err = browserIDNA.ToASCII(d)
		host, exact = strings.ToLower(host), err == nil
		if kept, ok := keepEmptyACE(d, host); ok {
			host, exact = kept, false
		}
	}
	if host == "" || strings.ContainsFunc(host, forbiddenInDomain) {
		return reading{host: strings.TrimRight(host, "."), rejected: true}
	}
	if endsInNumber(host) {
		ip, ok := parseIPv4(host)
		if !ok {
			return reading{host: host, rejected: true}
		}
		host = ip
	}
	return reading{host: strings.TrimRight(host, "."), exact: exact}
}

// keepEmptyACE puts back the labels Go's IDNA empties: a label that maps to
// the bare ACE prefix "xn--" decodes to nothing there, without an error.
// UTS #46 calls it an error, but some URL parsers keep the label as it is, so
// the host is read with it, and never as exact. ok: there was such a label.
func keepEmptyACE(d, host string) (string, bool) {
	if host != "" && !strings.HasPrefix(host, ".") && !strings.HasSuffix(host, ".") && !strings.Contains(host, "..") {
		return host, false // no label was emptied
	}
	mapped := strings.Split(mapEachRune(d), ".")
	if !slices.Contains(mapped, acePrefix) {
		return host, false
	}
	out := strings.Split(host, ".")
	if len(out) != len(mapped) {
		return host, true
	}
	for i, label := range mapped {
		if label == acePrefix {
			out[i] = acePrefix
		}
	}
	return strings.Join(out, "."), true
}

const acePrefix = "xn--"

// mapEachRune is the UTS #46 mapping of d, one code point at a time: enough
// to tell which labels map to the ACE prefix, as no composition makes one.
func mapEachRune(d string) string {
	var b strings.Builder
	for _, r := range d {
		if r < utf8.RuneSelf {
			b.WriteByte(byte(unicode.ToLower(r)))
			continue
		}
		m, _ := browserIDNA.ToUnicode(string(r))
		b.WriteString(m)
	}
	return b.String()
}

// percentDecode decodes %XX and leaves a "%" without two hex digits as it is.
func percentDecode(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
			v, _ := strconv.ParseUint(s[i+1:i+3], 16, 8)
			b.WriteByte(byte(v))
			i += 2
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

func hasACELabel(d string) bool {
	for label := range strings.SplitSeq(d, ".") {
		if len(label) >= len(acePrefix) && strings.EqualFold(label[:len(acePrefix)], acePrefix) {
			return true
		}
	}
	return false
}

// forbiddenInDomain is the URL Standard's forbidden domain code point.
func forbiddenInDomain(r rune) bool {
	return r <= 0x20 || r == 0x7f || strings.ContainsRune("#%/:<>?@[\\]^|", r)
}

// endsInNumber: the last label (a trailing empty one aside) is decimal digits
// or 0x hex, so the URL Standard reads the host as IPv4.
func endsInNumber(host string) bool {
	labels := strings.Split(host, ".")
	if len(labels) > 1 && labels[len(labels)-1] == "" {
		labels = labels[:len(labels)-1]
	}
	last := labels[len(labels)-1]
	if last != "" && strings.Trim(last, "0123456789") == "" {
		return true
	}
	return len(last) >= 2 && (last[:2] == "0x" || last[:2] == "0X") && strings.IndexFunc(last[2:], func(r rune) bool { return r > 0x7f || !isHex(byte(r)) }) < 0
}

// parseIPv4 is the URL Standard's IPv4 parser: up to four parts, each
// decimal, octal (leading 0) or hex (0x), the last one filling the rest.
func parseIPv4(host string) (string, bool) {
	parts := strings.Split(host, ".")
	if len(parts) > 1 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) > 4 {
		return "", false
	}
	var ip uint64
	for i, p := range parts {
		n, ok := ipv4Number(p)
		last := i == len(parts)-1
		if !ok || !last && n > 255 || last && n >= 1<<(8*(5-len(parts))) {
			return "", false
		}
		if last {
			ip += n
		} else {
			ip += n << (8 * (3 - i))
		}
	}
	return netip.AddrFrom4([4]byte{byte(ip >> 24), byte(ip >> 16), byte(ip >> 8), byte(ip)}).String(), true
}

func ipv4Number(p string) (uint64, bool) {
	base := 10
	switch {
	case len(p) >= 2 && (p[:2] == "0x" || p[:2] == "0X"):
		p, base = p[2:], 16
	case len(p) >= 2 && p[0] == '0':
		p, base = p[1:], 8
	case p == "":
		return 0, false
	}
	if p == "" {
		return 0, true
	}
	n, err := strconv.ParseUint(p, base, 64)
	if err != nil {
		// Too many digits still counts as a number, just too big for IPv4.
		return 1 << 40, errors.Is(err, strconv.ErrRange)
	}
	return min(n, 1<<40), true
}

// reachable reports whether a host can be resolved at all: an IP address, or
// a name whose last label holds only letters, digits, "-" and "_" (a single
// label can be completed by a search domain). "allowed.example's" parses as
// a URL host, but no resolver answers for it.
func reachable(host string) bool {
	if strings.HasPrefix(host, "[") {
		return true
	}
	last := host[strings.LastIndexByte(host, '.')+1:]
	return last != "" && strings.Trim(last, "abcdefghijklmnopqrstuvwxyz0123456789-_") == ""
}
