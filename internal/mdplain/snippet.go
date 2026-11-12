package mdplain

import (
	"strings"
	"unicode/utf8"

	"github.com/msch128/mnema-talk/internal/textcut"
)

// SnippetLen is how many characters of a message a snippet shows.
const SnippetLen = 200

// MaxSnippetUnits is the longest snippet in UTF-16 units (JavaScript's
// length): escaping writes a spoiler as five units and a symbol as two, so a
// cut text of SnippetLen-1 spoilers and the ellipsis is the worst case.
const MaxSnippetUnits = 5*(SnippetLen-1) + 1

const (
	// hiddenMark stands for a spoiler while the plain text is cut; a private
	// use character, removed from the message first.
	hiddenMark = "\uE000"
	// spoilerMarkdown is how a spoiler is written back into a snippet: every
	// reader shows it as its own word for "spoiler".
	spoilerMarkdown = "||…||"
	// escapedAnywhere are the characters that start inline Markdown.
	escapedAnywhere = "\\`*_~|<["
	// escapedFirst start a block (heading, quote, list item) at the start of a line.
	escapedFirst = "#>-"
)

// Snippet is the start of a message for places that show only a piece of it
// (reply quotes, search thread roots, reminders, the conversation list): at
// most limit characters of text, as Markdown. A message that fits is returned
// as it is. A longer one is reduced to its plain text first and cut after
// that: cutting the Markdown first left a spoiler (or code, or a link) open
// at the cut when it closed later, and every reader then showed the hidden
// text. The cut plain text is escaped back into Markdown, each spoiler
// written as ||…||, so Plain or the web client's previewText of the snippet
// is exactly that text, with the reader's word for a spoiler; an older client
// that strips the snippet itself therefore shows nothing the whole message
// hides. The web client has the same rule (web/src/shared/lib/markdown/snippet.ts);
// testdata/snippet.json is checked by both.
func Snippet(content string, limit int) string {
	if utf8.RuneCountInString(content) <= limit {
		return content
	}
	plain := strings.Join(strings.FieldsFunc(Plain(strings.ReplaceAll(content, hiddenMark, ""), hiddenMark), jsSpaceRune), " ")
	if utf8.RuneCountInString(plain) > limit {
		plain = jsTrim(textcut.Runes(plain, limit-1)) + "…"
	}
	return escapePlain(plain)
}

// escapePlain writes plain text as Markdown that reads back as the same text.
func escapePlain(plain string) string {
	var b strings.Builder
	digits := true
	for i, r := range plain {
		switch {
		case string(r) == hiddenMark:
			b.WriteString(spoilerMarkdown)
		case strings.ContainsRune(escapedAnywhere, r),
			i == 0 && strings.ContainsRune(escapedFirst, r),
			// "1. " would start a numbered list.
			i > 0 && digits && r == '.',
			// "http://" would start a link.
			r == ':' && urlSchemeEnds(plain, i):
			b.WriteByte('\\')
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
		digits = digits && r >= '0' && r <= '9'
	}
	return b.String()
}

// urlSchemeEnds reports whether the ":" at i ends "http://" or "https://".
func urlSchemeEnds(plain string, i int) bool {
	before := strings.ToLower(plain[max(0, i-5):i])
	return strings.HasPrefix(plain[i:], "://") && (strings.HasSuffix(before, "http") || strings.HasSuffix(before, "https"))
}
