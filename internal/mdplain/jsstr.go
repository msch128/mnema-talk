package mdplain

import (
	"strings"
	"unicode/utf16"
)

// The web client's parser works on JavaScript strings: UTF-16 code units,
// JavaScript's white space and its regular expression classes. The inline
// scanner here works on the same units, so every index, bound and character
// test lands where the client's does.

// encode turns a string into UTF-16 code units.
func encode(s string) []uint16 { return utf16.Encode([]rune(s)) }

// decode turns UTF-16 code units back into a string.
func decode(u []uint16) string { return string(utf16.Decode(u)) }

// jsSpace is JavaScript's \s for one code unit.
func jsSpace(c uint16) bool {
	switch {
	case c == '\t', c == '\n', c == '\v', c == '\f', c == '\r', c == ' ', c == 0xa0, c == 0x1680:
		return true
	case c >= 0x2000 && c <= 0x200a, c == 0x2028, c == 0x2029, c == 0x202f, c == 0x205f, c == 0x3000, c == 0xfeff:
		return true
	}
	return false
}

// jsSpaceRune is jsSpace for a rune (every JavaScript white space is in the BMP).
func jsSpaceRune(r rune) bool { return r <= 0xffff && jsSpace(uint16(r)) }

// jsTrim is String.prototype.trim.
func jsTrim(s string) string { return strings.TrimFunc(s, jsSpaceRune) }

// jsTrimEnd is String.prototype.trimEnd.
func jsTrimEnd(s string) string { return strings.TrimRightFunc(s, jsSpaceRune) }

// spaceAt is the client's isSpace(src[i]): outside src counts as white space.
func spaceAt(src []uint16, i int) bool { return i < 0 || i >= len(src) || jsSpace(src[i]) }

// wordAt is the client's isWord(src[i]).
func wordAt(src []uint16, i int) bool {
	if i < 0 || i >= len(src) {
		return false
	}
	c := src[i]
	return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// escapableAt is the client's isEscapable(src[i]): any unit but a letter, a digit or white space.
func escapableAt(src []uint16, i int) bool {
	if i < 0 || i >= len(src) {
		return false
	}
	c := src[i]
	return !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || jsSpace(c))
}

// unitAt is src[i], or 0 outside src (no delimiter is 0).
func unitAt(src []uint16, i int) uint16 {
	if i < 0 || i >= len(src) {
		return 0
	}
	return src[i]
}

// hasAt reports whether the ASCII literal lit starts at src[i].
func hasAt(src []uint16, i int, lit string) bool {
	if i < 0 || i+len(lit) > len(src) {
		return false
	}
	for k := 0; k < len(lit); k++ {
		if src[i+k] != uint16(lit[k]) {
			return false
		}
	}
	return true
}

// hasAtFold is hasAt ignoring ASCII case; lit is lower case.
func hasAtFold(src []uint16, i int, lit string) bool {
	if i < 0 || i+len(lit) > len(src) {
		return false
	}
	for k := 0; k < len(lit); k++ {
		c := src[i+k]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != uint16(lit[k]) {
			return false
		}
	}
	return true
}

// onlySpace reports whether src holds nothing but white space (its trim() is empty).
func onlySpace(src []uint16) bool {
	for _, c := range src {
		if !jsSpace(c) {
			return false
		}
	}
	return true
}
