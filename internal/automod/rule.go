// Package automod checks new and edited messages and new accounts against the
// rules the Owner or an Administrator configured (doc/moderation.md, "Automod").
//
// The rules are compiled once per version of the configuration and kept in
// memory, so the check in the send path touches no database except to find out
// how old an account is (and that is remembered). A hit blocks the message or
// only flags it, may time the author out, and adds an entry to the moderation
// queue of reports. The package imports no feature package, so the message
// path (chat), the sign-up path (auth) and the server can all use it.
package automod

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/httpx"
	"golang.org/x/net/idna"
)

// Rule kinds.
const (
	KindWords      = "words"
	KindLinks      = "links"
	KindMentions   = "mentions"
	KindNewAccount = "new_account"
	KindRaid       = "raid"
)

// Rule actions: block the message, or let it through and only flag it.
const (
	ActionBlock = "block"
	ActionFlag  = "flag"
)

// Link filter modes.
const (
	LinksBlockAll  = "block_all"
	LinksAllowList = "allow_list"
	LinksBlockList = "block_list"
)

// What a new account may not do.
const (
	RestrictLinks         = "links"
	RestrictAttachments   = "attachments"
	RestrictLinksAndFiles = "links_and_attachments"
	RestrictEverything    = "everything"
)

// Limits of the configuration.
const (
	MaxRules          = 25
	MaxWords          = 200
	MaxWordLen        = 64
	MaxDomains        = 100
	MaxExemptions     = 50
	MaxNameLen        = 60
	MaxMentionsLimit  = 50
	MinAccountAgeSecs = 60
	MaxAccountAgeSecs = 30 * 24 * 3600
	MinRaidJoins      = 2
	MaxRaidJoins      = 200
	MinRaidWindow     = 5
	MaxRaidWindow     = 3600
	MinRestrictSecs   = 60
	MaxRestrictSecs   = 24 * 3600
	MaxTimeoutSecs    = 28 * 24 * 3600
	MinTimeoutSecs    = 5
)

// Settings holds the settings of a rule; which fields count depends on its kind
// (the others stay empty).
type Settings struct {
	// Words (words): blocked words and phrases, matched as whole words without
	// regard to case; "*" in an entry stands for any run of letters and digits.
	Words []string `json:"words,omitempty" binding:"optional" maxLength:"64"`
	// Mode (links): block all links, allow only the listed domains, or block
	// the listed domains.
	Mode string `json:"mode,omitempty" binding:"optional" enums:"block_all,allow_list,block_list"`
	// Domains (links): the domains of the lists; a domain covers its subdomains.
	Domains []string `json:"domains,omitempty" binding:"optional"`
	// MaxMentions (mentions): a message with more distinct mentions breaks the rule.
	MaxMentions int `json:"max_mentions,omitempty" binding:"optional" minimum:"1" maximum:"50"`
	// MaxAgeSeconds (new_account): accounts younger than this are restricted
	// (60 seconds to 30 days).
	MaxAgeSeconds int `json:"max_age_seconds,omitempty" binding:"optional"`
	// Restrict (new_account): what a young account may not do.
	Restrict string `json:"restrict,omitempty" binding:"optional" enums:"links,attachments,links_and_attachments,everything"`
	// Joins and WindowSeconds (raid): more than Joins new accounts within
	// WindowSeconds count as a raid.
	Joins         int `json:"joins,omitempty" binding:"optional" minimum:"2" maximum:"200"`
	WindowSeconds int `json:"window_seconds,omitempty" binding:"optional" minimum:"5" maximum:"3600"`
	// RestrictSeconds (raid): how long new joiners stay restricted (timed out),
	// 60 seconds to a day.
	RestrictSeconds int `json:"restrict_seconds,omitempty" binding:"optional"`
}

// Rule is one automod rule as stored and as the API shows it.
type Rule struct {
	ID      uuid.UUID `json:"id" format:"uuid"`
	Kind    string    `json:"kind" enums:"words,links,mentions,new_account,raid"`
	Name    string    `json:"name" minLength:"1" maxLength:"60"`
	Enabled bool      `json:"enabled"`
	// Action is what a hit does to the message; raid protection only flags.
	Action string `json:"action" enums:"block,flag"`
	// TimeoutSeconds, when above 0, also times the author out for that long.
	TimeoutSeconds int      `json:"timeout_seconds" minimum:"0" maximum:"2419200"`
	Config         Settings `json:"config"`
	// ExemptRoles and ExemptChannels are skipped by the rule.
	ExemptRoles    []uuid.UUID `json:"exempt_roles" format:"uuid"`
	ExemptChannels []uuid.UUID `json:"exempt_channels" format:"uuid"`
	UpdatedAt      time.Time   `json:"updated_at" format:"date-time"`
}

