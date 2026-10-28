package automod

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/mention"
	"golang.org/x/text/unicode/norm"
)

// compiled is a rule ready to run: the patterns are built once, when the
// configuration is loaded, not per message.
type compiled struct {
	Rule
	exemptRoles    map[uuid.UUID]bool
	exemptChannels map[uuid.UUID]bool
	words          *regexp.Regexp
	domains        []string
}

// wordChars is what a "*" in a blocked word stands for, and what counts as
// part of a word at its edges.
const wordChars = `[\p{L}\p{N}_]`

func compile(r Rule) (*compiled, error) {
	c := &compiled{Rule: r, exemptRoles: set(r.ExemptRoles), exemptChannels: set(r.ExemptChannels)}
	switch r.Kind {
	case KindWords:
		re, err := compileWords(r.Config.Words)
		if err != nil {
			return nil, err
		}
		c.words = re
	case KindLinks:
		c.domains = r.Config.Domains
	}
	return c, nil
}

func set(ids []uuid.UUID) map[uuid.UUID]bool {
	m := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}

// compileWords builds one pattern for the whole list: each entry matches as a
// whole word or phrase, case-insensitively; "*" matches any run of letters and
// digits, spaces of a phrase any run of white space. nil: nothing to match.
func compileWords(words []string) (*regexp.Regexp, error) {
	var alts []string
	for _, w := range words {
		folded := fold(w)
		if strings.Trim(folded, "* ") == "" {
			continue // nothing left to match: an empty branch would match everything
		}
		parts := strings.Split(folded, "*")
		for i, p := range parts {
			parts[i] = phrasePart(p)
		}
		alts = append(alts, strings.Join(parts, wordChars+"*"))
	}
	if len(alts) == 0 {
		return nil, nil
	}
	return regexp.Compile(`(?i)(?:^|[^\p{L}\p{N}_])(?:` + strings.Join(alts, "|") + `)(?:[^\p{L}\p{N}_]|$)`)
}

// phrasePart quotes one piece of a blocked word; any run of white space inside
// (or at its edge) matches any run of white space.
func phrasePart(p string) string {
	fields := strings.Fields(p)
	for i := range fields {
		fields[i] = regexp.QuoteMeta(fields[i])
	}
	out := strings.Join(fields, `\s+`)
	if len(fields) > 0 && strings.TrimLeft(p, " \t") != p {
		out = `\s+` + out
	}
	if len(fields) > 0 && strings.TrimRight(p, " \t") != p {
		out += `\s+`
	}
	return out
}

// fold prepares text for matching: compatibility forms (fullwidth letters,
// no-break spaces) are normalised, every kind of space becomes a plain one and
// invisible format characters and stray combining marks are dropped, so none of
// them can hide a blocked word or end a link early.
func fold(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case unicode.IsSpace(r) || unicode.Is(unicode.Z, r):
			return ' '
		case unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Mn, r):
			return -1
		}
		return r
	}, norm.NFKC.String(s))
}

// matchesWords reports whether content contains a blocked word.
func (c *compiled) matchesWords(content string) bool {
	return c.words != nil && c.words.MatchString(fold(content))
}

var linkToken = regexp.MustCompile(`(?i)\b(?:https?://|www\.)[^\s<>"'\x60]+`)

// link is a link-like token of a message: its host, or ok false when the
// token cannot be read (which counts against it, never for it).
type link struct {
	host string
	ok   bool
}

// links lists the links in content.
func links(content string) []link {
	var out []link
	for _, m := range linkToken.FindAllString(fold(content), -1) {
		host, ok := hostOf(strings.TrimRight(m, ".,;:!?)]}"))
		out = append(out, link{host, ok})
	}
	return out
}

// matchesLinks reports whether content has a link the rule forbids. A link
// that cannot be read is forbidden whenever the rule is about which links are
// fine (every link, an allow list); a block list only knows the hosts it lists.
func (c *compiled) matchesLinks(content string) bool {
	for _, l := range links(content) {
		switch c.Config.Mode {
		case LinksBlockAll:
			return true
		case LinksAllowList:
			if !l.ok || !coveredBy(l.host, c.domains) {
				return true
			}
		case LinksBlockList:
			if l.ok && coveredBy(l.host, c.domains) {
				return true
			}
		}
	}
	return false
}

// coveredBy reports whether host is one of the domains or a subdomain of one.
func coveredBy(host string, domains []string) bool {
	for _, d := range domains {
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

// distinctMentions counts the different names mentioned, @all and @here
// included, exactly as chat reads them.
func distinctMentions(content string) int { return len(mention.Names(content)) }
