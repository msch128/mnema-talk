package mdplain

// Links, as the web client reads them (inline.ts matchAutolink, matchBare,
// matchMasked; link.ts safeHref).

// maxLabel bounds a link label, like the client.
const maxLabel = 500

func (r *run) autolink(i int) (hit, bool) {
	src := r.src
	if r.ctx.noLinks {
		return hit{}, false
	}
	k := i + 1
	switch {
	case hasAtFold(src, k, "https://"):
		k += len("https://")
	case hasAtFold(src, k, "http://"), hasAtFold(src, k, "mailto:"):
		k += len("http://")
	default:
		return hit{}, false
	}
	e := k
	for e < len(src) && !jsSpace(src[e]) && src[e] != '<' && src[e] != '>' && r.p.tick() {
		e++
	}
	if e == k || e >= len(src) || src[e] != '>' {
		return hit{}, false
	}
	href := src[i+1 : e]
	ok, sure := r.p.safeHref(href, false)
	if !sure {
		// Read as text it keeps its angle brackets: the text may differ, what is hidden only when decides.
		r.p.unsure = true
		if decides(href) {
			r.p.doubt = true
		}
	}
	if !ok {
		return hit{}, false
	}
	return hit{v: href, end: e + 1}, true
}

// urlUnit is BARE_URL's (and SAFE_HREF's) character class.
func urlUnit(c uint16) bool {
	return !jsSpace(c) && c >= 0x20 && c != 0x7f && c != '<' && c != '>' && c != '"' && c != '`' && c != '\\'
}

func trailingUnit(c uint16) bool {
	switch c {
	case '.', ',', ':', ';', '!', '?', '\'', '"', '*', '_', '~', '|':
		return true
	}
	return false
}

func (r *run) bare(i int) (hit, bool) {
	src := r.src
	if r.ctx.noLinks || wordAt(src, i-1) {
		return hit{}, false
	}
	k := i + len("http://")
	switch {
	case hasAtFold(src, i, "https://"):
		k++
	case !hasAtFold(src, i, "http://"):
		return hit{}, false
	}
	end := r.urlEnd(k)
	if end == k {
		return hit{}, false
	}
	end = r.trimURL(i, end)
	ok, sure := r.p.safeHref(src[i:end], true)
	if !sure && decides(src[i+1:end]) {
		r.p.doubt = true
	}
	// Unsure but deciding nothing: as a link or as text, the URL reads the same.
	if !ok && sure {
		return hit{}, false
	}
	return hit{v: src[i:end], end: end}, true
}

// urlRun is a stretch of URL units that bare URLs were read from: a later
// bare URL starting inside it ends where it ends, and its counts answer trimURL.
type urlRun struct {
	from, to int
	// pipes[k-from]: the first "||" at or after k; parens: ( ) [ ] before k.
	pipes  []int32
	counts [4][]int32
}

// urlEnd is the first unit at or after k outside the URL class.
func (r *run) urlEnd(k int) int {
	if u := r.url; u != nil && k >= u.from && k <= u.to {
		if u.pipes == nil {
			u.index(r.src)
			r.p.steps += u.to - u.from
		}
		return u.to
	}
	end := k
	for end < len(r.src) && urlUnit(r.src[end]) && r.p.tick() {
		end++
	}
	r.url = &urlRun{from: k, to: end}
	return end
}

func (u *urlRun) index(src []uint16) {
	n := u.to - u.from
	u.pipes = make([]int32, n+1)
	u.pipes[n] = int32(u.to)
	for k := n - 1; k >= 0; k-- {
		u.pipes[k] = u.pipes[k+1]
		if src[u.from+k] == '|' && unitAt(src, u.from+k+1) == '|' {
			u.pipes[k] = int32(u.from + k)
		}
	}
	for c := range u.counts {
		u.counts[c] = make([]int32, n+1)
	}
	for k := 0; k < n; k++ {
		for c, ch := range "()[]" {
			u.counts[c][k+1] = u.counts[c][k]
			if src[u.from+k] == uint16(ch) {
				u.counts[c][k+1]++
			}
		}
	}
}

// trimURL cuts what belongs to the sentence or the surrounding markup from src[i:end].
func (r *run) trimURL(i, end int) int {
	src := r.src
	end, counts := r.urlCounts(i, end)
	for end > i && r.p.tick() {
		last := src[end-1]
		closing := (last == ')' && counts[1] > counts[0]) || (last == ']' && counts[3] > counts[2])
		if !trailingUnit(last) && !closing {
			break
		}
		if at := parenKind(last); at >= 0 {
			counts[at]--
		}
		end--
	}
	return end
}