// RuleInput is the body that creates or replaces a rule.
type RuleInput struct {
	Kind           string      `json:"kind" enums:"words,links,mentions,new_account,raid"`
	Name           string      `json:"name" minLength:"1" maxLength:"60"`
	Enabled        bool        `json:"enabled"`
	Action         string      `json:"action" enums:"block,flag"`
	TimeoutSeconds int         `json:"timeout_seconds" minimum:"0" maximum:"2419200" binding:"optional"`
	Config         Settings    `json:"config"`
	ExemptRoles    []uuid.UUID `json:"exempt_roles" format:"uuid" binding:"optional"`
	ExemptChannels []uuid.UUID `json:"exempt_channels" format:"uuid" binding:"optional"`
}

// Normalize validates the input and returns it in its stored form: texts
// trimmed, lists deduplicated, the raid rule's fixed action set.
func (in RuleInput) Normalize() (RuleInput, error) {
	name, err := httpx.CleanName("name", in.Name, MaxNameLen, true)
	if err != nil {
		return in, err
	}
	in.Name = name
	if in.Action != ActionBlock && in.Action != ActionFlag {
		return in, httpx.ErrInvalidInput("action must be block or flag")
	}
	if in.TimeoutSeconds != 0 && (in.TimeoutSeconds < MinTimeoutSecs || in.TimeoutSeconds > MaxTimeoutSecs) {
		return in, httpx.ErrInvalidInput(fmt.Sprintf("timeout_seconds must be 0 or between %d and %d", MinTimeoutSecs, MaxTimeoutSecs))
	}
	if len(in.ExemptRoles) > MaxExemptions || len(in.ExemptChannels) > MaxExemptions {
		return in, httpx.ErrInvalidInput(fmt.Sprintf("at most %d exempt roles and channels", MaxExemptions))
	}
	in.ExemptRoles, in.ExemptChannels = uniqueIDs(in.ExemptRoles), uniqueIDs(in.ExemptChannels)
	in.Config, err = in.Config.normalize(in.Kind)
	if in.Kind == KindRaid {
		in.Action, in.TimeoutSeconds = ActionFlag, 0
	}
	return in, err
}

func uniqueIDs(ids []uuid.UUID) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// normalize checks the settings of kind and returns only those.
func (c Settings) normalize(kind string) (Settings, error) {
	switch kind {
	case KindWords:
		words, err := cleanList(c.Words, "words", MaxWords, MaxWordLen, cleanWord)
		return Settings{Words: words}, err
	case KindLinks:
		return c.normalizeLinks()
	case KindMentions:
		if c.MaxMentions < 1 || c.MaxMentions > MaxMentionsLimit {
			return c, httpx.ErrInvalidInput(fmt.Sprintf("max_mentions must be between 1 and %d", MaxMentionsLimit))
		}
		return Settings{MaxMentions: c.MaxMentions}, nil
	case KindNewAccount:
		if c.MaxAgeSeconds < MinAccountAgeSecs || c.MaxAgeSeconds > MaxAccountAgeSecs {
			return c, httpx.ErrInvalidInput(fmt.Sprintf("max_age_seconds must be between %d and %d", MinAccountAgeSecs, MaxAccountAgeSecs))
		}
		if !slices.Contains([]string{RestrictLinks, RestrictAttachments, RestrictLinksAndFiles, RestrictEverything}, c.Restrict) {
			return c, httpx.ErrInvalidInput("restrict must be links, attachments, links_and_attachments or everything")
		}
		return Settings{MaxAgeSeconds: c.MaxAgeSeconds, Restrict: c.Restrict}, nil
	case KindRaid:
		return c.normalizeRaid()
	}
	return c, httpx.ErrInvalidInput("kind must be words, links, mentions, new_account or raid")
}

