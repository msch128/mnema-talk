package webbundle

import (
	"strings"
	"unicode/utf8"
)

func validPathRune(r rune) bool {
	return r >= 0x20 && r != 0x7f && r != '\\' && r != ':'
}

func validPathSegment(seg string) bool {
	if seg == "" || seg == "." || seg == ".." {
		return false
	}
	// Windows silently strips trailing dots and spaces and reserves device names.
	if last := seg[len(seg)-1]; last == '.' || last == ' ' {
		return false
	}
	return !windowsReserved[strings.ToUpper(strings.SplitN(seg, ".", 2)[0])]
}

// windowsReserved are device names Windows cannot use as a file name, with or
// without an extension.
var windowsReserved = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
	"COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
	"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// ValidPath reports whether p is an acceptable bundle-relative path: UTF-8,
// forward slashes only, no empty, "." or ".." segments, no control characters,
// and nothing a Windows file system would rename (":", trailing dot or space,
// device names).
func ValidPath(p string) bool {
	if p == "" || len(p) > maxPathLen || !utf8.ValidString(p) {
		return false
	}
	for _, r := range p {
		if !validPathRune(r) {
			return false
		}
	}
	for _, seg := range strings.Split(p, "/") {
		if !validPathSegment(seg) {
			return false
		}
	}
	return true
}
