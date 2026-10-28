package httpx

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

const (
	zwnj = 0x200c
	zwj  = 0x200d
	vs16 = 0xfe0f
)

// stripInvisible removes invisible characters (isInvisibleRune) from a name.
// The two joiners are the exception, and only where they do real work:
//   - ZWJ stays between two emoji, or after VS16 or an emoji modifier and
//     before an emoji (family, flag and profession sequences);
//   - ZWNJ stays between two letters of a script that uses it (Arabic, which
//     covers Persian, and the Indic scripts).
//
// Next to Latin, digits or anything else a joiner changes nothing visible, so
// "Ad<ZWJ>min" collapses to "Admin" instead of passing as a different name.
func stripInvisible(s string) string {
	in := []rune(s)
	out := make([]rune, 0, len(in))
	for i, r := range in {
		switch {
		case r == zwj || r == zwnj:
			if i > 0 && i+1 < len(in) && joinerAllowed(r, in[i-1], in[i+1]) {
				out = append(out, r)
			}
		case !isInvisibleRune(r):
			out = append(out, r)
		}
	}
	return string(out)
}

func joinerAllowed(joiner, prev, next rune) bool {
	if joiner == zwj {
		return isEmoji(next) && (isEmoji(prev) || prev == vs16 || isEmojiModifier(prev))
	}
	return joinsAcrossZWNJ(prev) && joinsAcrossZWNJ(next)
}

func isEmojiModifier(r rune) bool { return r >= 0x1f3fb && r <= 0x1f3ff }

// isEmoji approximates Extended_Pictographic, which the standard library does
// not carry: the pictograph blocks plus the few older symbols with an emoji
// presentation.
func isEmoji(r rune) bool {
	switch {
	case r >= 0x1f000 && r <= 0x1faff && !isEmojiModifier(r):
		return true
	case r >= 0x2600 && r <= 0x27bf, r >= 0x2300 && r <= 0x23ff, r >= 0x2b00 && r <= 0x2bff:
		return true
	}
	switch r {
	case 0xa9, 0xae, 0x203c, 0x2049, 0x2122, 0x2139, 0x3030, 0x303d, 0x3297, 0x3299:
		return true
	}
	return false
}

var zwnjScripts = []*unicode.RangeTable{
	unicode.Arabic, unicode.Devanagari, unicode.Bengali, unicode.Gurmukhi, unicode.Gujarati,
	unicode.Oriya, unicode.Tamil, unicode.Telugu, unicode.Kannada, unicode.Malayalam, unicode.Sinhala,
}

func joinsAcrossZWNJ(r rune) bool { return unicode.In(r, zwnjScripts...) }

// confusables maps lower-case letters that read like a Latin letter or digit
// (Cyrillic, Greek and a few Latin variants) to that letter. It is a small
// curated subset of Unicode's confusables, aimed at the names the reserved
// word checks guard (everyone, here, admin, ...), not a full UTS #39 table.
var confusables = map[rune]string{
	// Cyrillic
	'а': "a", 'в': "b", 'е': "e", 'ё': "e", 'з': "3", 'к': "k", 'м': "m", 'н': "h", 'о': "o", 'р': "p",
	'с': "c", 'т': "t", 'у': "y", 'х': "x", 'ь': "b", 'і': "i", 'ї': "i", 'ј': "j", 'ѕ': "s", 'һ': "h",
	'ԁ': "d", 'ԛ': "q", 'ԝ': "w", 'ӏ': "l",
	// Greek
	'α': "a", 'β': "b", 'ε': "e", 'η': "n", 'ι': "i", 'κ': "k", 'ν': "v", 'ο': "o", 'ρ': "p", 'τ': "t",
	'υ': "u", 'χ': "x", 'ω': "w",
	// Latin variants
	'ı': "i", 'ł': "l", 'ø': "o", 'đ': "d", 'ß': "ss", 'ǀ': "l",
}

// NameSkeleton reduces a name to what it looks like to a reader: compatibility
// forms folded (NFKC), every invisible character and joiner gone, lower case,
// and look-alike Cyrillic, Greek and Latin variants mapped to their Latin
// letter (confusables). Reserved-name and duplicate checks compare skeletons,
// so neither an invisible character nor a homoglyph makes a reserved name look
// different.
func NameSkeleton(s string) string {
	s = norm.NFKC.String(s)
	var b strings.Builder
	for _, r := range s {
		if isInvisibleRune(r) {
			continue
		}
		r = unicode.ToLower(r)
		if m, ok := confusables[r]; ok {
			b.WriteString(m)
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}
