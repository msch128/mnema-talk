// Package mdplain reduces a message's Markdown to the text a notification may
// show. It reads the same flavour as the web client (web/src/lib/markdown), so
// a push, a reply preview and the message view agree: formatting marks go,
// link and code text stay, ||spoilers|| become a word, custom emoji tokens
// become :name:. The shared fixtures in testdata are checked by both sides.
package mdplain

import (
	"regexp"
	"strings"

	"github.com/msch128/mnema-talk/internal/emojitext"
)

var (
	heading = regexp.MustCompile(`^#{1,3} +(\S.*)$`)
	subtext = regexp.MustCompile(`^-# +(\S.*)$`)
	item    = regexp.MustCompile(`^[ \t]*(?:[-*]|\d{1,9}\.)[ \t]+(\S.*)$`)
	// fenceHead is an optional language tag on the first line of a code block.
	fenceHead = regexp.MustCompile(`^([A-Za-z0-9_+#.-]{1,20})\n([\s\S]*)$`)
)

// Plain returns content without Markdown formatting. spoiler is the word shown
// instead of hidden text (in the member's language); line breaks stay, the
// caller collapses them.
func Plain(content, spoiler string) string {
	s := &scanner{spoiler: spoiler, miss: map[string]int{}}
	var out []string
	for _, seg := range splitFences(content) {
		if seg.code {
			out = append(out, seg.text)
		} else {
			out = append(out, s.blocks(seg.text))
		}
	}
	return strings.Join(out, "\n")
}

type segment struct {
	text string
	code bool
}

// splitFences separates ``` code blocks from the text around them.
func splitFences(src string) []segment {
	var out []segment
	pos, from := 0, 0
	for {
		open := findFence(src, from)
		closeAt := -1
		if open >= 0 {
			if rel := strings.Index(src[open+3:], "```"); rel >= 0 {
				closeAt = open + 3 + rel
			}
		}
		if closeAt < 0 {
			break
		}
		code, ok := fenceBody(src[open+3 : closeAt])
		from = closeAt + 3
		if !ok {
			continue
		}
		if open > pos {
			out = append(out, segment{text: src[pos:open]})
		}
		out = append(out, segment{text: code, code: true})
		pos = from
	}
	if pos < len(src) {
		out = append(out, segment{text: src[pos:]})
	}
	return out
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
	if m := fenceHead.FindStringSubmatch(body); m != nil && strings.TrimSpace(m[2]) != "" {
		body = m[2]
	}
	body = strings.Trim(body, "\n")
	return body, strings.TrimSpace(body) != ""
}

// blocks reads the lines of a text run: quote, heading and list marks go, a
// run of ordinary lines is one paragraph so a spoiler may span its lines.
func (s *scanner) blocks(text string) string {
	var out []string
	var para []string
	flush := func() {
		if len(para) > 0 {
			out = append(out, s.inline(strings.Join(para, "\n"), 0, false))
			para = nil
		}
	}
	for _, line := range strings.Split(text, "\n") {
		line = stripQuote(line)
		if rest, ok := blockText(line); ok {
			flush()
			out = append(out, s.inline(rest, 0, false))
			continue
		}
		para = append(para, line)
	}
	flush()
	return strings.Join(out, "\n")
}

// stripQuote drops a leading "> " or ">>> ".
func stripQuote(line string) string {
	switch {
	case strings.HasPrefix(line, ">>> "):
		return line[4:]
	case strings.HasPrefix(line, "> "):
		return line[2:]
	}
	return line
}

// blockText returns the text of a heading, subtext or list item line.
func blockText(line string) (string, bool) {
	for _, re := range []*regexp.Regexp{heading, subtext, item} {
		if m := re.FindStringSubmatch(line); m != nil {
			return m[1], true
		}
	}
	return "", false
}

// token reads a custom emoji token at i as :name:.
func token(src string, i int) (string, int, bool) {
	cand := src[i:min(len(src), i+emojitext.MaxTokenLen)]
	end := strings.IndexByte(cand, '>')
	if end < 0 || !emojitext.IsToken(cand[:end+1]) {
		return "", i, false
	}
	return emojitext.Plain(cand[:end+1]), i + end + 1, true
}
