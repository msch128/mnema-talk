package mdplain

// Precomputed answers to the client's forward scans for masked links, so a
// run of unclosed "[" or "(" costs linear time: the client rescans up to
// maxLabel (label) or 4*maxLabel (destination) units per opener. Each table
// is built once per run, on the first opener that needs it.

// labelIndex: for every unescaped "[", the "]" that brings the bracket depth
// back to where it was (the first one; -1 when there is none).
type labelIndex struct{ closeAt []int32 }

// destIndex: for every position, the first ")" at or after it at the same
// parenthesis depth, the first white space and the first ">" (len when none).
type destIndex struct{ paren, space, angle []int32 }

func (r *run) labels() *labelIndex {
	if r.label == nil {
		r.label = buildLabelIndex(r.src)
		r.p.steps += 2 * len(r.src)
	}
	return r.label
}

func (r *run) dests() *destIndex {
	if r.dest == nil {
		r.dest = buildDestIndex(r.src)
		r.p.steps += 2 * len(r.src)
	}
	return r.dest
}

func buildLabelIndex(src []uint16) *labelIndex {
	n := len(src)
	depth := make([]int32, n)
	escaped := make([]bool, n)
	var d, low, high int32
	slashes := 0
	for k, c := range src {
		depth[k], escaped[k] = d, slashes%2 == 1
		if c == '\\' {
			slashes++
		} else {
			slashes = 0
		}
		if !escaped[k] && c == '[' {
			d++
		} else if !escaped[k] && c == ']' {
			d--
		}
		low, high = min(low, d), max(high, d)
	}
	// latest[d-low] is the nearest unescaped "]" seen so far that leaves depth d.
	latest := make([]int32, high-low+1)
	for k := range latest {
		latest[k] = -1
	}
	ix := &labelIndex{closeAt: make([]int32, n)}
	for k := n - 1; k >= 0; k-- {
		switch {
		case escaped[k]:
			ix.closeAt[k] = -1
		case src[k] == ']':
			latest[depth[k]-1-low] = int32(k)
		case src[k] == '[':
			ix.closeAt[k] = latest[depth[k]-low]
		}
	}
	return ix
}

func buildDestIndex(src []uint16) *destIndex {
	n := len(src)
	depth := make([]int32, n)
	var d, low, high int32
	for k, c := range src {
		depth[k] = d
		switch c {
		case '(':
			d++
		case ')':
			d--
		}
		low, high = min(low, d), max(high, d)
	}
	latest := make([]int32, high-low+1)
	for k := range latest {
		latest[k] = int32(n)
	}
	ix := &destIndex{paren: make([]int32, n+1), space: make([]int32, n+1), angle: make([]int32, n+1)}
	ix.paren[n], ix.space[n], ix.angle[n] = int32(n), int32(n), int32(n)
	for k := n - 1; k >= 0; k-- {
		c := src[k]
		if c == ')' {
			latest[depth[k]-low] = int32(k)
		}
		ix.paren[k] = latest[depth[k]-low]
		ix.space[k], ix.angle[k] = ix.space[k+1], ix.angle[k+1]
		if jsSpace(c) {
			ix.space[k] = int32(k)
		}
		if c == '>' {
			ix.angle[k] = int32(k)
		}
	}
	return ix
}
