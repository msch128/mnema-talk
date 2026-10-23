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

// NameSkeleton reduces a name to what it looks like to a reader: compatibility
// forms folded (NFKC), every invisible character and joiner gone, lower case.
// Reserved-name and duplicate checks compare skeletons, so no invisible
// character makes a reserved name look different.
func NameSkeleton(s string) string {
	s = norm.NFKC.String(s)
	s = strings.Map(func(r rune) rune {
		if isInvisibleRune(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, s)
	return strings.TrimSpace(s)
}