func (c Settings) normalizeLinks() (Settings, error) {
	if !slices.Contains([]string{LinksBlockAll, LinksAllowList, LinksBlockList}, c.Mode) {
		return c, httpx.ErrInvalidInput("mode must be block_all, allow_list or block_list")
	}
	domains, err := cleanList(c.Domains, "domains", MaxDomains, 253, cleanDomain)
	if c.Mode == LinksBlockAll {
		domains = nil
	}
	return Settings{Mode: c.Mode, Domains: domains}, err
}

func (c Settings) normalizeRaid() (Settings, error) {
	switch {
	case c.Joins < MinRaidJoins || c.Joins > MaxRaidJoins:
		return c, httpx.ErrInvalidInput(fmt.Sprintf("joins must be between %d and %d", MinRaidJoins, MaxRaidJoins))
	case c.WindowSeconds < MinRaidWindow || c.WindowSeconds > MaxRaidWindow:
		return c, httpx.ErrInvalidInput(fmt.Sprintf("window_seconds must be between %d and %d", MinRaidWindow, MaxRaidWindow))
	case c.RestrictSeconds < MinRestrictSecs || c.RestrictSeconds > MaxRestrictSecs:
		return c, httpx.ErrInvalidInput(fmt.Sprintf("restrict_seconds must be between %d and %d", MinRestrictSecs, MaxRestrictSecs))
	}
	return Settings{Joins: c.Joins, WindowSeconds: c.WindowSeconds, RestrictSeconds: c.RestrictSeconds}, nil
}

// cleanList trims, checks and deduplicates (case-insensitively) a list.
func cleanList(in []string, field string, maxItems, maxLen int, clean func(string) (string, error)) ([]string, error) {
	if len(in) > maxItems {
		return nil, httpx.ErrInvalidInput(fmt.Sprintf("%s: at most %d entries", field, maxItems))
	}
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, raw := range in {
		s, err := httpx.CleanText(field, raw, maxLen, false)
		if err != nil {
			return nil, err
		}
		if s, err = clean(s); err != nil {
			return nil, err
		}
		if s == "" || seen[strings.ToLower(s)] {
			continue
		}
		seen[strings.ToLower(s)] = true
		out = append(out, s)
	}
	return out, nil
}

func cleanWord(s string) (string, error) {
	s = strings.Join(strings.Fields(s), " ")
	for _, r := range s {
		if r < ' ' || r == 0x7f {
			return "", httpx.ErrInvalidInput("words must not contain control characters")
		}
	}
	if s != "" && strings.Trim(s, "* ") == "" {
		return "", httpx.ErrInvalidInput("a word needs at least one letter or digit besides *")
	}
	if utf8.RuneCountInString(s) > MaxWordLen {
		return "", httpx.ErrInvalidInput("a word is too long")
	}
	return s, nil
}

var domainPattern = regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9-]{2,63}$`)

// cleanDomain accepts a domain, or a link whose domain is meant, and returns
// the domain in the form links are compared in (lower case, punycode).
func cleanDomain(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	host, ok := hostOf(strings.TrimPrefix(s, "*."))
	if !ok || !domainPattern.MatchString(host) {
		return "", httpx.ErrInvalidInput(fmt.Sprintf("%q is not a domain", s))
	}
	return host, nil
}

// hostOf reads the host a browser would connect to from a link or bare
// domain: the part after the last "@" of the authority (so
// http://allowed.com@evil.com is evil.com), without port, percent-decoded,
// mapped by UTS-46 to punycode (full-width and ideographic dots become dots),
// lower case, without a trailing dot. ok is false when it cannot be read.
func hostOf(link string) (string, bool) {
	l := strings.TrimSpace(link)
	if i := strings.Index(l, "://"); i >= 0 {
		l = l[i+3:]
	}
	if i := strings.IndexAny(l, "/?#\\"); i >= 0 {
		l = l[:i]
	}
	if i := strings.LastIndex(l, "@"); i >= 0 {
		l = l[i+1:]
	}
	if strings.HasPrefix(l, "[") { // an IP literal: never a listed domain
		return strings.ToLower(l), strings.HasSuffix(l, "]")
	}
	if i := strings.LastIndex(l, ":"); i >= 0 {
		port := l[i+1:]
		if strings.Trim(port, "0123456789") != "" {
			return "", false
		}
		l = l[:i]
	}
	l, err := url.PathUnescape(l)
	if err != nil {
		return "", false
	}
	l, err = idna.Lookup.ToASCII(l)
	l = strings.TrimRight(strings.ToLower(l), ".")
	return l, err == nil && l != ""
}
