package mdplain

import (
	"regexp"
	"strings"
)

// Blocks, line for line the web client's parseBlocks (blocks.ts), each block
// turned into its plain text right away.

// jsWS is JavaScript's \s and jsDot its "." (anything but a line terminator).
const (
	jsWS  = `\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}`
	jsDot = `[^\n\r\x{2028}\x{2029}]`
)

var (
	heading   = regexp.MustCompile(`^#{1,3} +([^` + jsWS + `]` + jsDot + `*)$`)
	subtext   = regexp.MustCompile(`^-# +([^` + jsWS + `]` + jsDot + `*)$`)
	item      = regexp.MustCompile(`^[ \t]*(?:[-*]|[0-9]{1,9}\.)[ \t]+([^` + jsWS + `]` + jsDot + `*)$`)
	fenceHead = regexp.MustCompile(`(?s)^([A-Za-z0-9_+#.-]{1,20})\n(.*)$`)
)

type segment struct {
	code bool
	// start is where the segment begins in the source (segments cover it
	// without gaps); text drops the line break that touches a code block.
	start int
	text  string
}

// blocks is parseBlocks followed by blocksPlain: the plain text of each block.
func (p *parser) blocks(src string, quoted bool) []string {
	segs := splitFences(src)
	var out []string
	for i, seg := range segs {
		if seg.code {
			out = append(out, seg.text)
			continue
		}
		// The client joins the source of the later segments: the rest of src.
		tail := ""
		if i+1 < len(segs) {
			tail = src[segs[i+1].start:]
		}
		parts, rest := p.lines(strings.Split(seg.text, "\n"), tail, quoted)
		out = append(out, parts...)
		if rest {
			break
		}
	}
	return out
}

// lines is parseLines; rest reports that a ">>> " quote took the rest of the message.
func (p *parser) lines(lines []string, tail string, quoted bool) ([]string, bool) {
	var out, para []string
	flush := func() {
		if text := strings.Join(para, "\n"); text != "" {
			out = append(out, p.inlineString(text))
		}
		para = nil
	}
	for i := 0; i < len(lines); {
		if !quoted && strings.HasPrefix(lines[i], ">>> ") {
			flush()
			body := strings.Join(append([]string{lines[i][4:]}, lines[i+1:]...), "\n") + tail
			return append(out, strings.Join(p.blocks(body, true), "\n")), true
		}
		parts, n := p.block(lines, i, quoted)
		if n == 0 {
			para = append(para, lines[i])
			i++
			continue
		}
		flush()
		out = append(out, parts...)
		i += n
	}
	flush()
	return out, false
}

// block reads the quote, heading or list starting at lines[i]: its plain
// parts and how many lines it takes (none: an ordinary line).
func (p *parser) block(lines []string, i int, quoted bool) ([]string, int) {
	if n := countLines(lines, i, quoteLine, quoted); n > 0 {
		taken := make([]string, n)
		for k := range taken {
			taken[k] = lines[i+k][2:]
		}
		return []string{strings.Join(p.blocks(strings.Join(taken, "\n"), true), "\n")}, n
	}
	if text, ok := headingText(lines[i]); ok {
		return []string{p.inlineString(text)}, 1
	}
	n := countLines(lines, i, itemLine, false)
	parts := make([]string, n)
	for k := range parts {
		parts[k] = p.inlineString(itemText(lines[i+k]))
	}
	return parts, n
}

func quoteLine(line string) bool { return strings.HasPrefix(line, "> ") }

func itemLine(line string) bool { return item.MatchString(line) }

func itemText(line string) string { return jsTrimEnd(item.FindStringSubmatch(line)[1]) }

// countLines is how many lines from i on pass take (none when off).
func countLines(lines []string, i int, take func(string) bool, off bool) int {
	n := 0
	for !off && i+n < len(lines) && take(lines[i+n]) {
		n++
	}
	return n
}

func headingText(line string) (string, bool) {
	for _, re := range []*regexp.Regexp{heading, subtext} {
		if m := re.FindStringSubmatch(line); m != nil {
			return jsTrimEnd(m[1]), true
		}
	}
	return "", false
}

// splitFences separates ``` code blocks from the text around them.
func splitFences(src string) []segment {
	var out []segment
	pos, from := 0, 0
	for {
		open := findFence(src, from)
		if open < 0 {
			break
		}
		rel := strings.Index(src[open+3:], "```")
		if rel < 0 {
			break
		}
		closeAt := open + 3 + rel
		code, ok := fenceBody(src[open+3 : closeAt])
		from = closeAt + 3
		if !ok {
			continue
		}
		if open > pos {
			out = append(out, segment{start: pos, text: src[pos:open]})
		}
		out = append(out, segment{code: true, start: open, text: code})
		pos = from
	}
	if pos < len(src) {
		out = append(out, segment{start: pos, text: src[pos:]})
	}
	return dropBoundaryBreaks(out)
}

// dropBoundaryBreaks: the line break right before or after a code block belongs to the block.
func dropBoundaryBreaks(segs []segment) []segment {
	for i := range segs {
		if segs[i].code {
			continue
		}
		if i > 0 && segs[i-1].code {
			segs[i].text = strings.TrimPrefix(segs[i].text, "\n")
		}
		if i+1 < len(segs) && segs[i+1].code {
			segs[i].text = strings.TrimSuffix(segs[i].text, "\n")
		}
	}
	return segs
}

// findFence is the index of the next ``` at or after from that is not backslash-escaped.
func findFence(src string, from int) int {
	for from <= len(src) {
		rel := strings.Index(src[from:], "```")
		if rel < 0 {
			return -1
		}
		at := from + rel
		slashes := 0
		for at-1-slashes >= 0 && src[at-1-slashes] == '\\' {
			slashes++
		}
		if slashes%2 == 0 {
			return at
		}
		from = at + 1
	}
	return -1
}

// fenceBody is "lang\ncode" or just "code"; not ok when there is nothing to show.
func fenceBody(body string) (string, bool) {
	if m := fenceHead.FindStringSubmatch(body); m != nil && jsTrim(m[2]) != "" {
		body = m[2]
	}
	body = strings.Trim(body, "\n")
	return body, jsTrim(body) != ""
}
