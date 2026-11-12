package mdplain

import (
	"strconv"

	"github.com/msch128/mnema-talk/internal/emojitext"
)

// The inline scanner, line for line the web client's parseInline
// (web/src/shared/lib/markdown/inline.ts), producing the plain text directly.

// maxDepth is how deep formatting nests; deeper delimiters stay literal.
const maxDepth = 6

type inlineCtx struct {
	depth int
	// noLinks: inside a link label, where no further link starts.
	noLinks bool
}

// hit is what a construct at a position reads as: text that joins the
// surrounding text, or the plain form of a node.
type hit struct {
	text bool
	v    []uint16
	end  int
}

// run is one call of parseInline: its source, context and memo.
type run struct {
	p   *parser
	src []uint16
	ctx inlineCtx
	// miss remembers, per delimiter, from where on no closer exists.
	miss  map[string]int
	label *labelIndex
	dest  *destIndex
	// scanned counts the units label and destination scans read without an index.
	scanned int
	url     *urlRun
}

// textPlain is the plain form of a text node: custom emoji tokens read :name:.
func textPlain(buf []uint16) []uint16 {
	for _, c := range buf {
		if c == '<' {
			return encode(emojitext.Plain(decode(buf)))
		}
	}
	return buf
}

// inline is parseInline followed by inlinePlain.
func (p *parser) inline(src []uint16, ctx inlineCtx) []uint16 {
	r := &run{p: p, src: src, ctx: ctx, miss: map[string]int{}}
	var out, buf []uint16
	for i := 0; i < len(src); {
		p.tick()
		if src[i] == '\\' && escapableAt(src, i+1) {
			buf = append(buf, src[i+1])
			i += 2
			continue
		}
		h, ok := r.matchAt(i)
		switch {
		case !ok:
			buf = append(buf, src[i])
			i++
			continue
		case h.text:
			buf = append(buf, h.v...)
		default:
			out = append(out, textPlain(buf)...)
			buf = nil
			out = append(out, h.v...)
		}
		i = h.end
	}
	return append(out, textPlain(buf)...)
}

// inlineString is inline for a block's text.
func (p *parser) inlineString(text string) string {
	return decode(p.inline(encode(text), inlineCtx{}))
}

func (r *run) matchAt(i int) (hit, bool) {
	switch r.src[i] {
	case '`':
		return r.code(i), true
	case '<':
		return r.autolink(i)
	case '[':
		return r.masked(i)
	case 'h', 'H':
		return r.bare(i)
	case '*', '_', '~', '|':
		return r.format(i)
	}
	return hit{}, false
}

// scan is the first index >= from where test passes, skipping escapes, code
// spans and pair; a failed search is remembered per key.
func (r *run) scan(key string, from int, test func(int) bool, pair string) int {
	if missed, ok := r.miss[key]; ok && from >= missed {
		return -1
	}
	found := r.scanFrom(from, test, pair)
	if found < 0 {
		r.miss[key] = from
	}
	return found
}

func (r *run) scanFrom(from int, test func(int) bool, pair string) int {
	src := r.src
	var unclosed map[int]bool
	for j := from; j < len(src); j++ {
		if !r.p.tick() {
			return -1
		}
		switch {
		case src[j] == '\\':
			j++
		case src[j] == '`':
			// A code span is opaque: delimiters inside it neither close nor open anything.
			if unclosed == nil {
				unclosed = map[int]bool{}
			}
			j = r.skipCodeSpan(j, unclosed)
		case pair != "" && hasAt(src, j, pair):
			j += len(pair) - 1
		case test(j):
			return j
		}
	}
	return -1
}

// skipCodeSpan is the index of the last unit of the code span (or lone backtick run) at j.
func (r *run) skipCodeSpan(j int, unclosed map[int]bool) int {
	n := backticks(r.src, j)
	closeAt := -1
	if !unclosed[n] {
		closeAt = r.codeClose(j, n)
	}
	if closeAt < 0 {
		unclosed[n] = true
		return j + n - 1
	}
	return closeAt + n - 1
}

func backticks(src []uint16, i int) int {
	n := 1
	for unitAt(src, i+n) == '`' {
		n++
	}
	return n
}

