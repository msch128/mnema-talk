package mdplain

import "strings"

// maxDepth is how deep formatting nests; deeper delimiters stay literal. It
// mirrors the web client's parser (web/src/lib/markdown/inline.ts).
const maxDepth = 6

// maxLabel bounds a link label, like the client.
const maxLabel = 500

func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f' || b == '\v'
}

func isWord(b byte) bool {
	return b == '_' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// at is src[i], or a space when i is outside src (the client treats the edges as white space).
func at(src string, i int) byte {
	if i < 0 || i >= len(src) {
		return ' '
	}
	return src[i]
}

// escapable reports whether a backslash before b drops itself: any symbol that is not a letter, digit or space.
func escapable(b byte) bool { return !isLetter(b) && !(b >= '0' && b <= '9') && !isSpace(b) }

func isLetter(b byte) bool { return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') }

// scanner remembers, per delimiter and run of text, from where on no closer
// exists, so a run of unclosed delimiters costs linear time.
type scanner struct {
	spoiler string
	miss    map[string]int
}

// find returns the first index >= from where test passes, skipping escaped
// characters and the given pair (e.g. "**" while looking for "*").
func (s *scanner) find(key, src string, from int, test func(j int) bool, pair string) int {
	if m, ok := s.miss[key]; ok && from >= m {
		return -1
	}
	for j := from; j < len(src); j++ {
		switch {
		case src[j] == '\\':
			j++
		case pair != "" && strings.HasPrefix(src[j:], pair):
			j += len(pair) - 1
		case test(j):
			return j
		}
	}
	if m, ok := s.miss[key]; !ok || from < m {
		s.miss[key] = from
	}
	return -1
}

// inline turns a run of inline Markdown into its text.
func (s *scanner) inline(src string, depth int, noLinks bool) string {
	// The memo is only valid for this run of text.
	outer := s.miss
	s.miss = map[string]int{}
	defer func() { s.miss = outer }()
	var b strings.Builder
	for i := 0; i < len(src); {
		if src[i] == '\\' && i+1 < len(src) && escapable(src[i+1]) {
			b.WriteByte(src[i+1])
			i += 2
			continue
		}
		text, end, ok := s.match(src, i, depth, noLinks)
		if !ok {
			b.WriteByte(src[i])
			i++
			continue
		}
		b.WriteString(text)
		i = end
	}
	return b.String()
}

func (s *scanner) match(src string, i, depth int, noLinks bool) (string, int, bool) {
	switch src[i] {
	case '`':
		text, end := s.code(src, i)
		return text, end, true
	case '<':
		if text, end, ok := token(src, i); ok {
			return text, end, true
		}
		return autolink(src, i, noLinks)
	case '[':
		return s.masked(src, i, depth, noLinks)
	case 'h', 'H':
		return bareURL(src, i, noLinks)
	case '*', '_', '~', '|':
		return s.format(src, i, depth, noLinks)
	}
	return "", i, false
}

// code reads `code` and “code with ` inside“: a run of backticks closes
// with a run of the same length. An unclosed run stays as typed.
func (s *scanner) code(src string, i int) (string, int) {
	n := 1
	for i+n < len(src) && src[i+n] == '`' {
		n++
	}
	fence := strings.Repeat("`", n)
	for from := i + n; from < len(src); {
		rel := strings.Index(src[from:], fence)
		if rel < 0 {
			break
		}
		closeAt := from + rel
		runEnd := closeAt + n
		for runEnd < len(src) && src[runEnd] == '`' {
			runEnd++
		}
		if body := src[i+n : closeAt]; runEnd-closeAt == n && strings.TrimSpace(body) != "" {
			return codeBody(body), closeAt + n
		}
		from = runEnd
	}
	return fence, i + n
}

// codeBody drops one space of padding on both sides, so “ ` “ can show a backtick.
func codeBody(body string) string {
	if len(body) > 2 && body[0] == ' ' && body[len(body)-1] == ' ' && strings.TrimSpace(body) != "" {
		return body[1 : len(body)-1]
	}
	return body
}

// nested formats the delimited run src[from:to] one level deeper.
func (s *scanner) nested(src string, from, to, depth int, noLinks bool) string {
	return s.inline(src[from:to], depth+1, noLinks)
}

func (s *scanner) format(src string, i, depth int, noLinks bool) (string, int, bool) {
	if depth >= maxDepth {
		return "", i, false
	}
	var open, closeAt, width int
	switch {
	case src[i] == '*' && at(src, i+1) == '*':
		open, closeAt, width = i+2, s.find("**", src, i+3, func(k int) bool { return strings.HasPrefix(src[k:], "**") && at(src, k+2) != '*' }, ""), 2
	case src[i] == '_' && at(src, i+1) == '_':
		open, closeAt, width = i+2, s.find("__", src, i+3, func(k int) bool { return strings.HasPrefix(src[k:], "__") && at(src, k+2) != '_' }, ""), 2
	case src[i] == '~' && at(src, i+1) == '~':
		open, closeAt, width = i+2, s.find("~~", src, i+3, func(k int) bool { return strings.HasPrefix(src[k:], "~~") }, ""), 2
		if closeAt >= 0 && (isSpace(at(src, i+2)) || isSpace(at(src, closeAt-1))) {
			closeAt = -1
		}
	case src[i] == '|' && at(src, i+1) == '|':
		return s.spoilerRun(src, i)
	default:
		return s.emphasis(src, i, depth, noLinks)
	}
	if closeAt < 0 {
		return "", i, false
	}
	return s.nested(src, open, closeAt, depth, noLinks), closeAt + width, true
}

// emphasis handles *em* and _em_ (an underscore inside a word stays text).
func (s *scanner) emphasis(src string, i, depth int, noLinks bool) (string, int, bool) {
	var closeAt int
	switch src[i] {
	case '*':
		if isSpace(at(src, i+1)) || at(src, i+1) == '*' {
			return "", i, false
		}
		closeAt = s.find("*", src, i+2, func(k int) bool { return src[k] == '*' && at(src, k+1) != '*' && !isSpace(at(src, k-1)) }, "**")
	case '_':
		if (i > 0 && isWord(src[i-1])) || at(src, i+1) == '_' {
			return "", i, false
		}
		closeAt = s.find("_", src, i+2, func(k int) bool { return src[k] == '_' && !(k+1 < len(src) && isWord(src[k+1])) }, "__")
	default:
		return "", i, false
	}
	if closeAt < 0 {
		return "", i, false
	}
	return s.nested(src, i+1, closeAt, depth, noLinks), closeAt + 1, true
}

// spoilerRun replaces ||hidden|| by the word for "spoiler": the text must not
// reach a lock screen.
func (s *scanner) spoilerRun(src string, i int) (string, int, bool) {
	closeAt := s.find("||", src, i+3, func(k int) bool { return strings.HasPrefix(src[k:], "||") }, "")
	if closeAt < 0 {
		return "", i, false
	}
	return s.spoiler, closeAt + 2, true
}