// urlCounts cuts src[i:end] at its first "||" and counts its ( ) [ ].
func (r *run) urlCounts(i, end int) (int, [4]int) {
	var counts [4]int
	if u := r.url; u != nil && u.pipes != nil && i >= u.from-len("https://") && end == u.to {
		// The scheme lies before from and holds none of ( ) [ ].
		if p := int(u.pipes[max(i+1, u.from)-u.from]); p < end {
			end = p
		}
		base := max(i, u.from) - u.from
		for c := range counts {
			counts[c] = int(u.counts[c][end-u.from] - u.counts[c][base])
		}
		return end, counts
	}
	end = firstPipes(r.src, i+1, end)
	for _, c := range r.src[i:end] {
		if at := parenKind(c); at >= 0 {
			counts[at]++
		}
	}
	return end, counts
}

// firstPipes is the first "||" in src[from:end] (end when there is none).
func firstPipes(src []uint16, from, end int) int {
	for k := from; k+1 < end; k++ {
		if src[k] == '|' && src[k+1] == '|' {
			return k
		}
	}
	return end
}

func parenKind(c uint16) int {
	switch c {
	case '(':
		return 0
	case ')':
		return 1
	case '[':
		return 2
	case ']':
		return 3
	}
	return -1
}

func (r *run) masked(i int) (hit, bool) {
	if r.ctx.noLinks || r.ctx.depth >= maxDepth {
		return hit{}, false
	}
	closeAt := r.labelEnd(i)
	if closeAt < 0 || closeAt == i+1 {
		return hit{}, false
	}
	url, end, ok := r.destination(closeAt + 1)
	if !ok {
		return hit{}, false
	}
	if valid, sure := r.p.safeHref(url, false); !sure {
		r.p.doubt = true
	} else if !valid {
		return hit{}, false
	}
	label := r.p.inline(r.src[i+1:closeAt], inlineCtx{depth: r.ctx.depth + 1, noLinks: true})
	return hit{v: label, end: end}, true
}

// labelEnd is the index of the "]" closing the "[" at i (within maxLabel), or -1.
// It scans like the client until the scans of this run add up to more than
// its length, then answers from the label index.
func (r *run) labelEnd(i int) int {
	limit := min(len(r.src), i+maxLabel)
	if r.label == nil && r.scanned < 2*len(r.src)+64 {
		return r.scanLabel(i, limit)
	}
	j := int(r.labels().closeAt[i])
	if j >= 0 && j < limit {
		return j
	}
	return -1
}

func (r *run) scanLabel(i, limit int) int {
	depth := 0
	for j := i; j < limit; j++ {
		r.scanned++
		switch r.src[j] {
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

// destinationEnd is where the address from start stops (see destination).
func (r *run) destinationEnd(start, limit int, angled bool) int {
	if r.dest == nil && r.scanned < 2*len(r.src)+64 {
		return r.scanDestination(start, limit, angled)
	}
	ix := r.dests()
	if angled {
		return min(int(ix.angle[start]), limit)
	}
	return min(int(ix.space[start]), int(ix.paren[start]), limit)
}

func (r *run) scanDestination(start, limit int, angled bool) int {
	depth := 0
	for j := start; j < limit; j++ {
		r.scanned++
		c := r.src[j]
		if angled {
			if c == '>' {
				return j
			}
			continue
		}
		switch {
		case jsSpace(c), c == ')' && depth == 0:
			return j
		case c == '(':
			depth++
		case c == ')':
			depth--
		}
	}
	return limit
}

// destination reads "(url)", "(<url>)" or `(url "title")` at i; the title is dropped.
func (r *run) destination(i int) ([]uint16, int, bool) {
	src := r.src
	if unitAt(src, i) != '(' {
		return nil, 0, false
	}
	angled := unitAt(src, i+1) == '<'
	start := i + 1
	if angled {
		start++
	}
	limit := min(len(src), start+maxLabel*4)
	j := start
	if start < limit {
		j = r.destinationEnd(start, limit, angled)
	}
	url := src[min(start, len(src)):min(j, len(src))]
	if angled {
		j++
	}
	end, ok := r.destinationTail(j)
	return url, end, ok
}

// destinationTail is /^\s*(?:"[^"\n]*"|'[^'\n]*')?\s*\)/ on the next 400 units.
func (r *run) destinationTail(j int) (int, bool) {
	src := r.src
	w := min(len(src), j+400)
	k := skipSpace(src, j, w)
	if k < w && (src[k] == '"' || src[k] == '\'') {
		m := k + 1
		for m < w && src[m] != src[k] && src[m] != '\n' {
			m++
		}
		if m >= w || src[m] != src[k] {
			return 0, false
		}
		k = skipSpace(src, m+1, w)
	}
	if k < w && src[k] == ')' {
		return k + 1, true
	}
	return 0, false
}

func skipSpace(src []uint16, k, w int) int {
	for k < w && jsSpace(src[k]) {
		k++
	}
	return k
}
