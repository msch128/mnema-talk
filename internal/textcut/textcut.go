// Package textcut shortens user text to a character budget without breaking
// the character a reader sees. A byte or rune cut can split a ZWJ sequence
// (a family emoji), a skin-tone or flag pair, or a base letter from its
// combining marks, which leaves a different or broken character behind.
package textcut

import (
	"unicode"
	"unicode/utf8"
)

const (
	zeroWidthJoiner = '‍'
	skinToneFirst   = 0x1f3fb
	skinToneLast    = 0x1f3ff
	tagFirst        = 0xe0020
	tagLast         = 0xe007f
	regionalFirst   = 0x1f1e6
	regionalLast    = 0x1f1ff
)

// Runes returns s cut to at most max runes, backed up to the start of the
// character the cut would have split. The result is always valid UTF-8 when s
// is, and s itself when it fits.
func Runes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return s[:boundary(s, runeOffset(s, max))]
}

// Bytes returns s cut to at most max bytes, backed up to a rune boundary and
// then to a character boundary.
func Bytes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:boundary(s, cut)]
}

// runeOffset is the byte offset of the n-th rune (counting from 0).
func runeOffset(s string, n int) int {
	for i := range s {
		if n == 0 {
			return i
		}
		n--
	}
	return len(s)
}

// boundary moves cut back until s[cut:] does not continue the character that
// ends at cut. It stops at the start of s.
func boundary(s string, cut int) int {
	for cut > 0 && cut < len(s) {
		next, _ := utf8.DecodeRuneInString(s[cut:])
		prev, size := utf8.DecodeLastRuneInString(s[:cut])
		switch {
		case continues(next), prev == zeroWidthJoiner:
			cut -= size
		case isRegional(next) && isRegional(prev) && oddRegionalRun(s[:cut]):
			cut -= size
		default:
			return cut
		}
	}
	return cut
}

// continues reports runes that attach to the one before them.
func continues(r rune) bool {
	switch {
	case r == zeroWidthJoiner, r >= 0xfe00 && r <= 0xfe0f:
		return true
	case r >= skinToneFirst && r <= skinToneLast, r >= tagFirst && r <= tagLast:
		return true
	}
	return unicode.In(r, unicode.Mn, unicode.Me, unicode.Mc)
}

func isRegional(r rune) bool { return r >= regionalFirst && r <= regionalLast }

// oddRegionalRun reports whether head ends in an odd number of regional
// indicators, i.e. a flag pair that the next indicator would complete.
func oddRegionalRun(head string) bool {
	n := 0
	for len(head) > 0 {
		r, size := utf8.DecodeLastRuneInString(head)
		if !isRegional(r) {
			break
		}
		n++
		head = head[:len(head)-size]
	}
	return n%2 == 1
}