// codeClose is the index of the run of n backticks closing the code span opened at i, or -1.
func (r *run) codeClose(i, n int) int {
	src := r.src
	// The body must hold more than white space: its first other unit.
	text := i + n
	for text < len(src) && jsSpace(src[text]) {
		text++
	}
	for from := i + n; from < len(src); {
		c := r.fenceAt(from, n)
		if c < 0 {
			return -1
		}
		runEnd := c + backticks(src, c)
		if runEnd-c == n && text < c {
			return c
		}
		from = runEnd
	}
	return -1
}

// fenceAt is src.indexOf("`" * n, from).
func (r *run) fenceAt(from, n int) int {
	start, count := -1, 0
	for k := from; k < len(r.src); k++ {
		if !r.p.tick() {
			return -1
		}
		if r.src[k] != '`' {
			count = 0
			continue
		}
		if count == 0 {
			start = k
		}
		if count++; count == n {
			return start
		}
	}
	return -1
}

func (r *run) code(i int) hit {
	src := r.src
	n := backticks(src, i)
	key := "`" + strconv.Itoa(n)
	lone := hit{text: true, v: src[i : i+n], end: i + n}
	if m, ok := r.miss[key]; ok && i+n >= m {
		return lone
	}
	if c := r.codeClose(i, n); c >= 0 {
		return hit{v: codeBody(src[i+n : c]), end: c + n}
	}
	if m, ok := r.miss[key]; !ok || i+n < m {
		r.miss[key] = i + n
	}
	return lone
}

// codeBody drops one space of padding on both sides, so “ ` “ can show a backtick.
func codeBody(body []uint16) []uint16 {
	if len(body) > 2 && body[0] == ' ' && body[len(body)-1] == ' ' && !onlySpace(body) {
		return body[1 : len(body)-1]
	}
	return body
}

func (r *run) format(i int) (hit, bool) {
	// Too deep to nest further: the delimiters stay literal.
	if r.ctx.depth >= maxDepth {
		return hit{}, false
	}
	next := unitAt(r.src, i+1)
	switch r.src[i] {
	case '*':
		if next == '*' {
			return r.delimited(i, "**", '*')
		}
		return r.emStar(i)
	case '_':
		if next == '_' {
			return r.delimited(i, "__", '_')
		}
		return r.emUnderscore(i)
	case '~':
		if next == '~' {
			return r.strike(i)
		}
	default:
		if next == '|' {
			return r.spoiler(i)
		}
	}
	return hit{}, false
}

// delimited reads **strong** and __underline__.
func (r *run) delimited(i int, delim string, ch uint16) (hit, bool) {
	src := r.src
	j := r.scan(delim, i+3, func(k int) bool { return hasAt(src, k, delim) && unitAt(src, k+2) != ch }, "")
	return r.nested(i+2, j, j+2)
}

func (r *run) emStar(i int) (hit, bool) {
	src := r.src
	if spaceAt(src, i+1) || unitAt(src, i+1) == '*' {
		return hit{}, false
	}
	j := r.scan("*", i+2, func(k int) bool { return src[k] == '*' && unitAt(src, k+1) != '*' && !spaceAt(src, k-1) }, "**")
	return r.nested(i+1, j, j+1)
}

func (r *run) emUnderscore(i int) (hit, bool) {
	src := r.src
	if wordAt(src, i-1) || unitAt(src, i+1) == '_' {
		return hit{}, false
	}
	j := r.scan("_", i+2, func(k int) bool { return src[k] == '_' && !wordAt(src, k+1) }, "__")
	return r.nested(i+1, j, j+1)
}

func (r *run) strike(i int) (hit, bool) {
	src := r.src
	if spaceAt(src, i+2) {
		return hit{}, false
	}
	j := r.scan("~~", i+3, func(k int) bool { return hasAt(src, k, "~~") }, "")
	if j >= 0 && spaceAt(src, j-1) {
		return hit{}, false
	}
	return r.nested(i+2, j, j+2)
}

// spoiler replaces ||hidden|| by the spoiler word; what it hides is not read.
func (r *run) spoiler(i int) (hit, bool) {
	src := r.src
	j := r.scan("||", i+3, func(k int) bool { return hasAt(src, k, "||") }, "")
	if j < 0 {
		return hit{}, false
	}
	return hit{v: r.p.spoiler, end: j + 2}, true
}

// nested is the formatted run src[from:to] read one level deeper (to < 0: no closer).
func (r *run) nested(from, to, end int) (hit, bool) {
	if to < 0 {
		return hit{}, false
	}
	return hit{v: r.p.inline(r.src[from:to], inlineCtx{depth: r.ctx.depth + 1, noLinks: r.ctx.noLinks}), end: end}, true
}
