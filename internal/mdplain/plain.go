// Package mdplain reduces a message's Markdown to the text a notification may
// show. It is a port of the web client's parser (web/src/shared/lib/markdown:
// blocks.ts, inline.ts, link.ts, plain.ts), so a push, a snippet and the
// message view hide the same text: formatting marks go, link and code text
// stay, ||spoilers|| become a word, custom emoji tokens become :name:. The
// shared fixtures in testdata are checked by both sides, and a corpus of
// generated messages judged by the web renderer (testdata/cross.jsonl) is
// checked here.
//
// Where this code cannot be sure to read a message like the client (a link
// whose host only the browser's URL parser can judge, or a message that
// exhausts the work bound), it fails closed: everything between the first and
// the last "||" of the message counts as hidden. Every spoiler the client can
// draw lies there, so nothing it hides is shown.
package mdplain

import (
	"strings"
	"unicode/utf16"

	"github.com/msch128/mnema-talk/internal/emojitext"
)

// maxMarkdownUnits mirrors MAX_MARKDOWN_CHARS: longer text is not parsed.
const maxMarkdownUnits = 6000

// parser holds what one message's reading shares.
type parser struct {
	spoiler []uint16
	// doubt: the client may hide something else (see safeHref, tick); unsure:
	// it may show some text differently, never what is hidden.
	doubt, unsure bool
	// steps counts work; past budget every scan gives up and doubt is set.
	steps, budget int
}

// workPerUnit and workBase bound the steps of one message: linear in its length.
const (
	workPerUnit = 64
	workBase    = 4096
)

func newParser(spoiler string, units int) *parser {
	return &parser{spoiler: encode(spoiler), budget: workPerUnit*units + workBase}
}

// tick counts one step of work; false once the budget is spent.
func (p *parser) tick() bool {
	p.steps++
	if p.steps > p.budget {
		p.doubt = true
		return false
	}
	return true
}

// Plain returns content without Markdown formatting. spoiler is the word shown
// instead of hidden text (in the member's language); line breaks stay, the
// caller collapses them.
func Plain(content, spoiler string) string {
	s := strings.ReplaceAll(content, "\x00", "")
	out, doubt := read(s, spoiler)
	if !doubt || !strings.Contains(s, "||") {
		return out
	}
	first, last := strings.Index(s, "||"), strings.LastIndex(s, "||")
	before, _ := read(s[:first], spoiler)
	after, _ := read(s[last+2:], spoiler)
	return before + " " + spoiler + " " + after
}

// read is the client's markdownToPlain, and whether it may hide something else.
func read(s, spoiler string) (string, bool) {
	p, out := parse(s, spoiler)
	return out, p.doubt
}

func parse(s, spoiler string) (*parser, string) {
	units := utf16Len(s)
	p := newParser(spoiler, units)
	if units > maxMarkdownUnits {
		return p, emojitext.Plain(s)
	}
	return p, strings.Join(p.blocks(s, false), "\n")
}

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}
