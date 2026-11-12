package automod

// The work of one scan and the limit that is not one of the client's ends:
// the client's cap on a link target.

// maxDestination is how far the client reads the target of [text](url): at
// most 2000 UTF-16 code units (inline.ts destination, MAX_LABEL * 4), then it
// cuts the URL there. A cut inside the authority opens the host up to the
// cut, which may be any prefix of what the server reads, so an authority
// that long (scheme included) is opaque. No host is that long: DNS names end
// at 253 characters.
const maxDestination = 2000

// linkWork is the work one message may cost: every reading of a host costs
// its length in bytes plus 16 (four times that when IDNA runs on it), and
// trimming or counting brackets an eighth of the length trimmed. An ordinary
// message of 4000 characters full of links costs a fraction of it; one that
// costs more is tangled on purpose and counts as carrying an opaque link.
const linkWork = 1 << 15

// scan is the state of one links() call.
type scan struct {
	s     string
	out   []link
	spans [][2]int // the http(s) tokens, for www.
	work  int
	// The shared end of nested schemes: the first "||" at or after barFrom
	// in the token ending at barEnd (barAt, -1 for none), and where the run
	// of trailing characters before tailEnd starts.
	barFrom, barEnd, barAt int
	tailEnd, tailStart     int
}

func newScan(s string) *scan {
	return &scan{s: s, work: linkWork, barEnd: -1, barAt: -1, tailEnd: -1}
}

func (sc *scan) spend(n int) bool {
	sc.work -= n
	return sc.work >= 0
}

// spendReading pays for reading the host of authority a.
func (sc *scan) spendReading(a string) bool {
	cost := len(a) + 16
	if !isASCII(a) {
		cost *= 4
	}
	return sc.spend(cost)
}

// utf16Len is the length of s in UTF-16 code units, as JavaScript counts it.
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n++
		if r > 0xffff {
			n++
		}
	}
	return n
}

// utf16Longer reports whether s is longer than n UTF-16 code units.
func utf16Longer(s string, n int) bool {
	if len(s) <= n {
		return false // never more code units than bytes
	}
	return utf16Len(s) > n
}
