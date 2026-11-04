package wiki

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// reserved are addresses the wiki's own screens use: /wiki/<slug> never means one of them.
var reserved = map[string]bool{
	"search": true, "new": true, "edit": true, "history": true, "recent": true,
	"deleted": true, "api": true, "admin": true, "wiki": true, "pages": true, "revisions": true,
}

// transliterations are the letters the address turns into plain ones.
var transliterations = map[rune]string{
	'ä': "ae", 'ö': "oe", 'ü': "ue", 'ß': "ss", 'æ': "ae", 'ø': "o", 'å': "a", 'é': "e", 'è': "e", 'ê': "e", 'ë': "e",
	'á': "a", 'à': "a", 'â': "a", 'ã': "a", 'í': "i", 'ì': "i", 'î': "i", 'ï': "i", 'ó': "o", 'ò': "o", 'ô': "o", 'õ': "o",
	'ú': "u", 'ù': "u", 'û': "u", 'ñ': "n", 'ç': "c", 'ł': "l", 'ś': "s", 'š': "s", 'ž': "z", 'ý': "y", 'ÿ': "y", 'ź': "z", 'ż': "z", 'ć': "c", 'ń': "n", 'ą': "a", 'ę': "e", 'č': "c", 'ř': "r", 'ě': "e", 'ő': "o", 'ű': "u",
}

// DeriveSlug makes an address from a title: lower case, plain letters and
// digits, single hyphens. A title without any of those becomes "page".
func DeriveSlug(title string) string {
	var b strings.Builder
	pendingHyphen := false
	for _, r := range strings.ToLower(title) {
		var part string
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			part = string(r)
		case transliterations[r] != "":
			part = transliterations[r]
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || unicode.IsSpace(r) || unicode.IsPunct(r):
			pendingHyphen = b.Len() > 0
			continue
		default:
			continue
		}
		if pendingHyphen {
			b.WriteByte('-')
			pendingHyphen = false
		}
		b.WriteString(part)
	}
	slug := b.String()
	if len(slug) > MaxSlugLen {
		slug = strings.TrimRight(slug[:MaxSlugLen], "-")
	}
	if slug == "" {
		return "page"
	}
	return slug
}

// ValidSlug reports whether s is an address a page may have.
func ValidSlug(s string) bool {
	return len(s) <= MaxSlugLen && slugPattern.MatchString(s) && !reserved[s]
}

// numbered is slug with n appended ("faq-2"), cut so it still fits.
func numbered(slug string, n int) string {
	suffix := "-" + strconv.Itoa(n)
	if len(slug)+len(suffix) > MaxSlugLen {
		slug = strings.TrimRight(slug[:MaxSlugLen-len(suffix)], "-")
	}
	return slug + suffix
}
